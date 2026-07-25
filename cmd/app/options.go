package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/tab58/tenzing-agent-harness/internal/features/budgets"
	"github.com/tab58/tenzing-agent-harness/internal/features/mcp"
	"github.com/tab58/tenzing-agent-harness/internal/harness"
)

// cliConfig holds every flag value after cobra parsing and env merging.
// *Set fields record whether the flag was explicitly passed (cobra Changed) —
// needed where "unset" and "zero" mean different things.
type cliConfig struct {
	// mode
	Prompt       string
	OutputFormat string // "text" | "json"
	ListModels   bool

	// models
	Model           string
	SubagentModel   string
	BlackboardModel string
	AdvisorModel    string

	// budgets
	MaxTokens     int64
	MaxIterations int
	MaxWallClock  time.Duration

	// toggles
	SubagentDepth      int
	SubagentDepthSet   bool
	NoBlackboard       bool
	ApprovalTimeout    time.Duration
	ApprovalTimeoutSet bool
	NoPermissions      bool

	// wiring
	MCPServers     []string
	ConversationID string

	// serve
	Port        int
	NexusConfig string
	Debug       bool
}

// parseMCPServer parses "name=command arg1 arg2" into an mcp.ServerConfig.
func parseMCPServer(s string) (mcp.ServerConfig, error) {
	name, rest, found := strings.Cut(s, "=")
	if !found || name == "" {
		return mcp.ServerConfig{}, fmt.Errorf("mcp server %q: want format name=command [args...]", s)
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return mcp.ServerConfig{}, fmt.Errorf("mcp server %q: empty command", s)
	}
	return mcp.ServerConfig{Name: name, Command: fields[0], Args: fields[1:]}, nil
}

// harnessOptions maps flag values to harness options shared by serve and
// print modes. Mode-specific options (event bus, delta handlers, nexus
// tools, print-mode approval default) are appended by the caller.
func harnessOptions(cfg *cliConfig) ([]harness.HarnessOption, error) {
	var opts []harness.HarnessOption

	if cfg.SubagentModel != "" {
		def, err := resolveModel(cfg.SubagentModel)
		if err != nil {
			return nil, fmt.Errorf("--subagent-model: %w", err)
		}
		opts = append(opts, harness.WithSubagentModel(def))
	}
	if cfg.BlackboardModel != "" {
		def, err := resolveModel(cfg.BlackboardModel)
		if err != nil {
			return nil, fmt.Errorf("--blackboard-model: %w", err)
		}
		opts = append(opts, harness.WithBlackboardModel(def))
	}
	if cfg.AdvisorModel != "" {
		def, err := resolveModel(cfg.AdvisorModel)
		if err != nil {
			return nil, fmt.Errorf("--advisor-model: %w", err)
		}
		opts = append(opts, harness.WithAdvisorModel(def))
	}

	if cfg.MaxTokens != 0 || cfg.MaxIterations != 0 || cfg.MaxWallClock != 0 {
		opts = append(opts, harness.WithBudgets(budgets.Limits{
			MaxIterations: cfg.MaxIterations,
			MaxWallClock:  cfg.MaxWallClock,
			MaxTokens:     cfg.MaxTokens,
		}))
	}

	if cfg.SubagentDepthSet {
		opts = append(opts, harness.WithSubagentDepth(cfg.SubagentDepth))
	}
	if cfg.NoBlackboard {
		opts = append(opts, harness.WithBlackboardDisabled())
	}
	if cfg.ApprovalTimeoutSet {
		opts = append(opts, harness.WithApprovalTimeout(cfg.ApprovalTimeout))
	}
	if cfg.NoPermissions {
		opts = append(opts, harness.WithPermissionsDisabled())
	}
	if cfg.ConversationID != "" {
		opts = append(opts, harness.WithConversationID(cfg.ConversationID))
	}

	for _, s := range cfg.MCPServers {
		sc, err := parseMCPServer(s)
		if err != nil {
			return nil, err
		}
		opts = append(opts, harness.WithMCPServer(sc))
	}

	return opts, nil
}
