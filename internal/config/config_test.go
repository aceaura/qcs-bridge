package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesKeyValue(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	content := "# comment\nQCS_CMD_QUEUE=qcs-commands.fifo\n\nQCS_RESULT_QUEUE = qcs-results.fifo \nbad-line\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QCS_CONFIG", p)

	f, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Get("QCS_CMD_QUEUE"); got != "qcs-commands.fifo" {
		t.Errorf("QCS_CMD_QUEUE = %q", got)
	}
	if got := f.Get("QCS_RESULT_QUEUE"); got != "qcs-results.fifo" {
		t.Errorf("QCS_RESULT_QUEUE = %q (whitespace not trimmed?)", got)
	}
	if got := f.Get("MISSING"); got != "" {
		t.Errorf("MISSING should be empty, got %q", got)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	os.WriteFile(p, []byte("QCS_CMD_QUEUE=from-file\n"), 0o600)
	t.Setenv("QCS_CONFIG", p)
	t.Setenv("QCS_CMD_QUEUE", "from-env")

	f, _ := Load()
	if got := f.Get("QCS_CMD_QUEUE"); got != "from-env" {
		t.Errorf("env should win, got %q", got)
	}
}

func TestMissingFileIsEmpty(t *testing.T) {
	t.Setenv("QCS_CONFIG", filepath.Join(t.TempDir(), "nope"))
	f, err := Load()
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if got := f.Get("ANYTHING"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}
