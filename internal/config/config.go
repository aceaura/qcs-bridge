// Package config loads qcs-bridge settings from a local KEY=VALUE file
// (~/.qcs/config by default, overridable via QCS_CONFIG).
// Environment variables always take precedence over file values.
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// File holds parsed KEY=VALUE settings.
type File map[string]string

// Path returns the config file location.
func Path() string {
	if p := os.Getenv("QCS_CONFIG"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".qcs", "config")
}

// Load reads the config file. A missing file yields an empty (valid) File.
func Load() (File, error) {
	f := File{}
	b, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		f[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return f, nil
}

// Get returns the first non-empty value for keys: environment first, then file.
func (f File) Get(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	for _, k := range keys {
		if v := f[k]; v != "" {
			return v
		}
	}
	return ""
}
