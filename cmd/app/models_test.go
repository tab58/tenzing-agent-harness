package main

import (
	"strings"
	"testing"

	"github.com/tab58/tenzing-agent-harness/pkg/tenzing"
)

func TestResolveModel(t *testing.T) {
	known := tenzing.Ollama_GLM5_2_Cloud
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"known model by its own key", modelKey(known), false},
		{"case-insensitive", strings.ToUpper(modelKey(known)), false},
		{"unknown model", "ollama/does-not-exist", true},
		{"unknown provider", "nope/whatever", true},
		{"malformed no slash", "justaname", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveModel(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveModel(%q) = %+v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveModel(%q) error: %v", tt.in, err)
			}
			if got.Name != known.Name || got.Provider != known.Provider {
				t.Errorf("resolveModel(%q) = %s/%s, want %s/%s", tt.in, got.Provider, got.Name, known.Provider, known.Name)
			}
		})
	}
}

func TestResolveModelErrorListsValidNames(t *testing.T) {
	_, err := resolveModel("nope/whatever")
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), modelKey(tenzing.Ollama_GLM5_2_Cloud)) {
		t.Errorf("error should list valid models, got: %v", err)
	}
}

func TestModelList(t *testing.T) {
	out := modelList()
	if !strings.Contains(out, modelKey(tenzing.Anthropic_ClaudeOpus4_6)) {
		t.Errorf("modelList missing anthropic entry:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !sortedStrings(lines) {
		t.Error("modelList not sorted")
	}
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] < s[i-1] {
			return false
		}
	}
	return true
}
