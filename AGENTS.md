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

Four layers, hexagonal dependency direction: **Core → Adapters → Extensions → Composition Root**.

| Layer                                                          | Knows about                              | Does NOT know about                   |
| -------------------------------------------------------------- | ---------------------------------------- | ------------------------------------- |
| Core (`internal/core/`)                                        | Domain types, port interfaces (`ModelPort`, `ToolPort`, `ContextPort`), FSM, events, extension/hook contracts, loop (`Loop`, `RunTurn`) | Everything outside `core` — adapters, extensions, harness, CLI |
| Adapters (`internal/adapters/`)                                | Core port interfaces; concrete implementations (`contextstore`, `toolport.Composite`/`Wrap`) | Harness, CLI, sessions                |
| Extensions (`internal/extensions/`, plus `subagent.SpawnExt`)  | Core capability interfaces (`ToolProvider`, `PromptContributor`, session/iteration/tool hooks) and whatever feature they wrap (skills registry, blackboard, subagent factory) | The loop's internals, other extensions |
| Composition root (`harness.go`, `harness_options.go`, `llm.go`; `AgentRunner` facade) | Everything below: builds adapters, default + user extensions, the composite ToolPort, assembles the system prompt, wires one runner per loop | Tool implementations' internals       |
| Agent (`internal/adapters/agent/agent.go`)                     | LLM provider, message types — stateless: receives the full message list and per-turn tool definitions on every `DoReasoning` call, returns the assistant message rather than storing it | Filesystem, processes, tools, skills, conversation history/compression |

**Never import upward.** Core imports nothing from `internal/`. Adapters import core, never harness. Tools don't import harness. Agent doesn't import runner. If you need cross-layer communication, use an interface injected via config.

One deliberate exception to "never import upward": `harness.New` imports `internal/adapters/agent` in exactly one place — the unexported `defaultAgentBuilder` fallback — so a harness works out of the box with no brain injection. All other harness code talks only to the `runner.Agent` interface. Callers with a custom brain override it via `harness.WithAgentBuilder(builder)`.

### Blackboard REPL

`internal/harness/blackboard/` hosts the sandboxed Python REPL subprocess machinery (`repl.go`, `bootstrap.py`) plus the `Querier` interface and its LLM-backed implementation (`querier.go`) — the model-facing `rlm` tool and its offload path have been removed — alongside the `Blackboard` that builds on it: one persistent REPL per harness, shared by the main agent and all subagents through the `repl` tool. A single mutex serializes all access; write-own-slot is enforced in code — `bb` is a guard dict and creating/replacing a top-level key other than the executing agent's slot raises `PermissionError` (reads are unrestricted; the trusted Go `Deposit` path bypasses the guard). The blackboard's namespace (`bb` guard dict, `peek`, `bb_grep`) is injected via a setup exec (no blackboard-specific logic in `bootstrap.py`); `bootstrap.py`'s only related feature is a transport-level stdout cap (100k chars).

Known limit: `llm_query` inside the blackboard holds the REPL lock for all agents while it runs; keep individual calls small and prefer `llm_batch` for fan-out work. If this hurts, the upgrade path is an async callback queue — don't reach for it speculatively.

Cancellation or transport failure mid-call resets the blackboard (contents lost, lazily restarted empty); agents must tolerate missing slots.

Registration is via `internal/extensions/blackboardext`: a `core.Extension` providing the `repl` tool (`core.ToolProvider`, wrapping the same REPL tool implementation) and closing the blackboard on `core.SessionEndHook` (run by `Harness.Shutdown` with a 5s timeout). The `*blackboard.Blackboard` is built at the composition root and shared — main gets `blackboardext.New(bb, "main")`, each subagent gets `New(bb, childID)`. Behavior unchanged.

## Adding Tools

All tools reach the model through the composite ToolPort (`internal/adapters/toolport/composite.go`), which mounts three source kinds in stable definition order: native registry tools (sorted by name), extension `core.ToolProvider` bundles (registration order), then `core.DynamicToolSource` snapshots (re-read at each turn's `BeginTurn`). Name collisions with an already-mounted tool are a construction error (dynamic collisions are skipped with a warning). The loop snapshots the port once per turn and passes the definitions into every `DoReasoning` call — the agent holds no tool state.

Two ways to add a tool:

1. **Native built-in** (file/shell-level tooling): create `internal/features/builtins/tool_<name>.go`, implement `tooldef.Definition` (`Name()`, `Description()`, `Schema()`, `Execute()`, from `internal/core/tooldef`), register it in the registry (built-ins in `NewRegistry`, or inject via `harness.WithTool`).
2. **Extension tool**: implement `core.ToolProvider` on an extension — return `core.ToolSpec`s carrying their own `Execute` closure and an `Origin` like `"extension:<name>"`. No registry registration needed.

Notes:

- Tool descriptions are **instructions to the model**, not documentation — precise wording controls tool selection
- Tools never throw. Errors return `ToolResult{IsError: true}`; panics are recovered into error results by `Composite.Execute`. Loop doesn't break on tool errors

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

UX unchanged; new plumbing: skills surface through `internal/extensions/skillsext` — a `core.Extension` providing the `list_skills`/`load_skill` tools (`core.ToolProvider`) and the "Available skills…" system-prompt index (`core.PromptContributor`, appended by the composition root). The agent knows nothing about skills. The same extension instance is shared with subagents via the factory config (read-only registry).

## MCP Servers

`harness.WithMCPServer(mcpext.ServerConfig{Name, Command, Args, Env})` (repeat per server; re-exported as `tenzing.WithMCPServer`/`tenzing.MCPServerConfig`) mounts an external MCP server over stdio. The `mcpext` extension (`internal/extensions/mcpext`) connects on session start (a dead server logs a warning and serves zero tools — deliberately NOT load-bearing), closes on session end, and implements `core.DynamicToolSource`: tools are re-listed at each turn's `BeginTurn` with a 30s cache and surface as `mcp__<server>__<tool>` with `Origin: "mcp:<server>"`. Trust: the default permission policy's `AskOrigins: ["mcp:"]` escalates every MCP-origin tool to approval unless its full name is explicitly allow-listed. SSE/HTTP transports and `listChanged` subscriptions are follow-ons.

## Providers

LLM providers live in the external module `github.com/tab58/llm-providers` (Anthropic, OpenAI, Cerebras, Lightning, OpenRouter, Ollama). This repo imports:

- `github.com/tab58/llm-providers/common` — canonical types (`common.LLM`, `CompletionRequest`, `Message`, `ContentBlock`, `ToolDefinition`) used throughout the harness
- `github.com/tab58/llm-providers` (root package, aliased `provider`) — `provider.LLMFromEnv(model, opts...)` is `harness.New`'s default LLM factory. It resolves the API key from the provider's conventional env var (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `CEREBRAS_API_KEY`, `LIGHTNING_API_KEY`, `OPENROUTER_API_KEY`; Ollama is keyless, `OLLAMA_API_KEY` optional) and dispatches to the matching provider's `NewClient`. `cmd/` mains no longer construct provider clients directly — they pass a `common.ModelDefinition` to `harness.New` and let the default factory resolve credentials. Override with `harness.WithLLMFactory` (custom key sourcing / tests) or `harness.WithProviderBaseURL` (per-provider base URL, default factory only).

Models are `common.Model` values (`common.ModelDefinition{Name, MaxTokens, ContextWindowSize, DefaultContextWindow, Provider}`), not strings. To add or change a provider, change the external module and bump the dependency.

## Constructing a Harness

`harness.New(mainModel common.ModelDefinition, opts ...HarnessOption) (*Harness, error)` is the harness constructor. External consumers use the public facade `pkg/tenzing` — pure type/func aliases over `internal/harness` (plus the `runner`, `tooldef`, and `events` types its options reference) exposing `tenzing.New` + `Harness.RunTurn` for a single programmatic loop. `pkg/tenzing/models.go` re-exports `common.Model`/`ModelDefinition`/`Provider` and the standard llm-providers models (provider-prefixed, asserted to `ModelDefinition`). `pkg/tenzing/llm.go` re-exports the LLM client layer so consumers never import llm-providers directly: `LLM` (= `common.LLM`) and every type its methods touch (`CompletionRequest`/`CompletionResponse`, `ContentBlock`, `Message`, role/content-type/streaming/stop-reason types with their consts, `Usage`/`TokenCount`/`ModelInfo`), message/content constructors, `CombinedText`, the sentinel errors, and the client constructors `LLMFromModel` (explicit API key) / `LLMFromEnv` with `ClientOption`/`WithBaseURL`. Naming: `common.ToolDefinition` is aliased as `LLMToolDefinition` because `tenzing.ToolDefinition` is the harness-side `tooldef.Definition`. New harness options, Hooks event types, llm-providers standard models, or additions to the `common.LLM` surface must be re-exported there in the same change. The brain defaults to the built-in agent implementation (`internal/adapters/agent`); override with `WithAgentBuilder`.

`HarnessConfig` no longer exists. Behavior is configured via flat `HarnessOption` functions (`internal/harness/harness_options.go`):

- `WithAgentBuilder` — replaces the default agent implementation with a custom `runner.AgentBuilder` (the test seam for stub brains).
- `WithSubagentModel` / `WithBlackboardModel` / `WithAdvisorModel` — per-role `common.ModelDefinition`. An unset role model falls back to the main model. The advisor tool is registered only when `WithAdvisorModel` is set (no advisor by default).
- `WithLLMFactory` — replaces the default env-var-based LLM factory entirely; the test seam for injecting fakes.
- `WithProviderBaseURL(provider, url)` — per-provider base URL override consumed by the default factory only (ignored when `WithLLMFactory` is set).
- `WithTool` — injects an additional tool implementing `tooldef.Definition` (used by cmd/app to register nexus channel tools).
- Subagents (`spawn_agent` tool) are enabled by default at depth 1 using the main model; `WithSubagentDepth(0)` disables the tool.
- `WithBlackboardDisabled` — the shared blackboard REPL is on by default: main agent and subagents share one persistent Python process (`repl` tool); subagent results over 2000 chars are deposited to `bb['<agent_id>']['result']` and returned as a 1500/500-char head/tail preview. Blackboard execs/deposits are logged via `slog` at info level (code capped 500 chars, stdout head 200).
- `WithBudgets(budgets.Limits{MaxIterations, MaxWallClock, MaxTokens})` — graceful per-turn termination (`terminated: <reason>` error from `RunTurn`); zero fields unlimited. Subagents get `MaxIterations` from `WithSubagentMaxIterations` (default 100) via the same extension.
- `WithMCPServer(cfg)` — mounts an external MCP server (see "MCP Servers" above); repeat per server.
- **Permissions (default-on):** the permissions extension gates tool calls by name — the default policy (`permissions.DefaultPolicy`) escalates code-executing/file-writing tools (`bash`, `write`, `edit`, `revert`, `repl`, `spawn_agent`, `advisor`) to `AskUser`; the loop then emits `ApprovalRequestedEvent` and blocks up to `WithApprovalTimeout` (default 120s; 0 = deny immediately) for a `Respond(bool)`. Override the policy with `WithPermissionPolicy`, opt out entirely with `WithPermissionsDisabled()`. Ordering guarantee: permissions run FIRST among extensions, and later hooks can escalate but never lower its decision. **Migration note:** consumers driving the harness headlessly (no `OnApprovalRequested` hook, no cmd/app UI) must either call `WithPermissionsDisabled()` or handle the approval event — otherwise mutating tools are denied after the approval timeout. Subagent loops carry no permissions extension (the parent's spawn was already approved).
- `WithConversationID(id)` — resumes a prior conversation: the main agent runs under `id` instead of a random one, and `harness.New` loads that conversation's latest persisted memory file (see below) into the main `contextstore.Store`'s `InitialMemory`, which seeds it as a synthetic user/assistant summary exchange before the fresh turn. Omit to start a new conversation under a random ID (`Harness.ConversationID()` returns it either way — the caller's handle for a future resume).

LLM clients are cached per (provider, model, base URL) inside `harness.New`, so roles sharing a model definition share one client.

Memory (`internal/harness/memory.go`) is separate from the in-process `contextstore.Store`: it's the harness's own `ContextCompressedEvent` subscriber, persisting each compaction's summary to a per-conversation file (`.agent_memory-<stamp>-<runnerID>.md`, main-agent files in the OS config dir, subagent files in the OS cache dir) so a later `WithConversationID` resume — even in a new process — has something to load. Neither `internal/core` nor `internal/adapters/contextstore` know this file-backed store exists; they only emit/consume the event.

## Configuration & DI

All non-invariant runner behavior flows through `runner.AgentRunnerOption` functional options (`internal/harness/runner/agent_runner.go`), passed to `runner.NewAgentRunner(agent, opts...)`. To change runner behavior:

- Swap the Agent (different model/provider)
- Swap the ToolRegistry (`WithToolRegistry`, different native tool set) or the whole ToolPort (`WithToolPort` — the harness passes the composite here; unset falls back to wrapping the registry)
- Swap the ContextStore (`WithContextStore`) — required; the `core.ContextPort` implementation that owns this runner's conversation history
- Swap the SystemPrompt (`WithSystemPrompt`)
- Provide a `core.Emitter` to receive structured events from the loop (`WithEmitter`)
- Provide `WithTextDeltaHandler`/`WithThinkingDeltaHandler` callbacks for streaming text

System reminders (todo state, etc.) no longer flow through the runner. They
are injected each iteration by `core.Extension`s implementing
`core.BeforeIterationHook` (e.g. `internal/extensions/reminders`), registered
on `*core.Extensions` and passed to the runner via `runner.WithExtensions`.
`harness.New` wires the default extension set — reminders, skills
(`skillsext`), blackboard (`blackboardext`, unless disabled), and subagents
(`subagent.SpawnExt`, when depth > 0) — then appends caller extensions from
`harness.WithExtension` (also re-exported as `tenzing.WithExtension` with the
`Extension`/hook interfaces and `ToolSpec`).

Don't modify the loop. Don't add fields to the `AgentRunner` struct. Configure via `runner.AgentRunnerOption`.

## FSM Rules

Six states, six transitions. Don't add states or transitions without updating `SYSTEM_ARCHITECTURE.md`.

The FSM lives in `internal/core/fsm.go` and is per-Loop instance — subagents and concurrent loops don't share state.

## File Conventions

| Pattern                  | Location                                              |
| ------------------------ | ----------------------------------------------------- |
| Core domain (types/FSM/events/loop/ports) | `internal/core/` |
| Port adapters (contextstore, toolport) | `internal/adapters/` |
| Extensions                | `internal/extensions/<name>/`                          |
| Tool authoring contract  | `internal/core/tooldef/tooldef.go`                    |
| Built-in tool implementations | `internal/features/builtins/tool_*.go`          |
| Provider implementations | external: `github.com/tab58/llm-providers`            |
| Prompt templates         | `internal/harness/prompts/*.gotmpl`                   |
| REPL subprocess machinery | `internal/harness/blackboard/` (Python REPL, Querier) |
| Context management       | `internal/adapters/contextstore/` (implements `core.ContextPort`: history, tool_use/tool_result pairing, compression) |
| App (HTTP/SSE server)    | `cmd/app/`                                            |
| Public API facade        | `pkg/tenzing/` (aliases over `internal/harness`)      |
| Test files               | Same directory as source, `*_test.go`                 |
| Shared test helpers      | `**/testutil_test.go`                                 |
| Sub-agent system         | `internal/harness/subagent/`                          |
| Blackboard (shared REPL)  | `internal/harness/blackboard/` (persistent REPL, repl tool) |
| Event system              | `internal/core/` (vocabulary: `Event`, `EventType`, `BaseEvent`, `Emitter`); `internal/adapters/eventbus/` (`EventBus`, `Hooks`) |
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
