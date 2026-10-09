package proto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxOutputBytes keeps a result message safely under the SQS 256KB limit.
	MaxOutputBytes = 200 * 1024
	// MaxClockSkew is the accepted age of a signed command.
	MaxClockSkew = 5 * time.Minute
)

type Command struct {
	ID      string `json:"id"`
	TS      int64  `json:"ts"`
	Cmd     string `json:"cmd"`
	Timeout int    `json:"timeout_sec"`
	Sig     string `json:"sig"`
}

type Result struct {
	ID        string `json:"id"`
	TS        int64  `json:"ts"`
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
	Rejected  string `json:"rejected,omitempty"`
	Sig       string `json:"sig"`
}

type Heartbeat struct {
	ID       string `json:"id"` // always "heartbeat"
	TS       int64  `json:"ts"`
	Identity string `json:"identity"`
	Host     string `json:"host"`
	UptimeS  int64  `json:"uptime_sec"`
	Sig      string `json:"sig"`
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func Sign(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func commandPayload(c Command) string {
	return strings.Join([]string{c.ID, strconv.FormatInt(c.TS, 10), c.Cmd, strconv.Itoa(c.Timeout)}, "|")
}

func resultPayload(r Result) string {
	return strings.Join([]string{r.ID, strconv.FormatInt(r.TS, 10), strconv.Itoa(r.ExitCode), r.Stdout, r.Stderr, r.Rejected}, "|")
}

func heartbeatPayload(h Heartbeat) string {
	return strings.Join([]string{h.ID, strconv.FormatInt(h.TS, 10), h.Identity, h.Host}, "|")
}

func checkSig(secret, payload, sig string) bool {
	return hmac.Equal([]byte(Sign(secret, payload)), []byte(sig))
}

func SignCommand(secret string, c *Command) { c.Sig = Sign(secret, commandPayload(*c)) }

// VerifyCommand checks signature and freshness.
func VerifyCommand(secret string, c Command) error {
	if !checkSig(secret, commandPayload(c), c.Sig) {
		return fmt.Errorf("bad signature")
	}
	if skew := time.Since(time.Unix(c.TS, 0)); skew > MaxClockSkew || skew < -MaxClockSkew {
		return fmt.Errorf("stale timestamp (%s off)", skew.Round(time.Second))
	}
	return nil
}

func SignResult(secret string, r *Result) { r.Sig = Sign(secret, resultPayload(*r)) }

func VerifyResult(secret string, r Result) bool { return checkSig(secret, resultPayload(r), r.Sig) }

func SignHeartbeat(secret string, h *Heartbeat) { h.Sig = Sign(secret, heartbeatPayload(*h)) }

func VerifyHeartbeat(secret string, h Heartbeat) bool {
	return checkSig(secret, heartbeatPayload(h), h.Sig)
}

// shellMetachars matches anything that could chain or redirect commands.
// In read-only mode a matched command is rejected outright.
var shellMetachars = regexp.MustCompile(`[;&|<>$` + "`" + `\\(\)\n\r]`)

// deniedSubstrings blocks allowlist bypasses that write to disk or expose
// credentials, no matter which allowlisted command carries them
// (e.g. `cat ~/.qcs-secret`, `curl -s -o /tmp/x`, `aws s3api get-object`,
// IMDS credential paths).
var deniedSubstrings = []string{
	"qcs-secret",
	".aws/",
	".ssh/",
	".kube/config",
	"id_rsa",
	"id_ed25519",
	".pem",
	".key",
	"credentials",
	"get-object",
	"download",
	"export",
}

var readOnlyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s*kubectl\s+(get|describe|logs|top|api-resources|api-versions|cluster-info|version|config\s+current-context)\b`),
	regexp.MustCompile(`^\s*eksctl\s+(get|utils\s+describe|version)\b`),
	regexp.MustCompile(`^\s*aws\s+[\w-]+\s+(describe|list|get|lookup|query|search|check|head|batch-get|ls)[\w-]*(\s|$)`),
	regexp.MustCompile(`^\s*aws\s+sts\s+get-caller-identity\b`),
	regexp.MustCompile(`^\s*(ping|dig|nslookup|host|traceroute|uname|date|uptime|df\s|free\s|cat\s|ls\s|pwd|whoami)`),
}

// curl gets its own flag-level check instead of a regex on the first flag
// cluster: `curl -s -o /tmp/x <url>` used to slip through and write files.
const curlAllowedShort = "sSIkLfH"

var curlAllowedLong = map[string]bool{
	"--silent": true, "--show-error": true, "--head": true,
	"--insecure": true, "--location": true, "--fail": true,
}

var curlStart = regexp.MustCompile(`^\s*curl(\s|$)`)

func checkCurlFlags(cmd string) error {
	fields := strings.Fields(cmd)
	for _, f := range fields[1:] {
		if !strings.HasPrefix(f, "-") || f == "-" {
			continue
		}
		if strings.HasPrefix(f, "--") {
			if !curlAllowedLong[f] {
				return fmt.Errorf("curl option %q is not allowed in read-only mode", f)
			}
			continue
		}
		for _, r := range f[1:] {
			if !strings.ContainsRune(curlAllowedShort, r) {
				return fmt.Errorf("curl flag -%c is not allowed in read-only mode", r)
			}
		}
	}
	return nil
}

// CheckReadOnly enforces the allowlist and rejects shell chaining.
func CheckReadOnly(cmd string) error {
	if shellMetachars.MatchString(cmd) {
		return fmt.Errorf("shell metacharacters (; & | < > $ ` ( ) \\ newline) are not allowed in read-only mode")
	}
	for _, s := range deniedSubstrings {
		if strings.Contains(cmd, s) {
			return fmt.Errorf("command references %q, which is denied in read-only mode", s)
		}
	}
	if curlStart.MatchString(cmd) {
		return checkCurlFlags(cmd)
	}
	for _, p := range readOnlyPatterns {
		if p.MatchString(cmd) {
			return nil
		}
	}
	return fmt.Errorf("command not in read-only allowlist (allowed: kubectl/eksctl read verbs, aws describe/list/get/..., basic diagnostics)")
}

const truncSuffix = "\n...[truncated]"

func capString(s string, budget int) (string, bool) {
	if budget < 0 {
		budget = 0
	}
	if len(s) <= budget {
		return s, false
	}
	if budget <= len(truncSuffix) {
		return s[:budget], true
	}
	return s[:budget-len(truncSuffix)] + truncSuffix, true
}

// Truncate caps stdout/stderr so the JSON message stays under MaxOutputBytes.
func Truncate(stdout, stderr string) (string, string, bool) {
	out, t1 := capString(stdout, MaxOutputBytes)
	errStr, t2 := capString(stderr, MaxOutputBytes-len(out))
	return out, errStr, t1 || t2
}
