package nexus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "nexus.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(cfg.Channels) != 0 {
		t.Errorf("want 0 channels, got %d", len(cfg.Channels))
	}
}

func TestLoadConfigValid(t *testing.T) {
	p := writeTemp(t, `
channels:
  - name: api-logs
    type: file-tail
    path: /var/log/api.log
  - name: docker-web
    type: command
    cmd: ["docker", "logs", "-f", "web"]
    error_pattern: "(?i)fatal"
    buffer_size: 50
    trigger: false
  - name: hooks
    type: webhook
`)
	cfg, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Channels) != 3 {
		t.Fatalf("want 3 channels, got %d", len(cfg.Channels))
	}

	c0 := cfg.Channels[0]
	if c0.ErrorPattern != DefaultErrorPattern {
		t.Errorf("default error_pattern not applied: %q", c0.ErrorPattern)
	}
	if c0.BufferSize != DefaultBufferSize {
		t.Errorf("default buffer_size not applied: %d", c0.BufferSize)
	}
	if !c0.TriggerEnabled() {
		t.Error("trigger should default to true")
	}

	c1 := cfg.Channels[1]
	if c1.BufferSize != 50 || c1.ErrorPattern != "(?i)fatal" {
		t.Errorf("explicit fields overridden: %+v", c1)
	}
	if c1.TriggerEnabled() {
		t.Error("trigger: false not honored")
	}
}

func TestLoadConfigInvalid(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{"duplicate names", `
channels:
  - {name: a, type: webhook}
  - {name: a, type: webhook}
`, "duplicate"},
		{"unknown type", `
channels:
  - {name: a, type: carrier-pigeon}
`, "type"},
		{"file-tail missing path", `
channels:
  - {name: a, type: file-tail}
`, "path"},
		{"command missing cmd", `
channels:
  - {name: a, type: command}
`, "cmd"},
		{"bad regex", `
channels:
  - {name: a, type: webhook, error_pattern: "(unclosed"}
`, "error_pattern"},
		{"bad name chars", `
channels:
  - {name: "a b/c", type: webhook}
`, "name"},
		{"empty name", `
channels:
  - {name: "", type: webhook}
`, "name"},
		{"negative buffer", `
channels:
  - {name: a, type: webhook, buffer_size: -1}
`, "buffer_size"},
		{"not yaml", `{{{`, "parse"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(writeTemp(t, tt.yaml))
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}
