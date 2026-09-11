package config

import (
	"os"
	"testing"
)

func TestParseSecretBare(t *testing.T) {
	s, err := ParseSecret("  deadbeef\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Value != "deadbeef" || s.Region != "" {
		t.Fatalf("got %+v", s)
	}
}

func TestParseSecretToken(t *testing.T) {
	s, err := ParseSecret(FormatToken("us-west-1", "deadbeef") + "\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Value != "deadbeef" || s.Region != "us-west-1" {
		t.Fatalf("got %+v", s)
	}
}

func TestResolveRegionTokenBeatsEnvAndFile(t *testing.T) {
	t.Setenv("AWS_REGION", "sa-east-1")
	s, err := ParseSecret(FormatToken("us-west-1", "deadbeef"))
	if err != nil {
		t.Fatal(err)
	}
	f := File{"AWS_REGION": "eu-west-1"}
	if got := ResolveRegion(s, f); got != "us-west-1" {
		t.Errorf("token region must win, got %q", got)
	}
}

func TestResolveRegionBareSecretFallsBack(t *testing.T) {
	// File.Get reads the environment first, so clear the developer's ambient
	// region to test the file fallback in isolation.
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	s, err := ParseSecret("deadbeef")
	if err != nil {
		t.Fatal(err)
	}
	if got := ResolveRegion(s, File{"AWS_REGION": "eu-west-1"}); got != "eu-west-1" {
		t.Errorf("bare secret should fall back to file, got %q", got)
	}
	t.Setenv("AWS_DEFAULT_REGION", "ap-south-1")
	if got := ResolveRegion(s, File{}); got != "ap-south-1" {
		t.Errorf("bare secret should fall back to env, got %q", got)
	}
}

func TestPinRegionOverwritesEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "sa-east-1")
	t.Setenv("AWS_DEFAULT_REGION", "sa-east-1")
	if err := PinRegion("us-west-1"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"AWS_REGION", "AWS_DEFAULT_REGION"} {
		if got := os.Getenv(k); got != "us-west-1" {
			t.Errorf("%s = %q, want us-west-1", k, got)
		}
	}
}

func TestPinRegionEmptyIsNoop(t *testing.T) {
	t.Setenv("AWS_REGION", "sa-east-1")
	if err := PinRegion(""); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("AWS_REGION"); got != "sa-east-1" {
		t.Errorf("empty region must not clobber env, got %q", got)
	}
}

func TestParseSecretRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "qcs1:", "qcs1:us-west-1", "qcs1::deadbeef", "qcs1:us-west-1:"} {
		if _, err := ParseSecret(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}
