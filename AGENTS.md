# Agent Guidelines — tenzing-agent-harness

Go module: `github.com/tab58/tenzing-agent-harness`

## Key Docs

- `SYSTEM_ARCHITECTURE.md` — source of truth for system design. Update when code changes affect harness structure, agent interface, tool system, provider layer, or config surface.

Project-specific rules for AI agents working in this codebase. General behavioral guidelines live in `CLAUDE.md`.

## Build & Test

```bash
go build ./...          # build
go test ./...           # unit tests
go test -race ./...     # race detector
go vet ./...            # static analysis
task app                # run the app (HTTP/SSE server)
```

No CI pipeline yet. Run `go build ./...` and `go test ./...` before declaring work done.

## Module & Imports

- Module path: `github.com/tab58/tenzing-agent-harness`
- Use short import aliases only when needed to disambiguate
- Internal packages import via `tenzing-agent/internal/...`

## Layer Boundaries

Three layers, strict dependency direction: **Harness → AgentRunner → Agent**.

| Layer                                                          | Knows about                              | Does NOT know about                   |
| -------------------------------------------------------------- | ---------------------------------------- | ------------------------------------- |
| Harness (`harness.go`, `harness_options.go`, `llm.go`)         | AgentRunner, EventBus, LLM construction/caching, config wiring | Tool implementations                  |
| AgentRunner (`agent_runner.go`, `loop_fsm.go`)                 | Agent interface, tool registry, FSM, Emitter interface | CLI, sessions, users                  |
| Agent (`internal/agent/agent.go`, `internal/llmctx/`)          | LLM provider, message types, compression | Filesystem, processes, tools directly |

**Never import upward.** Tools don't import harness. Agent doesn't import runner. If you need cross-layer communication, use an interface injected via config.

One deliberate exception to "never import upward": `harness.New` imports `internal/agent` in exactly one place — the unexported `defaultAgentBuilder` fallback — so a harness works out of the box with no brain injection. All other harness code talks only to the `runner.Agent` interface. Callers with a custom brain override it via `harness.WithAgentBuilder(builder)`.

### Blackboard REPL

`internal/harness/blackboard/` hosts the sandboxed Python REPL subprocess machinery (`repl.go`, `bootstrap.py`) plus the `Querier` interface and its LLM-backed implementation (`querier.go`) — the model-facing `rlm` tool and its offload path have been removed — alongside the `Blackboard` that builds on it: one persistent REPL per harness, shared by the main agent and all subagents through the `repl` tool. A single mutex serializes all access; write-own-slot/read-anything is enforced by prompt convention, not code. The blackboard's helpers (`bb`, `peek`, `bb_grep`) are injected via a setup exec (no blackboard-specific logic in `bootstrap.py`); `bootstrap.py`'s only related feature is a transport-level stdout cap (100k chars).

Known limit: `llm_query` inside the blackboard holds the REPL lock for all agents while it runs; keep individual calls small and prefer `llm_batch` for fan-out work. If this hurts, the upgrade path is an async callback queue — don't reach for it speculatively.

Cancellation or transport failure mid-call resets the blackboard (contents lost, lazily restarted empty); agents must tolerate missing slots.

## Adding Tools

1. Create `internal/harness/tools/tooldef/tool_<name>.go`
2. Implement `tooldef.Definition` interface: `Name()`, `Description()`, `Schema()`, `Execute()`
3. Register: most tools go in `GetDefaultToolDefs()` in `internal/harness/tools/registry.go`. The `repl` tool (shared blackboard) is registered by `harness.New()` unless `WithBlackboardDisabled` is set; its sub-LM queries fall back to the main model unless `WithBlackboardModel` is set
4. Tool descriptions are **instructions to the model**, not documentation — precise wording controls tool selection
5. Tools never throw. Errors return `ToolResult{IsError: true}`. Loop doesn't break on tool errors

If the tool needs external state (skill registry, task graph), define a narrow interface in tooldef and accept it in the constructor. Don't import the concrete type.

## Adding Skills

1. Create `skills/<name>/SKILL.md` with YAML frontmatter:
   ```yaml
   ---
   name: skill-name
   description: One-line description
   ---
   ```
2. Body is loaded lazily via `load_skill` tool — no registration code needed
3. Skill metadata is discovered at startup from frontmatter only

## Providers

LLM providers live in the external module `github.com/tab58/llm-providers` (Anthropic, OpenAI, Cerebras, Lightning, OpenRouter, Ollama). This repo imports:

- `github.com/tab58/llm-providers/common` — canonical types (`common.LLM`, `CompletionRequest`, `Message`, `ContentBlock`, `ToolDefinition`) used throughout the harness
- `github.com/tab58/llm-providers` (root package, aliased `provider`) — `provider.LLMFromEnv(model, opts...)` is `harness.New`'s default LLM factory. It resolves the API key from the provider's conventional env var (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `CEREBRAS_API_KEY`, `LIGHTNING_API_KEY`, `OPENROUTER_API_KEY`; Ollama is keyless, `OLLAMA_API_KEY` optional) and dispatches to the matching provider's `NewClient`. `cmd/` mains no longer construct provider clients directly — they pass a `common.ModelDefinition` to `harness.New` and let the default factory resolve credentials. Override with `harness.WithLLMFactory` (custom key sourcing / tests) or `harness.WithProviderBaseURL` (per-provider base URL, default factory only).

Models are `common.Model` values (`common.ModelDefinition{Name, MaxTokens, ContextWindowSize, DefaultContextWindow, Provider}`), not strings. To add or change a provider, change the external module and bump the dependency.

## Constructing a Harness

`harness.New(mainModel common.ModelDefinition, opts ...HarnessOption) (*Harness, error)` is the harness constructor. External consumers use the public facade `pkg/tenzing` — pure type/func aliases over `internal/harness` (plus the `runner`, `tooldef`, and `events` types its options reference) exposing `tenzing.New` + `Harness.RunTurn` for a single programmatic loop. `pkg/tenzing/models.go` re-exports `common.Model`/`ModelDefinition`/`Provider` and the standard llm-providers models (provider-prefixed, asserted to `ModelDefinition`). `pkg/tenzing/llm.go` re-exports the LLM client layer so consumers never import llm-providers directly: `LLM` (= `common.LLM`) and every type its methods touch (`CompletionRequest`/`CompletionResponse`, `ContentBlock`, `Message`, role/content-type/streaming/stop-reason types with their consts, `Usage`/`TokenCount`/`ModelInfo`), message/content constructors, `CombinedText`, the sentinel errors, and the client constructors `LLMFromModel` (explicit API key) / `LLMFromEnv` with `ClientOption`/`WithBaseURL`. Naming: `common.ToolDefinition` is aliased as `LLMToolDefinition` because `tenzing.ToolDefinition` is the harness-side `tooldef.Definition`. New harness options, Hooks event types, llm-providers standard models, or additions to the `common.LLM` surface must be re-exported there in the same change. The brain defaults to the built-in agent implementation (`internal/agent`); override with `WithAgentBuilder`.

`HarnessConfig` no longer exists. Behavior is configured via flat `HarnessOption` functions (`internal/harness/harness_options.go`):

- `WithAgentBuilder` — replaces the default agent implementation with a custom `runner.AgentBuilder` (the test seam for stub brains).
- `WithSubagentModel` / `WithBlackboardModel` / `WithAdvisorModel` — per-role `common.ModelDefinition`. An unset role model falls back to the main model. The advisor tool is registered only when `WithAdvisorModel` is set (no advisor by default).
- `WithLLMFactory` — replaces the default env-var-based LLM factory entirely; the test seam for injecting fakes.
- `WithProviderBaseURL(provider, url)` — per-provider base URL override consumed by the default factory only (ignored when `WithLLMFactory` is set).
- `WithTool` — injects an additional tool implementing `tooldef.Definition` (used by cmd/app to register nexus channel tools).
- Subagents (`spawn_agent` tool) are enabled by default at depth 1 using the main model; `WithSubagentDepth(0)` disables the tool.
- `WithBlackboardDisabled` — the shared blackboard REPL is on by default: main agent and subagents share one persistent Python process (`repl` tool); subagent results over 2000 chars are deposited to `bb['<agent_id>']['result']` and returned as a 1500/500-char head/tail preview. Blackboard execs/deposits are logged via `slog` at info level (code capped 500 chars, stdout head 200).

LLM clients are cached per (provider, model, base URL) inside `harness.New`, so roles sharing a model definition share one client.

## Configuration & DI

All non-invariant behavior flows through `AgentRunnerConfig`. To change runner behavior:

- Swap the Agent (different model/provider)
- Swap the ToolRegistry (different tool set)
- Swap the SystemPrompt
- Swap the ReminderBuilder
- Provide an `events.Emitter` to receive structured events from the loop
- Provide `OnTextDelta`/`OnThinkingDelta` callbacks for streaming text

Don't modify the loop. Don't add fields to the runner struct. Configure via `AgentRunnerConfig`.

## FSM Rules

Six states, six transitions. Don't add states or transitions without updating `SYSTEM_ARCHITECTURE.md`.

The FSM is per-runner instance — subagents and concurrent loops don't share state.

## File Conventions

| Pattern                  | Location                                              |
| ------------------------ | ----------------------------------------------------- |
| Tool implementations     | `internal/harness/tools/tooldef/tool_*.go`            |
| Provider implementations | external: `github.com/tab58/llm-providers`            |
| Prompt templates         | `internal/harness/prompts/*.gotmpl`                   |
| REPL subprocess machinery | `internal/harness/blackboard/` (Python REPL, Querier) |
| Context management       | `internal/agent/context/` (compression, task graph)   |
| App (HTTP/SSE server)    | `cmd/app/`                                            |
| Public API facade        | `pkg/tenzing/` (aliases over `internal/harness`)      |
| Test files               | Same directory as source, `*_test.go`                 |
| Shared test helpers      | `**/testutil_test.go`                                 |
| Sub-agent system         | `internal/harness/subagent/`                          |
| Blackboard (shared REPL)  | `internal/harness/blackboard/` (persistent REPL, repl tool) |
| Event system             | `internal/harness/events/`                            |
| Embedded assets          | Adjacent to consumer (e.g. `blackboard/bootstrap.py`) |
| Nexus (input channels)   | `internal/app/nexus/`                                 |
| Nexus channel tools      | `internal/app/nexus/tools/`                           |
| App-level wiring helpers | `internal/app/` (log SSE broadcaster)                 |

## Testing

- Table-driven tests as standard pattern
- Test files live next to source
- Use `testutil_test.go` for shared helpers within a package
- Mock via interfaces, not concrete types
- `go test -race ./...` catches concurrency bugs — run before any change touching goroutines, channels, or shared state

## Common Mistakes to Avoid

- **Mutating the loop.** New capabilities = new tools or new config, never loop changes
- **Importing upward.** Tools → harness or agent → runner = architecture violation
- **Wide interfaces in tools.** Tools accept narrow interfaces, not concrete types
- **Hardcoding provider behavior.** All provider differences stay in the provider layer; canonical types above
- **Forgetting `SYSTEM_ARCHITECTURE.md`.** If your change affects harness structure, agent interface, tool system, provider layer, or config surface — update the architecture doc in the same PR
