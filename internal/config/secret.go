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
