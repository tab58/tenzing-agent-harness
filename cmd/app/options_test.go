package main

import (
	"testing"
	"time"
)

func TestParseMCPServer(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantName string
		wantCmd  string
		wantArgs []string
		wantErr  bool
	}{
		{"command only", "fs=mcp-server-fs", "fs", "mcp-server-fs", nil, false},
		{"command with args", "gh=npx -y @mcp/github", "gh", "npx", []string{"-y", "@mcp/github"}, false},
		{"missing equals", "just-a-name", "", "", nil, true},
		{"empty name", "=cmd", "", "", nil, true},
		{"empty command", "name=", "", "", nil, true},
		{"empty string", "", "", "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMCPServer(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseMCPServer(%q) = %+v, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMCPServer(%q) error: %v", tt.in, err)
			}
			if got.Name != tt.wantName || got.Command != tt.wantCmd {
				t.Errorf("got %q/%q, want %q/%q", got.Name, got.Command, tt.wantName, tt.wantCmd)
			}
			if len(got.Args) != len(tt.wantArgs) {
				t.Fatalf("args = %v, want %v", got.Args, tt.wantArgs)
			}
			for i := range got.Args {
				if got.Args[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, got.Args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestHarnessOptions(t *testing.T) {
	tests := []struct {
		name    string
		cfg     cliConfig
		wantN   int // expected option count
		wantErr bool
	}{
		{"empty config", cliConfig{}, 0, false},
		{"budgets only", cliConfig{MaxTokens: 1000, MaxIterations: 5, MaxWallClock: time.Minute}, 1, false},
		{"toggles", cliConfig{NoBlackboard: true, NoPermissions: true, SubagentDepth: 2, SubagentDepthSet: true}, 3, false},
		{"approval timeout", cliConfig{ApprovalTimeout: 30 * time.Second, ApprovalTimeoutSet: true}, 1, false},
		{"conversation id", cliConfig{ConversationID: "abc"}, 1, false},
		{"one mcp server", cliConfig{MCPServers: []string{"fs=mcp-server-fs"}}, 1, false},
		{"bad mcp server", cliConfig{MCPServers: []string{"nope"}}, 0, true},
		{"bad role model", cliConfig{SubagentModel: "bogus/nope"}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := harnessOptions(&tt.cfg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("harnessOptions error: %v", err)
			}
			if len(got) != tt.wantN {
				t.Errorf("got %d options, want %d", len(got), tt.wantN)
			}
		})
	}
}
