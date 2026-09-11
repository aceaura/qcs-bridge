package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TokenPrefix marks a secret string that also carries the AWS region, so a
// single value is enough to configure a machine: qcs1:<region>:<hex-secret>.
const TokenPrefix = "qcs1:"

// Secret is a shared HMAC secret plus the region it was issued for.
// Region is empty for a bare (legacy) secret.
type Secret struct {
	Value  string
	Region string
}

// ParseSecret accepts either a bare secret or a qcs1:<region>:<secret> token.
func ParseSecret(s string) (Secret, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Secret{}, errors.New("empty secret")
	}
	if !strings.HasPrefix(s, TokenPrefix) {
		return Secret{Value: s}, nil
	}
	region, value, ok := strings.Cut(strings.TrimPrefix(s, TokenPrefix), ":")
	region, value = strings.TrimSpace(region), strings.TrimSpace(value)
	if !ok || region == "" || value == "" {
		return Secret{}, fmt.Errorf("malformed secret token: expected %s<region>:<secret>", TokenPrefix)
	}
	return Secret{Value: value, Region: region}, nil
}

// FormatToken builds the single-value token form of a secret.
func FormatToken(region, secret string) string {
	return TokenPrefix + region + ":" + secret
}

// ResolveRegion returns the region qcs-bridge must operate in.
//
// A region carried by the secret token wins over AWS_REGION and the config
// file: the secret is issued for one region's queues, so an unrelated region
// in the ambient environment (CloudShell exports the region of whichever
// session you happened to open) would only surface as NonExistentQueue.
func ResolveRegion(s Secret, f File) string {
	if s.Region != "" {
		return s.Region
	}
	return f.Get("AWS_REGION", "AWS_DEFAULT_REGION")
}

// PinRegion fixes the region for this process and everything it spawns by
// overwriting the AWS region environment variables, so an aws CLI invoked by a
// remote command cannot disagree with the region the bridge is talking to.
func PinRegion(region string) error {
	if region == "" {
		return nil
	}
	if err := os.Setenv("AWS_REGION", region); err != nil {
		return err
	}
	return os.Setenv("AWS_DEFAULT_REGION", region)
}

// SecretPath returns the secret file location.
func SecretPath() string {
	if f := os.Getenv("QCS_SECRET_FILE"); f != "" {
		return f
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".qcs-secret")
}

// LoadSecret reads the secret from QCS_SECRET, QCS_SECRET_FILE, or ~/.qcs-secret.
func LoadSecret() (Secret, error) {
	if s := os.Getenv("QCS_SECRET"); s != "" {
		return ParseSecret(s)
	}
	b, err := os.ReadFile(SecretPath())
	if err != nil {
		return Secret{}, errors.New("no secret found: set QCS_SECRET, QCS_SECRET_FILE, or ~/.qcs-secret")
	}
	return ParseSecret(string(b))
}
