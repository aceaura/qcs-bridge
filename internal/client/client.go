// Package client is the local side of the qcs bridge: it signs commands,
// sends them over SQS, and waits for correlated results. Shared by the
// MCP server (cmd/qcs-mcp) and the CLI (cmd/qcs).
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"qcs-bridge/internal/proto"
)

// ResultWaitBuffer is extra time beyond the command timeout to wait for the result.
const ResultWaitBuffer = 45 * time.Second

type Bridge struct {
	sqsClient *sqs.Client
	secret    string
	cmdQueue  string
	resQueue  string
	hbQueue   string
}

func getenv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func loadSecret() (string, error) {
	if s := os.Getenv("QCS_SECRET"); s != "" {
		return s, nil
	}
	if f := os.Getenv("QCS_SECRET_FILE"); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".qcs-secret"))
	if err != nil {
		return "", errors.New("no secret found: set QCS_SECRET, QCS_SECRET_FILE, or ~/.qcs-secret")
	}
	return strings.TrimSpace(string(b)), nil
}

func resolveQueueURL(ctx context.Context, client *sqs.Client, nameOrURL string) (string, error) {
	if strings.HasPrefix(nameOrURL, "https://") {
		return nameOrURL, nil
	}
	out, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(nameOrURL)})
	if err != nil {
		return "", fmt.Errorf("resolve queue %q: %w", nameOrURL, err)
	}
	return *out.QueueUrl, nil
}

// New loads config from the environment and resolves queue URLs.
// Required env: QCS_CMD_QUEUE, QCS_RESULT_QUEUE, QCS_HEARTBEAT_QUEUE (names or URLs),
// plus a secret via QCS_SECRET / QCS_SECRET_FILE / ~/.qcs-secret and standard AWS credentials.
func New(ctx context.Context) (*Bridge, error) {
	cmdQueue := getenv("QCS_CMD_QUEUE")
	resQueue := getenv("QCS_RESULT_QUEUE")
	hbQueue := getenv("QCS_HEARTBEAT_QUEUE")
	if cmdQueue == "" || resQueue == "" || hbQueue == "" {
		return nil, errors.New("set QCS_CMD_QUEUE, QCS_RESULT_QUEUE, QCS_HEARTBEAT_QUEUE (names or URLs)")
	}
	secret, err := loadSecret()
	if err != nil {
		return nil, err
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	sqsClient := sqs.NewFromConfig(cfg)

	b := &Bridge{sqsClient: sqsClient, secret: secret}
	if b.cmdQueue, err = resolveQueueURL(ctx, sqsClient, cmdQueue); err != nil {
		return nil, err
	}
	if b.resQueue, err = resolveQueueURL(ctx, sqsClient, resQueue); err != nil {
		return nil, err
	}
	if b.hbQueue, err = resolveQueueURL(ctx, sqsClient, hbQueue); err != nil {
		return nil, err
	}
	return b, nil
}

// Exec sends a command to CloudShell and waits for its result.
// Returns the remote exit code and formatted output.
func (b *Bridge) Exec(ctx context.Context, command string, timeoutSec int) (string, int, error) {
	if strings.TrimSpace(command) == "" {
		return "", -1, errors.New("empty command")
	}
	if timeoutSec <= 0 {
		timeoutSec = 120
	}

	c := proto.Command{ID: proto.NewID(), TS: time.Now().Unix(), Cmd: command, Timeout: timeoutSec}
	proto.SignCommand(b.secret, &c)
	body, _ := json.Marshal(c)

	if _, err := b.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(b.cmdQueue),
		MessageBody:            aws.String(string(body)),
		MessageGroupId:         aws.String("qcs"),
		MessageDeduplicationId: aws.String("cmd-" + c.ID),
	}); err != nil {
		return "", -1, fmt.Errorf("send command: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second+ResultWaitBuffer)
	defer cancel()

	r, err := b.waitResult(waitCtx, c.ID)
	if err != nil {
		return "", -1, err
	}

	var sb strings.Builder
	if r.Rejected != "" {
		fmt.Fprintf(&sb, "[REJECTED by agent policy] %s\n", r.Rejected)
	}
	if r.Stdout != "" {
		sb.WriteString(r.Stdout)
		if !strings.HasSuffix(r.Stdout, "\n") {
			sb.WriteString("\n")
		}
	}
	if r.Stderr != "" {
		fmt.Fprintf(&sb, "[stderr]\n%s\n", r.Stderr)
	}
	if r.Truncated {
		sb.WriteString("[note] output was truncated to fit SQS message limit\n")
	}
	fmt.Fprintf(&sb, "[exit_code=%d]", r.ExitCode)
	return sb.String(), r.ExitCode, nil
}

func (b *Bridge) waitResult(ctx context.Context, wantID string) (proto.Result, error) {
	for {
		out, err := b.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(b.resQueue),
			WaitTimeSeconds:     10,
			MaxNumberOfMessages: 10,
			VisibilityTimeout:   5,
		})
		if err != nil {
			if ctx.Err() != nil {
				return proto.Result{}, fmt.Errorf("timed out waiting for result of command %s (agent offline? run status)", wantID)
			}
			return proto.Result{}, fmt.Errorf("receive result: %w", err)
		}
		for _, msg := range out.Messages {
			var r proto.Result
			if err := json.Unmarshal([]byte(*msg.Body), &r); err != nil || !proto.VerifyResult(b.secret, r) {
				b.deleteMessage(ctx, b.resQueue, msg.ReceiptHandle)
				continue
			}
			stale := time.Since(time.Unix(r.TS, 0)) > 10*time.Minute
			if r.ID == wantID || stale {
				b.deleteMessage(ctx, b.resQueue, msg.ReceiptHandle)
			}
			if r.ID == wantID {
				return r, nil
			}
		}
	}
}

// Status reports the agent's last heartbeat, keeping the newest heartbeat
// message in the queue for subsequent calls.
func (b *Bridge) Status(ctx context.Context) (string, error) {
	out, err := b.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(b.hbQueue),
		WaitTimeSeconds:     3,
		MaxNumberOfMessages: 10,
		VisibilityTimeout:   10,
	})
	if err != nil {
		return "", fmt.Errorf("receive heartbeat: %w", err)
	}

	var latest *proto.Heartbeat
	for _, msg := range out.Messages {
		var h proto.Heartbeat
		if err := json.Unmarshal([]byte(*msg.Body), &h); err != nil || !proto.VerifyHeartbeat(b.secret, h) {
			b.deleteMessage(ctx, b.hbQueue, msg.ReceiptHandle)
			continue
		}
		if latest == nil || h.TS > latest.TS {
			latest = &h
		}
	}
	if latest == nil {
		return "qcs-agent: OFFLINE (no heartbeat in queue — CloudShell VM was likely recycled; reopen CloudShell and run qcs-start)", nil
	}

	latestBody, _ := json.Marshal(*latest)
	for _, msg := range out.Messages {
		if *msg.Body != string(latestBody) {
			b.deleteMessage(ctx, b.hbQueue, msg.ReceiptHandle)
		}
	}

	age := time.Since(time.Unix(latest.TS, 0)).Round(time.Second)
	state := "ONLINE"
	if age > 3*time.Minute {
		state = "STALE (agent probably dead)"
	}
	return fmt.Sprintf("qcs-agent: %s\nidentity: %s\nhost: %s\nlast heartbeat: %s ago\nagent uptime: %ds",
		state, latest.Identity, latest.Host, age, latest.UptimeS), nil
}

func (b *Bridge) deleteMessage(ctx context.Context, queue string, receipt *string) {
	_, _ = b.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(queue), ReceiptHandle: receipt})
}
