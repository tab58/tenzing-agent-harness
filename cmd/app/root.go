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
			// Env fallback for env-configured settings, including the
			// models.yaml path and model/trust defaults.
			envCfg := &Config{}
			if err := config.Load(envCfg); err != nil {
				return fmt.Errorf("load env config: %w", err)
			}

			// Load models.yaml into the process-wide registry before any
			// model resolution so custom entries resolve everywhere.
			reg, err := loadModelRegistry(envCfg.ModelsConfig)
			if err != nil {
				return err
			}
			models = reg

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

			// Effective model precedence: --model flag > TENZING_MODEL env >
			// models.yaml default > compiled-in default (the flag default).
			if !cmd.Flags().Changed("model") {
				switch {
				case os.Getenv("TENZING_MODEL") != "":
					cfg.Model = envCfg.Model
				case models.defaultModel.Name != "":
					cfg.Model = modelKey(models.defaultModel.Provider, models.defaultModel.Name)
				}
			}
			// Validate the main model up front for both modes.
			if _, err := resolveModel(cfg.Model); err != nil {
				return err
			}

			markSetFlags(cfg, cmd.Flags().Changed)

			// Env fallback for the three pre-existing env vars.
			mergeEnv(cfg, envCfg, cmd.Flags().Changed, func(name string) bool {
				_, ok := os.LookupEnv(name)
				return ok
			})
			cfg.ModelsConfig = envCfg.ModelsConfig
			cfg.ProjectTrust = envCfg.ProjectTrust

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
			if cmd.Flags().Changed("timeout") {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: --timeout is ignored in serve mode")
			}
			return runServe(cmd.Context(), cfg)
		},
	}

	fl := cmd.Flags()
	fl.StringVarP(&cfg.Prompt, "prompt", "p", "", "run one headless agent turn with this prompt, then exit (@path.png args attach images)")
	fl.StringVar(&cfg.OutputFormat, "output-format", "text", "print-mode output: text (final answer) or json (JSONL events)")
	fl.BoolVar(&cfg.ListModels, "list-models", false, "print known models and exit")

	fl.StringVar(&cfg.Model, "model", modelKey(defaultModel.Provider, defaultModel.Name), "main model as provider/name (see --list-models)")
	fl.StringVar(&cfg.SubagentModel, "subagent-model", "", "model for subagents (default: main model)")
	fl.StringVar(&cfg.BlackboardModel, "blackboard-model", "", "model for blackboard llm_query (default: main model)")
	fl.StringVar(&cfg.AdvisorModel, "advisor-model", "", "model for the advisor tool (setting it enables the tool)")

	fl.Int64Var(&cfg.MaxTokens, "max-tokens", 0, "per-turn token budget, 0 = unlimited")
	fl.IntVar(&cfg.MaxIterations, "max-iterations", 0, "per-turn iteration budget, 0 = unlimited")
	fl.DurationVar(&cfg.MaxWallClock, "max-wall-clock", 0, "per-turn wall-clock budget, 0 = unlimited")

	fl.IntVar(&cfg.SubagentDepth, "subagent-depth", 1, "subagent nesting depth, 0 disables spawn_agent")
	fl.DurationVar(&cfg.ApprovalTimeout, "approval-timeout", 0, "tool-approval wait (serve default 120s; print default 0 = deny)")
	fl.BoolVar(&cfg.NoPermissions, "no-permissions", false, "disable permission gating entirely")
	fl.BoolVar(&cfg.SkipPermissions, "dangerously-skip-permissions", false, "auto-approve all approval prompts (sandboxes/pipelines)")
	fl.BoolVar(&cfg.ReadOnly, "read-only", false, "deny tools not marked read-only, no approval prompts")
	fl.BoolVar(&cfg.Thinking, "thinking", false, "model reasoning on or off (default: provider default)")
	fl.BoolVar(&cfg.NoSession, "no-session", false, "disable session persistence for this run")
	fl.BoolVar(&cfg.NoContextFiles, "no-context-files", false, "skip AGENTS.md context-file loading")

	fl.StringVar(&cfg.SystemFile, "system", "", "file whose contents replace the system prompt")
	fl.StringVar(&cfg.Resume, "resume", "", "resume the conversation with this ID (see GET /sessions or the session filenames)")
	fl.BoolVarP(&cfg.ContinueLatest, "continue", "c", false, "continue the most recent conversation for this directory")
	fl.BoolVar(&cfg.Trust, "trust", false, "treat the working directory as trusted for this run (loads ./SYSTEM.md, ./APPEND_SYSTEM.md, ./.tenzing/prompts); not persisted")
	fl.DurationVar(&cfg.Timeout, "timeout", 0, "print mode: abort the turn after this duration (e.g. 5m; 0 = no timeout)")

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
	cfg.ThinkingSet = changed("thinking")
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
