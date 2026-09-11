// qcs-agent runs inside AWS CloudShell: long-polls an SQS command queue,
// executes signed commands locally, and posts results back to SQS.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	qcsconfig "qcs-bridge/internal/config"
	"qcs-bridge/internal/proto"
)

const maxCmdTimeout = 300

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

type agent struct {
	sqsClient *sqs.Client
	secret    string
	cmdQueue  string
	resQueue  string
	hbQueue   string
	identity  string
	hostname  string
	started   time.Time
	readOnly  bool
	audit     *os.File
	auditPath string
}

func (a *agent) auditf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	log.Print(line)
	if a.audit != nil {
		fmt.Fprintf(a.audit, "%s %s\n", time.Now().UTC().Format(time.RFC3339), line)
	}
}

func (a *agent) sendHeartbeat(ctx context.Context) {
	h := proto.Heartbeat{
		ID:       "heartbeat",
		TS:       time.Now().Unix(),
		Identity: a.identity,
		Host:     a.hostname,
		UptimeS:  int64(time.Since(a.started).Seconds()),
	}
	proto.SignHeartbeat(a.secret, &h)
	body, _ := json.Marshal(h)
	_, err := a.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(a.hbQueue),
		MessageBody:            aws.String(string(body)),
		MessageGroupId:         aws.String("qcs"),
		MessageDeduplicationId: aws.String(fmt.Sprintf("hb-%d", h.TS)),
	})
	if err != nil {
		log.Printf("heartbeat send failed: %v", err)
		return
	}
	log.Printf("--- heartbeat ok (uptime %ds) ---", h.UptimeS)
}

func (a *agent) runHeartbeatLoop(ctx context.Context) {
	a.sendHeartbeat(ctx)
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.sendHeartbeat(ctx)
		}
	}
}

func (a *agent) execute(ctx context.Context, c proto.Command) proto.Result {
	r := proto.Result{ID: c.ID, TS: time.Now().Unix()}

	if a.readOnly {
		if err := proto.CheckReadOnly(c.Cmd); err != nil {
			r.Rejected = err.Error()
			r.ExitCode = -1
			a.auditf("REJECTED id=%s cmd=%q reason=%s", c.ID, c.Cmd, err)
			return r
		}
	}

	timeout := c.Timeout
	if timeout <= 0 || timeout > maxCmdTimeout {
		timeout = maxCmdTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", c.Cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	r.ExitCode = 0
	if runErr != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(runErr, &exitErr):
			r.ExitCode = exitErr.ExitCode()
		case errors.Is(cmdCtx.Err(), context.DeadlineExceeded):
			r.ExitCode = -1
			stderr.WriteString(fmt.Sprintf("\n[qcs-agent] killed after %ds timeout\n", timeout))
		default:
			r.ExitCode = -1
			stderr.WriteString("\n[qcs-agent] exec error: " + runErr.Error() + "\n")
		}
	}

	r.Stdout, r.Stderr, r.Truncated = proto.Truncate(stdout.String(), stderr.String())
	a.auditf("EXEC id=%s exit=%d truncated=%v cmd=%q", c.ID, r.ExitCode, r.Truncated, c.Cmd)
	return r
}

// displayResult prints the full interaction to the terminal (and log file
// when stdout/stderr are redirected there), so every request/response is visible.
func (a *agent) displayResult(r proto.Result) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<<< RESULT id=%s exit=%d truncated=%v\n", r.ID, r.ExitCode, r.Truncated)
	if r.Rejected != "" {
		fmt.Fprintf(&sb, "[REJECTED] %s\n", r.Rejected)
	}
	sb.WriteString("----- stdout -----\n")
	if r.Stdout == "" {
		sb.WriteString("(empty)\n")
	} else {
		sb.WriteString(r.Stdout)
		if !strings.HasSuffix(r.Stdout, "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("----- stderr -----\n")
	if r.Stderr == "" {
		sb.WriteString("(empty)\n")
	} else {
		sb.WriteString(r.Stderr)
		if !strings.HasSuffix(r.Stderr, "\n") {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("------------------")
	log.Print(sb.String())
}

func (a *agent) pollOnce(ctx context.Context) error {
	out, err := a.sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(a.cmdQueue),
		WaitTimeSeconds:     20,
		MaxNumberOfMessages: 1,
	})
	if err != nil {
		return err
	}
	for _, msg := range out.Messages {
		var c proto.Command
		if err := json.Unmarshal([]byte(*msg.Body), &c); err != nil {
			a.auditf("DROP unparseable message: %v", err)
			a.delete(ctx, msg.ReceiptHandle)
			continue
		}
		if err := proto.VerifyCommand(a.secret, c); err != nil {
			a.auditf("DROP invalid command id=%s: %v", c.ID, err)
			a.delete(ctx, msg.ReceiptHandle)
			continue
		}

		a.auditf(">>> RECV id=%s timeout=%ds cmd=%q", c.ID, c.Timeout, c.Cmd)
		r := a.execute(ctx, c)
		a.displayResult(r)
		proto.SignResult(a.secret, &r)
		body, _ := json.Marshal(r)
		_, sendErr := a.sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:               aws.String(a.resQueue),
			MessageBody:            aws.String(string(body)),
			MessageGroupId:         aws.String("qcs"),
			MessageDeduplicationId: aws.String("res-" + r.ID),
		})
		if sendErr != nil {
			log.Printf("result send failed for %s: %v (command left for retry)", r.ID, sendErr)
			continue
		}
		a.delete(ctx, msg.ReceiptHandle)
	}
	return nil
}

func (a *agent) delete(ctx context.Context, receipt *string) {
	_, err := a.sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(a.cmdQueue),
		ReceiptHandle: receipt,
	})
	if err != nil {
		log.Printf("delete failed: %v", err)
	}
}

func main() {
	allowAll := flag.Bool("allow-all", false, "disable read-only allowlist (dangerous: permits arbitrary commands)")
	flag.Parse()

	log.SetPrefix("[qcs-agent] ")
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)

	cfgFile, err := qcsconfig.Load()
	if err != nil {
		log.Fatalf("load config file: %v", err)
	}
	cmdQueue := cfgFile.GetOr(qcsconfig.DefaultCmdQueue, "QCS_CMD_QUEUE")
	resQueue := cfgFile.GetOr(qcsconfig.DefaultResultQueue, "QCS_RESULT_QUEUE")
	hbQueue := cfgFile.GetOr(qcsconfig.DefaultHeartbeatQueue, "QCS_HEARTBEAT_QUEUE")
	secret, err := qcsconfig.LoadSecret()
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// An explicit region always wins; otherwise the secret token carries it.
	loadOpts := []func(*config.LoadOptions) error{}
	if region := cfgFile.Get("AWS_REGION", "AWS_DEFAULT_REGION"); region != "" {
		loadOpts = append(loadOpts, config.WithRegion(region))
	} else if secret.Region != "" {
		loadOpts = append(loadOpts, config.WithRegion(secret.Region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}
	sqsClient := sqs.NewFromConfig(cfg)

	identity := "unknown"
	if stsOut, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err == nil {
		identity = aws.ToString(stsOut.Arn)
	}
	hostname, _ := os.Hostname()

	home, _ := os.UserHomeDir()
	auditPath := filepath.Join(home, "qcs-audit.log")
	auditFile, err := os.OpenFile(auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("audit log unavailable: %v", err)
	}
	defer func() {
		if auditFile != nil {
			auditFile.Close()
		}
	}()

	a := &agent{
		sqsClient: sqsClient,
		secret:    secret.Value,
		identity:  identity,
		hostname:  hostname,
		started:   time.Now(),
		readOnly:  !*allowAll,
		audit:     auditFile,
	}

	a.cmdQueue, err = resolveQueueURL(ctx, sqsClient, cmdQueue)
	if err != nil {
		log.Fatal(err)
	}
	a.resQueue, err = resolveQueueURL(ctx, sqsClient, resQueue)
	if err != nil {
		log.Fatal(err)
	}
	a.hbQueue, err = resolveQueueURL(ctx, sqsClient, hbQueue)
	if err != nil {
		log.Fatal(err)
	}

	mode := "read-only"
	if !a.readOnly {
		mode = "ALLOW-ALL (dangerous)"
	}
	a.auditf("START identity=%s host=%s mode=%s", identity, hostname, mode)

	go a.runHeartbeatLoop(ctx)

	for {
		if err := a.pollOnce(ctx); err != nil {
			if ctx.Err() != nil {
				a.auditf("STOP")
				return
			}
			log.Printf("poll error: %v (retrying in 5s)", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}
