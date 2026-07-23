# System Architecture

## Design Goals

**Four-layer hexagonal architecture: Core → Adapters → Extensions → Composition Root.**

- **Core** (`internal/core/`) — the invariant domain. Types (`ToolCall`, `ToolResult`, `ReasoningResult`, `ResponseMeta`), port interfaces (`ModelPort`, `ToolPort`, `ContextPort`), the FSM, events, the extension system (`Extensions`, hooks, `Decision`), and the agent loop (`Loop`, `RunTurn`). Core imports nothing from this repo's `internal/` tree — adapters and extensions import core, never the reverse.
- **Adapters** (`internal/adapters/`) — port implementations. `contextstore/` implements `ContextPort` (in-memory history, tool_use/tool_result pairing, compression). `toolport/` wraps `tools.Registry` as `ToolPort` (error-as-ToolResult conversion, panic recovery). The `Agent` interface in `runner/agent.go` extends `ModelPort` with construction-time wiring methods; the concrete agent (`internal/agent/`) implements it.
- **Extensions** (`internal/extensions/`) — optional hooks that plug into the loop via `core.Extensions` (e.g. `reminders/` for system reminders). They implement core hook interfaces (`BeforeIterationHook`, `ToolCallHook`, etc.) and are registered at construction.
- **Composition Root** — `harness.New` (the outermost shell) and `AgentRunner` (thin facade). The harness wires adapters, extensions, and tools, and assembles the system prompt (base + extension `PromptFragments`); `AgentRunner` performs one-time agent wiring (stream callbacks) and delegates to `core.Loop.RunTurn`, translating `TurnResult` into the `(string, error)` contract the harness expects.

The Harness creates an AgentRunner for the main session. A subagent spawns a fresh AgentRunner with its own Loop, FSM, and message history. This separation means you can swap the agent (different LLM, different strategy) without touching execution infrastructure, swap the harness (CLI → server, local → remote) without touching decision logic, and reuse the loop for subagents without coupling them to the CLI layer.

**Everything that isn't invariant is configurable.** The loop (perception → action → observation), the FSM, and the dispatch pattern (`name → handler(input)`) are structural invariants — they live in `core.Loop` and never change. Everything else is injectable: which model (via `ModelPort`), which tools (via `ToolPort`), which context store (via `ContextPort`), which extensions, which system prompt. This allows piece-by-piece optimization without touching the loop.

**Port interfaces are the DI surface.** All non-invariant loop behavior flows through `ModelPort`, `ToolPort`, `ContextPort`, and `Extensions`. The `AgentRunner` options (`WithToolRegistry`, `WithSkillsRegistry`, etc.) remain the external API but internally build the ports and delegate to `core.NewLoop`.

**The loop never changes.** Perception → action → observation is the single primitive. New capabilities are added by registering tools or wrapping the loop with new mechanisms (planning, subagents, context compression) — never by modifying the loop itself.

**Tools are the only extension point.** Adding a capability = one registry entry, zero loop changes. Tool descriptions are instructions, not documentation — precise wording controls model tool selection. Tools never throw; errors are strings the agent interprets.

**Provider agnosticism.** All LLM interaction flows through canonical types (`Message`, `ContentBlock`, `CompletionRequest/Response`). Provider implementations convert to/from SDK-specific types. Swapping providers requires zero changes above the provider layer.

**Risk changes the process.** *(IMPLEMENTED.)* The permissions extension (`internal/extensions/permissions`, a `core.ToolCallHook` registered FIRST in the default extension order) classifies each tool call by name: `Deny` > `Ask` > `Allow` > policy default. The default policy asks for anything that executes code or writes files (`bash`, `write`, `edit`, `revert`, `repl`, `spawn_agent`, `advisor`) and allows read-only/in-memory tools. `AskUser` makes the core loop emit `ApprovalRequestedEvent` and block (up to `ApprovalTimeout`, harness default 120s; timeout/cancel = deny) for a `Respond(bool)` — cmd/app forwards it over SSE as `approval_requested` and resolves it via `POST /approve {call_id, approved}` with approve/deny buttons in the embedded UI. Configure with `harness.WithPermissionPolicy`, opt out with `harness.WithPermissionsDisabled` (headless/trusted drivers), tune waiting with `harness.WithApprovalTimeout`. The check happens in the core loop before tool execution — not in the tool itself, not in the Agent.

**Long tasks have budgets.** *(IMPLEMENTED.)* The budgets extension (`internal/extensions/budgets`, a `core.BeforeIterationHook`) enforces per-turn limits — `MaxIterations`, `MaxWallClock`, `MaxTokens` (input+output cumulative); zero = unlimited. The loop populates `TurnContext.Elapsed/InputTokens/OutputTokens` before each iteration's hooks; when a limit trips the extension sets `Terminate` and the turn ends gracefully with `TurnResult{Terminated: reason}` — a result, not a crash. Wire via `harness.WithBudgets(limits)`; subagents get `MaxIterations` from `WithSubagentMaxIterations` (default 100) through the same extension. Cost budget (USD) is deferred — llm-providers exposes no pricing tables.

**Context is assembled, not dumped.** The system prompt is ordered by stability for cache efficiency: Layer 0 (system policies, stable prefix, cached) → Layer 1 (skill definitions, rarely change, cached) → Layer 2 (session instructions, per conversation, not cached) → Layer 3 (JIT-retrieved tool outputs, fresh, not cached). Untrusted data (user input, tool output from external sources) is marked with trust labels so the harness can treat it differently. *(Partially implemented — skills use progressive disclosure, but no cache-aware ordering or trust labels yet.)*

**Registries own implementations, Agent gets metadata.** Tools and skills follow the same wiring pattern: registries load from disk at startup, extensions surface them through the composite ToolPort and prompt fragments, and the core loop passes the per-turn tool definitions into every `DoReasoning` call. The Agent holds no capability state at all — it tells the LLM what capabilities exist (from the definitions handed to it per request), and the loop actually runs them.

```
main.go
  ├── skills.NewRegistry()              → empty registry; RegisterSkillDir(dir)
  │                                        scans each dir for skill metadata
  ├── tools.NewRegistry(cwd, defs...)   → holds tool implementations
  │     └── includes skill tools that reference skill registry
  │
  ├── Agent gets metadata only:
  │     ├── toolRegistry.ProviderDefinitions()  → what tools exist
  │     └── skillRegistry.Discover()            → what skills exist
  │
  └── AgentRunner gets registries for execution:
        └── toolRegistry                        → executes tool calls
```

---

Go module: `github.com/tab58/tenzing-agent-harness` (go 1.25.9)

```
cmd/app/main.go                         Entry point — signal handling, banner, exit codes
cmd/app/container.go                    AppContainer — config, logging, agent server + HTTP server wiring
cmd/app/server.go                       agentServer — routes (/query, /cancel, /info, /debug, /ingest/{name}), SSE broadcast, event forwarding
cmd/app/index.go                        Embedded chat UI (single-page HTML served at /)

internal/
├── core/                                Invariant domain: types, FSM, events, loop; imports nothing internal
│   ├── turn.go                         Domain types (ToolCall, ToolResult, ReasoningResult, ResponseMeta)
│   ├── ports.go                        Port interfaces (ModelPort, ToolPort, ContextPort)
│   ├── loop.go                         Loop struct, NewLoop, RunTurn — the invariant agent loop
│   ├── fsm.go                          Per-runner finite state machine (6 states, 6 transitions)
│   ├── extension.go                    Extensions, hooks, Decision, TurnContext, TurnResult
│   ├── event.go                        Event interface, BaseEvent, Emitter, EventType constants
│   └── event_types.go                  All concrete event struct types (22 events; ApprovalRequestedEvent lives in approval.go)
├── adapters/                            Port implementations
│   ├── contextstore/                   ContextPort: in-memory history, pairing, compression
│   └── toolport/                       ToolPort: Composite (native + extension + dynamic mounts, panic recovery, SpecFromDefinition) and Wrap (plain registry port)
├── app/                                 App-level wiring shared by cmd/app
│   ├── logsse.go                        LogBroadcaster — io.Writer teeing slog output to /debug SSE
│   └── nexus/                          Input channel monitoring (see "Nexus" below)
│       └── tools/                      Channel tools: list_channels, read_channel, search_channel
├── extensions/                          core.Extension implementations
│   ├── reminders/                      BeforeIterationHook — injects TODO-plan-state system reminders
│   ├── skillsext/                      ToolProvider (list_skills/load_skill) + PromptContributor (skills index)
│   ├── blackboardext/                  ToolProvider (repl) + SessionEndHook (closes the shared blackboard)
│   ├── permissions/                    ToolCallHook — name/origin policy gating (default-on, runs first)
│   ├── budgets/                        BeforeIterationHook — iteration/wall-clock/token limits → graceful Terminate
│   └── mcpext/                         DynamicToolSource + session hooks — external MCP servers over stdio
├── agent/                              Concrete, stateless Agent implementation
│   └── agent.go                        Agent struct, AgentConfig, DoReasoning — owns no history, no compression, no memory I/O
└── harness/                            Composition root: wiring, config, event bus, memory persistence
    ├── harness.go                      New(), Harness struct, Shutdown, RunTurn — builds ports/adapters and wires core.Loop via runner.AgentRunner
    ├── harness_options.go              HarnessOption functional options (no config struct)
    ├── llm.go                          llmCache, defaultLLMFactory — per-(provider,model,baseURL) client caching
    ├── memory.go                       memoryStore — persists ContextCompressedEvent summaries per conversation to disk; loaded back on WithConversationID resume
    ├── runner/                         AgentRunner facade over core.Loop
    │   ├── agent.go                    Agent interface (extends core.ModelPort with construction-time wiring methods)
    │   ├── agent_runner.go             AgentRunner, AgentRunnerOption, NewAgentRunner — one-time agent wiring, builds core.Loop, RunLoop delegates to Loop.RunTurn
    │   └── loop_fsm.go                 FSM type/const aliases from core
    ├── events/                         Event bus & typed hooks
    │   ├── event.go, event_types.go    Type aliases over the core event types
    │   ├── bus.go                      EventBus — fan-out to buffered subscriber channels
    │   ├── emitter.go                  Emitter interface alias
    │   └── hooks.go                    Hooks struct, StartHooks — typed callback dispatch off the bus
    ├── prompts/                        System prompt construction
    ├── skills/                         Skill discovery & lazy loading
    │   ├── registry.go                 Discover frontmatter at startup, Load on demand
    │   ├── tool_list_skills.go         List available skills (interface: SkillLister)
    │   └── tool_load_skill.go          Load skill content (interface: SkillContentLoader)
    ├── tools/                          Tool dispatch system
    │   └── registry.go                 Name→Definition map, Execute(), GetDefaultToolDefs()
    ├── snapshot/                       Pre-write file snapshots backing the Write/Revert tools
    │   ├── snapshot_store.go           In-memory file snapshot store
    │   ├── tool_snapshot_write.go      File write with pre-write snapshot
    │   └── tool_snapshot_revert.go     Restore file from snapshot
    ├── todo/                           In-memory planning system
    │   ├── todo_file.go                TodoFile — per-instance in-memory store with IDs, deps, priorities, topo sort
    │   ├── tool_todowrite.go           Bulk-write plan with dependency-by-index
    │   ├── tool_todocreate.go          Add single task mid-execution
    │   ├── tool_todoupdate.go          Update task status by ID
    │   ├── tool_todonext.go            Get next unblocked task
    │   └── tool_todoread.go            Read plan in dependency order
    ├── advisor/                        advisor tool — stateless one-shot LLM consult, registered only via WithAdvisorModel
    ├── blackboard/                     Shared Python REPL package (Blackboard, REPL, Querier)
    │   ├── blackboard.go               Blackboard: lazy-start REPL, Execute/Deposit
    │   ├── bootstrap.py                Embedded Python REPL (//go:embed)
    │   ├── preview.go                  Preview (fixed-truncation summaries)
    │   ├── querier.go                  Querier interface + llmQuerier (stateless one-shot LLM calls)
    │   ├── repl.go                     Python subprocess + JSON-line IPC
    │   └── tool_repl.go                repl tool (REPLTool)
    └── subagent/                       Sub-agent delegation
        ├── subagent_factory.go         SubAgentFactory — builds child AgentRunner + Agent + contextstore.Store + child extensions/composite
        ├── spawn_ext.go                SpawnExt — mounts spawn_agent as a core.Extension (in-package to avoid an import cycle)
        └── tool_spawn_agent.go         spawn_agent tool + AgentFactory interface

LLM provider layer: external module github.com/tab58/llm-providers
├── common/                             LLM interface + canonical message types
├── anthropic/, openai/, cerebras/,     One package per provider (constructors,
│   lightning/, openrouter/, ollama/    model definitions)
├── openai_compat/                      Shared OpenAI-compatible base + 429 retry
├── ratelimit/                          TokenBucket, Semaphore, Wrap decorator
└── logger/                             Optional diagnostics logger

internal/harness/tools/tooldef/         Tool implementations (Write/Revert now in harness/snapshot/, skill tools in harness/skills/, todo tools in harness/todo/ — see tree above)
├── definition.go                       Definition interface, Schema, ToolCall, ToolResult
├── tool_bash.go                        Shell command execution (120s timeout)
├── tool_read.go                        File read with line numbers
├── tool_edit.go                        String replacement in file
├── tool_grep.go                        Regex search across files (cap 500)
├── tool_glob.go                        File pattern matching
├── file_tracker.go                     Read-before-edit enforcement (content hashing)
└── fsutil.go                           Atomic file writes, per-path locks
```

## Core Loop (`internal/core/loop.go`)

The agent loop is an FSM-driven perception->action->observation cycle owned by `core.Loop`. It drives three ports (`ModelPort`, `ToolPort`, `ContextPort`), runs extension hooks, and emits events. The loop never branches on model output beyond `stop_reason`. Each `Loop` owns its own FSM instance — subagents and concurrent loops don't share state.

```go
type LoopConfig struct {
    ID           string
    Model        ModelPort       // stateless reasoning (injectable)
    Tools        ToolPort        // tool execution (injectable)
    Context      ContextPort     // conversation history (injectable)
    Emitter      Emitter         // nil-safe event emitter
    Extensions   *Extensions     // nil -> NewExtensions()
    SystemPrompt string          // for logging; ModelPort owns applying it
    FSM          *LoopFSM        // nil -> NewLoopFSM()
}

func NewLoop(cfg LoopConfig) (*Loop, error)
func (l *Loop) RunTurn(ctx context.Context, input string) (TurnResult, error)
func (l *Loop) ID() string
```

`RunTurn` returns `(TurnResult, error)`: `FinalAnswer`/`Iterations`/`Duration` on success; `Terminated` reason from the hook-terminate path (returned as `TurnResult{Terminated: reason}, nil` — not an error); real failures return the error.

## AgentRunner (Facade)

`AgentRunner` (`internal/harness/runner/agent_runner.go`) is a thin facade over `core.Loop`. It performs one-time agent wiring (stream/thinking callbacks) at construction time, builds a `core.Loop` via `core.NewLoop(core.LoopConfig{Model: agent, Tools: tp, Context: o.contextStore, Emitter, Extensions, SystemPrompt})` — where `tp` is the `core.ToolPort` from `WithToolPort` (the harness passes the composite) or a `toolport.Wrap(o.toolRegistry)` fallback — and `RunLoop` is a one-line delegate to `Loop.RunTurn`, translating `TurnResult` into the `(string, error)` contract the harness expects: a non-empty `Terminated` becomes `fmt.Errorf("terminated: %s", reason)` (with `FinalAnswer` still returned alongside), otherwise `FinalAnswer` passes through directly.

The runner has no `Cwd` — working directory is a tool execution concern owned by the `Registry`. The FSM, message assembly, hook dispatch, retry logic, and tool dispatch described below all live in `core.Loop.RunTurn` (`internal/core/loop.go`) — the invariant loop the design goals describe. `AgentRunner` owns none of it; it wires the three ports once at construction and forwards the call.

### State Machine

```
started ──StartReasoning──▶ reasoning_started ──FinishReasoning──▶ reasoning_finished
                                                                       │
                                                          ┌────────────┼────────────┐
                                                          ▼                         ▼
                                                tool_execution_started           stopped
                                                          │
                                              FinishToolExecution
                                                          ▼
                                              tool_execution_finished
                                                          │
                                                   (loops back to
                                                    started via Reset)
```

Six states, six transitions (`internal/core/fsm.go`). `Reset` can fire from any state, including `started` — `RunTurn` itself resets first thing, before the loop's first iteration. FSM is per-`Loop` instance — subagents and concurrent loops don't share state.

### RunTurn Flow

```
Loop.RunTurn(ctx, input string) → (TurnResult, error)

1. context.AppendUser(ctx, input) — once, before the loop
2. Reset FSM
3. tools.BeginTurn(ctx); toolDefs := tools.Definitions() — snapshot the tool
   surface once per turn (composite re-reads dynamic sources here)
4. Loop (iteration = 1, 2, ...):
   a. Check ctx cancellation
   b. StartReasoning
   c. extensions.RunBeforeIteration(ctx, turnCtx) — load-bearing; hooks append
      turnCtx.Reminders and may set turnCtx.Terminate for graceful termination
      (Terminate set → Reset FSM, return TurnResult{Terminated: reason}, nil — not an error)
   d. messages := context.Messages(ctx)
   e. reasoningResult := model.DoReasoning(ctx, messages, turnCtx.Reminders, toolDefs)
   f. FinishReasoning
   g. If ToolCalls is empty (final answer):
      - context.AppendAssistant(ctx, reasoningResult.Meta.AssistantMessage)
      - if the answer is empty, looks like a tool call written as plain text, or
        was truncated at the output token limit, and fewer than 2 retries have
        been used: context.AppendUser(ctx, a retry instruction) → loop to 4a
      - else: Stop, extensions.RunAfterTurn(ctx, &tr) (degrading), return
        TurnResult{FinalAnswer, Iterations, Duration}
   h. Else (tool calls present) — batch semantics:
      - StartToolExecution
      - DECISION PHASE, sequential in issue order: extensions.RunToolCall(ctx, tcc)
        (load-bearing; hooks may escalate Decision to Deny/AskUser, never
        de-escalate; a hook error itself also blocks execution with a synthetic
        result, distinct from an explicit Deny). AskUser calls emit their
        ApprovalRequestedEvent immediately (non-blocking).
      - EXECUTION PHASE, concurrent (errgroup.WithContext): one goroutine per
        call writes results[i] — Allow executes; Deny/hook-error produce
        synthetic error results; AskUser waits out its approval channel in its
        own goroutine (others don't wait on the straggler). Per-call events
        (ToolExecutionStarted/Finished, succeeded/failed) fire inside the
        goroutines — EventBus.Emit is goroutine-safe.
      - after the barrier: extensions.RunToolResult(ctx, trc) sequentially in
        issue order (degrading transform)
      - FinishToolExecution
      - context.AppendAssistant(ctx, reasoningResult.Meta.AssistantMessage), then
        one context.AppendToolResults(ctx, outputs) in issue order — results
        carry ToolUseID so the store pairs them to the assistant's tool_use
        blocks; the context store is only touched after the barrier
      - loop to 4a
5. On any error: Reset FSM, emit ErrorEvent, return the error
```

Exit: model produces `ReasoningResult{ToolCalls: nil}` with a valid final answer, or a `BeforeIteration` hook sets `TurnContext.Terminate` (graceful termination — a `TurnResult`, not an error).
Error: ctx canceled, an FSM transition error, `BeforeIteration` hook error, `DoReasoning` error, or a `ContextPort` append error.

## Agent Interface

```go
type Agent interface {
    GetCurrentModel() string
    UpdateStreamCallback(fn func(text string))
    UpdateThinkingCallback(fn func(text string))
    DoReasoning(ctx context.Context, messages []common.Message, systemReminders []string, tools []common.ToolDefinition) (ReasoningResult, error)
}

type ReasoningResult struct {
    ToolCalls   []core.ToolCall  // empty → final answer ready
    FinalAnswer string           // populated when ToolCalls is empty
    Meta        ResponseMeta
}

type ResponseMeta struct {
    Model, ResponseID, StopReason string
    InputTokens, OutputTokens     int64
    AssistantText                 string
    AssistantMessage               common.Message // full response, for the caller to append
}
```

`ReasoningResult`/`ResponseMeta` live in `internal/core/turn.go`; `runner.ReasoningResult`/`runner.ResponseMeta` are aliases. `core.ModelPort` (`internal/core/ports.go`) mirrors this `DoReasoning` signature — `Agent` is the runner-facing name for the same contract, extended with the `Update*` wiring calls.

The Agent is **stateless**: `messages` is the full conversation history, rebuilt from the runner's `core.ContextPort` on every call, and `tools` is the tool surface for the turn, snapshotted from the loop's `core.ToolPort`. The agent neither stores nor mutates them, and returns the model's full response via `Meta.AssistantMessage` rather than appending it anywhere — the runner does that, against its `ContextPort`. `systemReminders` carries the current todo plan state.

The concrete implementation lives in `internal/agent/`. Tool definitions are applied per-request from the `tools` parameter — the agent holds no tool state.

```go
type AgentConfig struct {
    Model        common.LLM
    SystemPrompt string // fully assembled by the composition root (base + extension fragments)
}
```

Constructor: `New(cfg)`. The agent builds a `CompletionRequest` from the passed-in messages each reasoning cycle and parses the `CompletionResponse` into `ReasoningResult`. It owns no history, no compression, and no memory file I/O — see `## Context Compression` and `ContextPort` below for where those now live.

### ContextPort

```go
type ContextPort interface {
    Messages(ctx context.Context) ([]common.Message, error)
    AppendUser(ctx context.Context, text string) error
    AppendAssistant(ctx context.Context, msg common.Message) error
    AppendToolResults(ctx context.Context, results []ToolResult) error
}
```

Declared in `internal/core/ports.go`; owns the conversation the Agent used to keep itself — user/assistant messages, `tool_use`/`tool_result` pairing (a `tool_use` unanswered in the immediately following message is rejected by the Anthropic API), and compression. The `AgentRunner` holds one `ContextPort` per runner (`runner.WithContextStore`, required) and rebuilds `messages` from it every iteration instead of the Agent tracking history itself.

The in-memory implementation is `internal/adapters/contextstore.Store` (`contextstore.New(contextstore.Config{LLM, InitialMemory, Emitter, RunnerID})`). `harness.New` builds one per main runner (seeded with resumed `InitialMemory`) and `subagent.SubAgentFactory` builds a fresh one per spawned child (no `InitialMemory` — children don't resume). The store emits `ContextCompressedEvent` itself on compaction, stamped with its own `RunnerID`.

## Harness

Deliberately thin orchestrator. Holds the main runner, optionally registers the subagent tool. No loop logic, no tool dispatch, no reminders.

```go
type Harness struct {
    mainAgentRunner *runner.AgentRunner
    toolRegistry    *tools.Registry
    todoFile        *todo.TodoFile
    eventBus        *events.EventBus
    stopHooks       func()          // unsubscribes caller-supplied events.Hooks
    stopMemoryHook  func()          // unsubscribes the ContextCompressedEvent -> memory.go persister
    blackboard      *blackboard.Blackboard
    extensions      *core.Extensions
}
```

Constructor: `New(mainModel common.ModelDefinition, opts ...HarnessOption) (*Harness, error)`. `HarnessConfig` no longer exists; behavior is configured via flat `HarnessOption` functions (`internal/harness/harness_options.go`) applied over `defaultHarnessOptions()`:

- `WithAgentBuilder` — replaces the default agent implementation with a custom `runner.AgentBuilder`; the test seam for stub brains.
- `WithSubagentModel` / `WithBlackboardModel` / `WithAdvisorModel` — per-role `common.ModelDefinition`; an unset role falls back to `mainModel`. The advisor tool is registered only when `WithAdvisorModel` is set (no advisor by default). `WithBlackboardModel` sets the model used for `llm_query`/`llm_batch` sub-LM calls inside the shared blackboard REPL.
- `WithSubagentDepth` (default 1, 0 disables `spawn_agent`) / `WithSubagentMaxIterations` (default 100 per child).
- `WithLLMFactory` — replaces the default env-var-based LLM factory (`provider.LLMFromEnv`) entirely; the test seam for injecting fakes.
- `WithProviderBaseURL(provider, url)` — per-provider base URL override, consumed by the default factory only.
- `WithBlackboardDisabled` — the shared blackboard REPL (`repl` tool, see "Blackboard (Shared REPL)" under Tool System) is on by default; this disables it.
- `WithDisabledTool`, `WithSkillsDir`, `WithTool`, `WithHooks(events.Hooks)`, `WithSystemPrompt`, `WithEventBus`, `WithTextDeltaHandler`, `WithThinkingDeltaHandler` — remaining registry/observability knobs.

LLM clients are cached per (provider, model, base URL) via an internal `llmCache` (`internal/harness/llm.go`), so roles sharing a model definition share one client and its rate limiter.

The brain defaults to the built-in agent implementation: when no `WithAgentBuilder` is set, `harness.New` falls back to an unexported adapter over `agent.New` (`internal/agent`). This is the one place `internal/harness` imports `internal/agent`; all other harness code depends only on the `runner.Agent` interface.

### Turns

`RunTurn(ctx, query)` — runs one agent turn via `RunLoop` and returns the answer. `cmd/app` drives turns over HTTP (`POST /query`) and streams progress via SSE.

## Tool System

### Composite ToolPort (`internal/adapters/toolport/composite.go`)

The loop's `core.ToolPort` in the harness is the `Composite`, which mounts three source kinds in stable definition order: native registry tools (sorted by name, `Origin: "native"`), extension `core.ToolProvider` bundles (registration order, `Origin: "extension:<name>"`), then `core.DynamicToolSource` snapshots (re-read at each turn's `BeginTurn`; e.g. MCP). Extension tools are `core.ToolSpec`s carrying their own `Execute` closure — no registry registration. Static name collisions are a construction error; dynamic collisions are skipped with a warning. Panic recovery lives in `Composite.Execute` — the single choke point for all mounts. `toolport.Wrap(registry)` remains as a plain single-registry port for tests/simple drivers (no-op `BeginTurn`).

### Registry

```go
type Registry struct {
    tools      map[string]tooldef.Definition
    workingDir string
}
```

- `NewRegistry(workingDir, tools...)` — creates registry with working directory for tool execution
- `Register(def)` — adds tool, fails if name exists
- `Execute(ctx, name, input)` — lookup, build `ExecutionContext` with registry's `workingDir`, run tool, return `ToolResult`
- `CopyWithout(names...)` — clone registry excluding named tools (preserves `workingDir`)
- `Definitions()` — return all registered tool definitions
- `ProviderDefinitions()` — convert registered tools to `[]common.ToolDefinition` (name, description, JSON schema) for injection into Agent

### Tool Interface

```go
type Definition interface {
    Name() string
    Description() string         // instruction to model — precise wording matters
    Schema() Schema              // JSON Schema for input
    Execute(ctx, exctx) → (ToolResult, error)
}

type ExecutionContext struct {
    Arguments  []string           // tool input (typically one JSON string)
    WorkingDir string
}

type ToolResult struct {
    ToolUseID string
    Output    string              // returned to model as observation
    IsError   bool                // flags error results (model decides recovery)
}
```

Tools never throw — errors returned as `ToolResult{IsError: true}`. Loop doesn't break on tool errors.

### Tool Inventory (19 tools)

| Tool | Description | Key behavior |
|------|-------------|--------------|
| `bash` | Shell command | 120s timeout, exit code in output |
| `Read` | File contents | Line-numbered output, default 2000 lines |
| `Write` | Write file | Snapshots previous content first |
| `Edit` | String replace | Unique match required unless `replace_all` |
| `Grep` | Regex search | Caps at 500 matches |
| `Glob` | File patterns | Supports `**` wildcard |
| `Revert` | Restore file | Pops from snapshot store (one-shot) |
| `spawn_agent` | Delegate operational task | Spawns child AgentRunner with full tools, blocks until complete, returns final answer |
| `repl` | Shared persistent REPL (blackboard) | Slot `main` for the main agent, `a1`/`a2`/… for subagents; single mutex serializes all access; output truncated at 2000 chars |
| `advisor` | Plan review by stronger model | One-shot call to the advisor model (plan + optional context); returns critique: risks, missing steps, alternatives. Disabled by default — registered only when `WithAdvisorModel` is set; not given to subagents |
| `list_skills` | List skills | Returns name→description map from skill registry |
| `load_skill` | Load skill | Lazy-loads full `SKILL.md` content by name |
| `TodoWrite` | Write plan | Bulk-write tasks with deps-by-index, assigns IDs, replaces the store's plan |
| `TodoCreate` | Add task | Append single task mid-execution with deps-by-ID |
| `TodoUpdate` | Update status | By ID or prefix: `pending`, `in_progress`, `done` |
| `TodoNext` | Next task | Highest-priority pending with all deps done |
| `TodoRead` | Show plan | Topologically sorted task list with statuses and deps |

### Shared Infrastructure

**SnapshotStore** — in-memory `map[string][]byte` behind mutex. `Write` saves before overwriting; `Revert` pops (clears entry). Shared across all tools within a registry.

**FileTracker** — SHA-256 content stamps per file path. Enforces read-before-edit (not yet wired into Edit/Write tools, but available). Returns `ErrNotRead` or `ErrChangedSinceRead`.

**fsutil** — per-path mutex locks (`sync.Map`) for concurrent file access. `writeFileAtomic` uses temp-file + rename for crash safety.

**Todo reminders** — `TodoFile.FormatReminder()` formats the runner's own plan as a `<system-reminder>` block; returns `""` when the plan is empty. Wired per runner via `runner.WithTodoFile` and injected in `buildSystemReminders()`.

### Blackboard (Shared REPL)

Package `internal/harness/blackboard/`. A `Blackboard` is one persistent Python REPL per harness, shared by the main agent and every subagent. It wraps `NewREPL` (`repl.go`, `Querier`, and `bootstrap.py` in the same package host the sandboxed Python REPL subprocess machinery — there is no separate model-facing `rlm` tool). `bootstrap.py` carries no blackboard-specific logic — the shared namespace (`bb`, a guard dict that rejects writes to top-level keys other than the executing agent's slot) and helpers (`peek`, `bb_grep`) are injected via a one-time setup `Execute` call when the process lazily starts on first use. (`bootstrap.py`'s only blackboard-relevant feature is a transport-level stdout cap of 100k chars.) A single `sync.Mutex` serializes all access — the entire concurrency contract. If the REPL transport fails or a call is cancelled mid-execution, `Blackboard` closes and discards it so the next call restarts fresh (blackboard contents are lost); agents must tolerate a slot they expect being missing.

- **`repl` tool** (`blackboard.NewREPLTool`) — one instance per agent, bound to a slot ID: `main` for the main agent, `a1`, `a2`, … for subagents (assigned from a package-level `atomic.Int64` counter in `subagent.nextAgentID`). Convention (not enforced in code): an agent writes only inside `bb['<its slot>']`, reads anything, never busy-waits on another slot. REPL stdout returned to the calling agent is truncated to `DefaultHeadChars`+`DefaultTailChars` (1500+500 = 2000) chars.
- **Deposit/preview flow** (`SubAgentFactory.SpawnAgent`, `internal/harness/subagent/subagent_factory.go`) — a subagent's final answer up to `inlineResultMax` (2000) chars is returned inline, prefixed with `"Sub-agent <id> completed (blackboard slot bb['<id>'])."` so the orchestrator knows the slot even when nothing was deposited (the sub-agent may have written to its slot itself). Longer results are deposited via `Blackboard.Deposit` to `bb['<agent_id>']['result']` (agent ID validated against `[A-Za-z0-9_]+`, value passed through `SetVar`, never spliced into generated code) and the tool returns `"Sub-agent <id> completed."` plus a `Preview`: a 1500-char head / 500-char tail summary with a hint to inspect the full value via `peek()`/`bb_grep()`. If the deposit itself fails, `SpawnAgent` falls back to returning the full result inline.
- **Logging** — every `exec`/`deposit` op is logged via `slog` at info level (so it lands in `.tenzing-agent.log`): agent, code (capped 500 chars), `stdout_len`, stdout head (capped 200 chars); transport failures log at error level. This is the only visibility into blackboard state since it never appears in transcripts.
- **Options** — `WithBlackboardDisabled()` skips registering the blackboard and the `repl` tool entirely (subagent results are then always inline). On by default and re-exported from `pkg/tenzing`.
- **Shutdown** — `Harness.Shutdown()` runs the extensions' session-end hooks (5s timeout) alongside stopping hook dispatch; the blackboard extension's `SessionEnd` closes the Python subprocess (`Blackboard.Close`). The `repl` tool mounts via `internal/extensions/blackboardext` (`core.ToolProvider`), not the tool registry.

Known limit: `llm_query`/`llm_batch` inside the blackboard hold the REPL mutex for all agents while they run — keep individual calls small and prefer `llm_batch` for fan-out work.

## Skill System

### Registry

```go
type Definition struct {
    Name        string
    Description string
    path        string   // unexported — callers use Load()
}

type Registry struct {
    skills map[string]Definition
}
```

- `NewRegistry()` — creates an empty registry (no args)
- `RegisterSkillDir(dir)` — tilde-expands `dir` (`~/...` resolves against the home directory) and scans it at registration time, discovering skills via frontmatter parsing; nonexistent or unreadable dirs are skipped silently
- `Discover()` — returns a copy of the skills map (metadata only, no file content)
- `Load(name)` — lazy-loads full `SKILL.md` content from disk on demand

Skills are subdirectories of a registered skills dir, each containing a `SKILL.md` with YAML frontmatter (`name`, `description`) between `---` fences. Discovery reads only frontmatter — zero full-body reads at startup.

Skill metadata is passed as data into `AgentConfig` for system prompt injection. The `SkillRegistry` itself is passed to `list_skills`/`load_skill` tool constructors for runtime access.

## Unified Todo System

In-memory planning system in `internal/harness/todo/`. Each `TodoFile` instance (constructed with `todo.NewTodoStore()`) holds its own plan — one per harness or subagent, never shared — so concurrent runners in one process cannot clobber or observe each other's plans. State survives context compression (it lives in-process, outside the message history) but not process restarts.

```go
type Task struct {
    ID, Description, Status, Result string
    Priority    TaskPriority       // high, medium, low
    DependsOn   []string           // task IDs
}

type TodoFile struct { mu sync.Mutex; tasks []Task; emitter events.Emitter }
```

- `WriteTasks(tasks)` — bulk write, replaces existing plan
- `CreateTask(desc, dependsOn, priority)` — appends one task, validates dependencies, assigns random hex ID
- `UpdateTask(taskID, status, result)` — by ID or prefix match
- `NextTask()` — highest-priority pending task with all deps done
- `FormatReminder()` — topologically sorted `<system-reminder>` block injected per turn
- `SetEmitter(events.Emitter)` — emits `TaskCreatedEvent`/`TaskCompletedEvent`

Five tools (`TodoWrite`, `TodoCreate`, `TodoUpdate`, `TodoNext`, `TodoRead`). `TodoWrite` accepts dependency-by-index for bulk planning; `TodoCreate` uses dependency-by-ID for mid-execution additions. Display always topologically sorted.

Plan state is re-injected from the in-memory store after context compression via `TodoProvider func() string` wired through the compressor. The agent cannot lose its plan regardless of summary quality.

## Context Compression

Three-layer compression in `internal/adapters/contextstore/compressor/compressor.go`. Prevents unbounded history growth during long sessions.

```go
type Compressor struct { llm common.LLM; threshold, summarizeBudget int }
```

- `EstimateSize(messages)` — sums char lengths across all content blocks
- `MaybeCompress(ctx, messages)` — triggers when history exceeds 75% of context window AND more than 6 messages. Splits at `len-6`, summarizes the older portion via LLM (sectioned third-person digest: Decisions / Files touched / Current state / Open work / Last position; input budget = half the context window with a head+tail omission marker), injects current todo state from disk (via `TodoProvider`), returns `[summary, todo_state, ack, ...recent_6]`
- No file I/O. The summary surfaces as `ContextCompressedEvent` on the event bus; the harness persists it per conversation — main agent → `<UserConfigDir>/tenzing/.agent_memory-<YYYYMMDD-HHMM>-<AGENT_ID>.md`, sub-agents → `<UserCacheDir>/tenzing/` (write-only) — with a 7-day TTL sweep at startup. Resume via `WithConversationID(id)`. See `docs/superpowers/specs/2026-07-11-agent-memory-design.md`.

Integrated in `contextstore.Store` — runs after every `AppendUser`/`AppendAssistant`/`AppendToolResults` call that leaves the most recent message as an assistant turn (see "### ContextPort" under Agent Interface above). `contextstore.Config.InitialMemory` seeds history when the harness resumes a conversation. The Agent itself is uninvolved — it never sees compression happen.

Compression is non-fatal: LLM errors are logged, original history preserved.

## Sandboxed Python REPL

`internal/harness/blackboard/` hosts the sandboxed Python REPL subprocess machinery alongside the blackboard that builds on it (see "Blackboard (Shared REPL)" under Tool System): `repl.go` (subprocess + JSON-line IPC over stdin/stdout, callbacks for `llm_query`/`llm_batch`, `read_file`, `grep_file`, `list_files`), `bootstrap.py` (the embedded Python side of the protocol), and `querier.go` (the `Querier` interface + `llmQuerier`, a stateless one-shot LLM caller used for `llm_query`/`llm_batch`). There is no standalone model-facing tool here — REPL access reaches the model only through the blackboard's `repl` tool.

## Sub-Agent Architecture

Recursive delegation via full autonomous sub-agents. The main agent delegates operational tasks (file editing, commands, investigations) to child agents that run their own AgentRunner loop. Children can themselves spawn sub-agents up to a configurable max depth.

Architecture: `spawn_agent` tool, mounted via `subagent.SpawnExt` (`core.ToolProvider`; it lives in the subagent package rather than `internal/extensions/` because the factory's own child wiring builds it for grandchildren — a separate package would be an import cycle) → `AgentFactory` interface → `SubAgentFactory` builds child AgentRunner + Agent per spawn: fresh native registry, child extension set (`childExtensions`: own todo reminders, shared skills ext, shared blackboard ext under the child's slot ID, and — below max depth — a `SpawnExt` wrapping a nested factory), a child composite ToolPort over both, and the child system prompt assembled with the extension `PromptFragments`. Children share the blackboard REPL with their parent when one is configured, so analytical work over large inputs can run there via `llm_query`/`llm_batch`.

Depth control: factory tracks `currentDepth`. Depth exclusion is a wiring decision — `childExtensions` omits `SpawnExt` at `maxDepth`, so that child's tool surface has no `spawn_agent`. Default max depth is 1 (main → child; no grandchildren) using the main model.

Tool isolation: each child gets fresh native tool instances via `tools.NewRegistry(cwd)`. No tool instance is shared between parent and child (the shared blackboard/skills extensions are deliberate wiring decisions). `pathLocks` (package-level `sync.Map`) is the one intentional exception — serializes file writes across all agents in-process.

Children get their own empty `TodoFile` (`todo.NewTodoStore()`) and the parent's shared skills extension (read-only registry). No event hooks.

Wired via `WithSubagentDepth` (default 1, 0 = disabled) and `WithSubagentMaxIterations` (default 100 per child). Children are built with the same `runner.AgentBuilder` as the main agent — the default built-in agent, or whatever `WithAgentBuilder` set.

## Event System

Typed event bus providing full observability of the agent loop. Events fire at FSM state transitions and business-level boundaries. Package: `internal/harness/events/`.

### Architecture

`EventBus` fans out events to buffered subscriber channels. Async dispatch — if a subscriber's buffer is full, the event is dropped (logged via `slog.Warn`). Thread-safe via `sync.RWMutex`.

Layers emit via the narrow `Emitter` interface (`Emit(Event)`), never importing `EventBus` directly. The Harness creates the bus and passes it down.

### Event Types (22)

Session: `session.started`, `session.ended`. Turn: `turn.started`, `turn.completed`. FSM: `loop.started`, `loop.stopped`, `reasoning.started`, `reasoning.finished`, `tool_execution.started`, `tool_execution.finished`. Business: `llm.response`, `tool.succeeded`, `tool.failed`, `tool.progress`, `context.compressing` (reserved), `context.compressed`, `error`. Subagent: `subagent.started`, `subagent.stopped`. Task: `task.created`, `task.completed`. Approval: `approval.requested` (`ApprovalRequestedEvent` — carries `CallID`/`ToolName`/`Input`/`Reason` plus a non-serialized `Respond(approved bool)` callback; idempotent, safe from any goroutine; the loop blocks on it until response, context cancel, or `LoopConfig.ApprovalTimeout`, where timeout/cancel = deny and 0 denies immediately without emitting).

All events embed `BaseEvent` (type, timestamp, runner ID) and are JSON-serializable.

### Subscribing

Programmatic: `bus.Subscribe(bufSize)` returns `<-chan Event`. Type-switch on concrete event structs.

Hooks: `events.Hooks` struct has one typed `func(XxxEvent)` field per event type (all optional). `events.StartHooks(bus, hooks)` subscribes with a buffer of 64, dispatches in a goroutine, and returns a `stop func()` that unsubscribes to end dispatch; `Harness.Shutdown` calls it to stop hook dispatch.

### Emit Sites

Core loop (`core/loop.go`): emits turn, loop, reasoning, tool execution, LLM response, and error events. Harness (`harness.go`): emits session events. Subagent (`subagent_factory.go`): emits subagent lifecycle events. Todo (`todo_file.go`): emits task lifecycle events. Context store (`contextstore/store.go`): emits compression events.

### Streaming

`OnTextDelta` and `OnThinkingDelta` remain direct Agent callbacks, not events. Token-level streaming is out of scope for the event system.

### Wiring

`WithEventBus` (optional — `defaultHarnessOptions` creates one if not overridden), `WithHooks` (optional typed `events.Hooks`). `Harness.EventBus()` accessor for programmatic subscription.

## Nexus

Input channel monitoring that turns external log/event streams into agent wake-ups. Package: `internal/app/nexus/` (channel runtime), `internal/app/nexus/tools/` (agent-facing tools). Wired into `cmd/app` alongside the harness.

### Config

`nexus.yaml` (path from `NEXUS_CONFIG`, default `nexus.yaml`; a missing file means zero channels — nexus is entirely optional). Each entry under `channels:` configures one channel:

- `type` — `file-tail` (polls a file for new lines, requires `path`), `command` (runs a long-lived subprocess and restarts it with backoff on exit, requires `cmd`), or `webhook` (ingested via HTTP POST, no source goroutine).
- `error_pattern` — regex tested against each line to classify it as an error; defaults to `(?i)error|panic|fatal`.
- `buffer_size` — ring buffer capacity per channel; defaults to 1000.
- `trigger` — whether error lines on this channel wake the agent; defaults to true.

### Ring Buffers

Each channel owns a fixed-size ring buffer (`Ring`) of `Entry{Seq, Text, IsError}`, holding the most recent `buffer_size` lines. Reads (`Nexus.Read`, `Nexus.Search`) and the channel tools operate on this buffer only — nexus does not persist history to disk.

### Error → Debounced Agent Wake

A line matching `error_pattern` emits a `ChannelErrorEvent` and, if the channel has `trigger` enabled, notifies a `Trigger`. `Trigger` (`internal/app/nexus/trigger.go`) debounces per channel (30s cooldown, wired in `cmd/app/container.go`) and holds a global queue-of-one: while a turn is running, newly erroring channels accumulate in a pending set instead of firing again; `TurnEnded()` (called after every turn via `agentServer.onTurnEnd`) flushes the pending set once the agent is free. A successful wake starts a turn with a synthesized investigation prompt (`agentServer.nexusPrompt`) built from each pending channel's recent errors.

### Routes

- `POST /ingest/{name}` — webhook ingest; only mounted when at least one channel is configured. 202 on success, 404 for an unknown or non-webhook channel name.
- `GET /debug` — SSE stream of `slog` output, fed by `app.LogBroadcaster` (an `io.Writer` teed into the slog handler alongside the log file). Independent of the nexus channels — available whenever the app runs.

### Events

Three SSE event types are forwarded from the nexus event bus by `agentServer.forwardEvents`: `channel_error` (channel, text, seq), `channel_status` (channel, state — channel source goroutine lifecycle), and `nexus_trigger` (channels — emitted when a wake actually starts a turn).

### Channel Tools

`nexustools.NewListChannelsTool`, `NewReadChannelTool`, `NewSearchChannelTool` (package `internal/app/nexus/tools`) wrap `Nexus.ChannelInfos`/`Read`/`Search` as `tooldef.Definition`s. Registered via `harness.WithTool` only when channels are configured — the agent gets no nexus tools when nexus is disabled.

## Provider Layer

Lives in the external module `github.com/tab58/llm-providers`. The harness imports canonical types from its `common` package and, from the module's root package (aliased `provider`), `provider.LLMFromEnv(model, opts...)` — the harness's default LLM factory (`internal/harness/llm.go`). It resolves the API key from the provider's conventional env var (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `CEREBRAS_API_KEY`, `LIGHTNING_API_KEY`, `OPENROUTER_API_KEY`; Ollama is keyless, `OLLAMA_API_KEY` optional) and dispatches to the matching provider's `NewClient` (`anthropic`, `openai`, `cerebras`, `lightning`, `openrouter`, `ollama`). Constructors take a `common.Model` (a `common.ModelDefinition` value, not a string) and return a `common.LLM` wrapped with default client-side rate limiting; `WithNoRateLimit` options opt out. Callers needing custom key sourcing override the factory with `harness.WithLLMFactory`.

### LLM Interface

```go
type LLM interface {
    SendSyncMessage(ctx, req) → (CompletionResponse, error)
    SendStreamingMessage(ctx, req, events chan<- StreamEvent) → error
    SendMessageWithTools(ctx, req, tools []ToolDefinition) → (CompletionResponse, error)
    CountTokens(ctx, req) → (TokenCount, error)
    ListModels(ctx) → ([]ModelInfo, error)
    GetCurrentModel() → string
}
```

Six implementations, compile-time checked:

| Provider | Type | Notes |
|----------|------|-------|
| `Anthropic` | Direct SDK | Native tool use, token counting, rate limiting |
| `OpenAI` | OpenAI-compat | `useMaxCompletionTokens: true` |
| `Cerebras` | OpenAI-compat | Fast inference |
| `Lightning` | OpenAI-compat | Local/edge |
| `OpenRouter` | OpenAI-compat | Multi-backend routing |
| `Ollama` | Direct HTTP | Local LLM |

### Message Types (provider-agnostic)

```go
type Message struct {
    Role    Role              // user, assistant, system, tool
    Content []ContentBlock
}

type ContentBlock struct {
    Type         ContentType   // text, tool_use, tool_result
    Text         string
    ToolUseID    string        // ties tool_use to tool_result
    ToolName     string
    ToolInput    json.RawMessage
    ToolResultID string
    ToolOutput   string
}

type CompletionRequest struct {
    Model, System string
    Messages      []Message
    MaxTokens     int64
    Temperature   *float64
    Tools         []ToolDefinition
}

type CompletionResponse struct {
    ID, Model  string
    Content    []ContentBlock
    StopReason StopReason      // end_turn, tool_use, max_tokens, stop
    Usage      Usage           // InputTokens, OutputTokens
}
```

Helper methods: `CompletionResponse.Text()` returns first text block; `CompletionResponse.ToolCalls()` returns all tool_use blocks.

### Streaming

```go
type StreamEvent struct {
    Type     StreamEventType  // start, delta, stop, error
    Text     string           // delta text
    Response *CompletionResponse  // final accumulated (on stop)
    Err      error            // on error
}
```

Flow: `start` → `delta`* → `stop`. `error` possible at any point.

Anthropic streaming reconstructs tool call JSON from partial `input_json_delta` fragments, joining them on `content_block_stop`.

OpenAI-compat streaming tracks `pendingToolCall` structs keyed by stream index, accumulating function argument fragments.

### Rate Limiting

**TokenBucket** (`llm-providers/ratelimit`) — token-bucket algorithm with configurable:
- `Rate` (tokens/second refill)
- `BurstSize` (max bucket capacity)
- `MaxConcurrency` (semaphore slots)

Anthropic default: 10K input tokens/min, 10 concurrent requests.

`Acquire(ctx, cost)` blocks until tokens available or ctx canceled. `Release()` frees concurrency slot.

**Retry** (OpenAI-compat only) — exponential backoff on HTTP 429. 2s base, 60s max, 50% jitter, 5 attempts. Streaming retries only if no events emitted yet.

### Provider Conversion

Each provider converts between canonical types and SDK-specific types:

- `toAnthropicMessages` / `fromAnthropicResponse` — handles system prompt as `TextBlockParam` (not a message), tool input schema split into `properties`/`required`
- `toOpenAIMessages` / `fromOpenAIResponse` — system prompt injected as first message, tool definitions as `FunctionDefinitionParam`

## Dependencies

| Package | Purpose |
|---------|---------|
| `tab58/llm-providers` | LLM provider layer (canonical types, provider clients, rate limiting) |
| `anthropics/anthropic-sdk-go` | Anthropic API client (via llm-providers) |
| `openai/openai-go/v3` | OpenAI API client (via llm-providers) |
| `looplab/fsm` | Finite state machine for loop transitions |
| `golang.org/x/sync` | Weighted semaphore for concurrency limiting |

## Known Design Issues

- **`FileTracker`** — exists but isn't wired into Edit/Write tools.

## What's Not Built Yet

- No async execution, multi-agent teams (Phase 3)
- No permission governance or session persistence (Phase 4); permission gates design: `docs/superpowers/specs/2025-06-25-permission-gates-design.md`
- No parallel tool execution, prompt caching, MCP integration (Phase 5)
