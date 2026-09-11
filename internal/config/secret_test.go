package config

import "testing"

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

func TestParseSecretRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "qcs1:", "qcs1:us-west-1", "qcs1::deadbeef", "qcs1:us-west-1:"} {
		if _, err := ParseSecret(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}
