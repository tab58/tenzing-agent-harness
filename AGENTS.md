# Agent Guidelines — tenzing-agent-harness

Go module: `tenzing-agent`

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

- Module path: `tenzing-agent`
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

RLM offloading: when a single input exceeds half the compression threshold, `DoReasoning` routes it through an injected `OffloadFn` before appending to history. The function is `rlm.Engine.Run`, injected at the `cmd/` layer to respect layer boundaries.

### RLM fetchers must not compress

The RLM loop uses `rlm.NewSimpleFetcherFactory` (plain message slice) everywhere. Do not reintroduce a compressing fetcher backed by `internal/agent/context`. Two reasons, learned the hard way (2026-07):

1. **Compression is the wrong tool inside an RLM.** Per the RLM paper (`docs/recursive-language-models.pdf`, Alg. 1 + §2), sub-loop history is bounded *structurally*: the huge input lives in the REPL as a variable, each turn appends only the code block plus truncated stdout (`TruncateMax`, default 2000 chars), and `MaxIterations` caps the loop. Mid-loop summarization is lossy and can garble the model's memory of which REPL variables hold what — the loop's entire working state. The paper benchmarks compaction as a baseline and beats it by 26% median.
2. **Shared memory-file contamination.** `agentctx.NewContext` always loads `.agent_memory.md` on construction and overwrites it on compression. A compressing RLM fetcher therefore inherited the main agent's persisted session summary into every throwaway sub-loop, and — worse — clobbered the main agent's memory file with sub-loop summaries.

If RLM history growth ever becomes a real problem, tighten `TruncateMax`/`MaxIterations` on the engine; don't add compression.

## Adding Tools

1. Create `internal/harness/tools/tooldef/tool_<name>.go`
2. Implement `tooldef.Definition` interface: `Name()`, `Description()`, `Schema()`, `Execute()`
3. Register: most tools go in `GetDefaultToolDefs()` in `internal/harness/tools/registry.go`. The `rlm` tool is always registered by `harness.New()`; its LLM falls back to the main model unless `WithRLMModel` is set
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

`harness.New(mainModel common.ModelDefinition, opts ...HarnessOption) (*Harness, error)` is the harness constructor. The brain defaults to the built-in agent implementation (`internal/agent`); override with `WithAgentBuilder`.

`HarnessConfig` no longer exists. Behavior is configured via flat `HarnessOption` functions (`internal/harness/harness_options.go`):

- `WithAgentBuilder` — replaces the default agent implementation with a custom `runner.AgentBuilder` (the test seam for stub brains).
- `WithSubagentModel` / `WithRLMModel` / `WithAdvisorModel` — per-role `common.ModelDefinition`. An unset role model falls back to the main model. The advisor tool is registered only when `WithAdvisorModel` is set (no advisor by default).
- `WithLLMFactory` — replaces the default env-var-based LLM factory entirely; the test seam for injecting fakes.
- `WithProviderBaseURL(provider, url)` — per-provider base URL override consumed by the default factory only (ignored when `WithLLMFactory` is set).
- `WithTool` — injects an additional tool implementing `tooldef.Definition` (used by cmd/app to register nexus channel tools).
- Subagents (`spawn_agent` tool) are enabled by default at depth 1 using the main model; `WithSubagentDepth(0)` disables the tool.

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
| RLM engine               | `internal/harness/rlm/` (Fetcher, Querier, Engine)    |
| Context management       | `internal/agent/context/` (compression, task graph)   |
| Context overflow router  | `internal/agent/context/compressor/router.go`         |
| App (HTTP/SSE server)    | `cmd/app/`                                            |
| Test files               | Same directory as source, `*_test.go`                 |
| Shared test helpers      | `**/testutil_test.go`                                 |
| Sub-agent system         | `internal/harness/subagent/`                          |
| Event system             | `internal/harness/events/`                            |
| Embedded assets          | Adjacent to consumer (e.g. `rlm/bootstrap.py`)       |
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
