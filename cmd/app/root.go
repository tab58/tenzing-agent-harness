package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tab58/huma-http-server/config"
)

// exitCodeError carries a process exit code through cobra's error return.
type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }

func newRootCmd() *cobra.Command {
	cfg := &cliConfig{}

	cmd := &cobra.Command{
		Use:   "tenzing",
		Short: "Tenzing agent harness — HTTP/SSE server by default, one-shot agent turn with -p",
		Long: "Runs the Tenzing agent harness.\n\n" +
			"Without -p: serves the HTTP/SSE app (today's behavior).\n" +
			"With -p \"prompt\": runs a single headless agent turn and exits.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cfg.ListModels {
				fmt.Fprint(cmd.OutOrStdout(), modelList())
				return nil
			}
			if cmd.Flags().Changed("prompt") && cfg.Prompt == "" {
				return errors.New("-p requires a non-empty prompt")
			}
			if cfg.OutputFormat != "text" && cfg.OutputFormat != "json" {
				return fmt.Errorf("--output-format must be text or json, got %q", cfg.OutputFormat)
			}
			if cfg.Prompt == "" && cmd.Flags().Changed("output-format") {
				return errors.New("--output-format requires -p")
			}

			// Validate the main model up front for both modes.
			if _, err := resolveModel(cfg.Model); err != nil {
				return err
			}

			markSetFlags(cfg, cmd.Flags().Changed)

			// Env fallback for the three pre-existing env vars.
			envCfg := &Config{}
			if err := config.Load(envCfg); err != nil {
				return fmt.Errorf("load env config: %w", err)
			}
			mergeEnv(cfg, envCfg, cmd.Flags().Changed, func(name string) bool {
				_, ok := os.LookupEnv(name)
				return ok
			})

			if cfg.Prompt != "" {
				// Warns on explicit CLI flags only — SERVER_PORT/NEXUS_CONFIG
				// env vars merged above stay silent by design (ambient env
				// shouldn't nag every print run).
				for _, name := range []string{"port", "nexus-config"} {
					if cmd.Flags().Changed(name) {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: --%s is ignored in print mode\n", name)
					}
				}
				return runPrintFn(cmd.Context(), cfg, cmd.OutOrStdout(), cmd.ErrOrStderr())
			}
			return runServe(cmd.Context(), cfg)
		},
	}

	fl := cmd.Flags()
	fl.StringVarP(&cfg.Prompt, "prompt", "p", "", "run one headless agent turn with this prompt, then exit")
	fl.StringVar(&cfg.OutputFormat, "output-format", "text", "print-mode output: text (final answer) or json (JSONL events)")
	fl.BoolVar(&cfg.ListModels, "list-models", false, "print known models and exit")

	fl.StringVar(&cfg.Model, "model", modelKey(defaultModel), "main model as provider/name (see --list-models)")
	fl.StringVar(&cfg.SubagentModel, "subagent-model", "", "model for subagents (default: main model)")
	fl.StringVar(&cfg.BlackboardModel, "blackboard-model", "", "model for blackboard llm_query (default: main model)")
	fl.StringVar(&cfg.AdvisorModel, "advisor-model", "", "model for the advisor tool (setting it enables the tool)")

	fl.Int64Var(&cfg.MaxTokens, "max-tokens", 0, "per-turn token budget, 0 = unlimited")
	fl.IntVar(&cfg.MaxIterations, "max-iterations", 0, "per-turn iteration budget, 0 = unlimited")
	fl.DurationVar(&cfg.MaxWallClock, "max-wall-clock", 0, "per-turn wall-clock budget, 0 = unlimited")

	fl.IntVar(&cfg.SubagentDepth, "subagent-depth", 1, "subagent nesting depth, 0 disables spawn_agent")
	fl.BoolVar(&cfg.NoBlackboard, "no-blackboard", false, "disable the shared Python blackboard REPL")
	fl.DurationVar(&cfg.ApprovalTimeout, "approval-timeout", 0, "tool-approval wait (serve default 120s; print default 0 = deny)")
	fl.BoolVar(&cfg.NoPermissions, "no-permissions", false, "disable permission gating entirely")

	fl.StringArrayVar(&cfg.MCPServers, "mcp-server", nil, `mount an MCP server, repeatable: "name=command arg1 arg2"`)
	fl.StringVar(&cfg.ConversationID, "conversation-id", "", "resume a prior conversation's memory")

	fl.IntVar(&cfg.Port, "port", 8080, "serve-mode listen port (env SERVER_PORT)")
	fl.StringVar(&cfg.NexusConfig, "nexus-config", "nexus.yaml", "nexus channel config path (env NEXUS_CONFIG)")
	fl.BoolVar(&cfg.Debug, "debug", false, "trace-level logging to a fresh log file (env LOG_DEBUG)")

	return cmd
}

// markSetFlags records whether flags with "unset vs. zero" semantics were
// explicitly passed, so harnessOptions can tell a deliberate zero from a
// default.
func markSetFlags(cfg *cliConfig, changed func(name string) bool) {
	cfg.SubagentDepthSet = changed("subagent-depth")
	cfg.ApprovalTimeoutSet = changed("approval-timeout")
}

// mergeEnv applies env-var fallback: an env value wins over the flag default,
// but an explicitly passed flag wins over env. Only the three pre-existing
// env vars participate. present reports whether the named env var is
// actually set — config.Load applies struct-tag defaults (e.g. port 8080)
// to env unconditionally, so env.ServerPort != 0 is true even when
// SERVER_PORT was never set; present is the real signal.
func mergeEnv(cfg *cliConfig, env *Config, changed func(name string) bool, present func(name string) bool) {
	if !changed("port") && present("SERVER_PORT") {
		cfg.Port = env.ServerPort
	}
	if !changed("debug") && present("LOG_DEBUG") {
		cfg.Debug = env.LogDebug
	}
	if !changed("nexus-config") && present("NEXUS_CONFIG") {
		cfg.NexusConfig = env.NexusConfig
	}
}
