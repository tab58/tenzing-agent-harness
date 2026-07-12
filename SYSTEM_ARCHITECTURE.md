# System Architecture

## Design Goals

**Three-layer separation: Harness → AgentRunner → Agent.**

- **Harness** — the outermost shell. CLI/TUI, process lifecycle, session management, user I/O. Knows how to talk to humans. Does not know how to run an agent loop.
- **AgentRunner** — the reusable loop primitive. Owns the FSM, drives perception→action→observation, dispatches tools via the registry. Knows how to run *any* agent through the cycle. Does not know about CLI, sessions, or users.
- **Agent** — the reasoning engine. Decides what to do, which tools to call, how to interpret results. Talks to the LLM. Does not touch the filesystem or manage processes directly.

The Harness creates an AgentRunner for the main session. A subagent spawns a fresh AgentRunner with its own FSM and message history. This separation means you can swap the agent (different LLM, different strategy) without touching execution infrastructure, swap the harness (CLI → server, local → remote) without touching decision logic, and reuse the loop for subagents without coupling them to the CLI layer.

**Everything that isn't invariant is configurable.** The loop (perception → action → observation), the FSM, and the dispatch pattern (`name → handler(input)`) are structural invariants — they never change. Everything else is injectable via `AgentRunnerConfig`: which agent, which tools, which system prompt, which reminders get injected, how subagents are constructed. This allows piece-by-piece optimization of the harness without touching the loop. Swap the model, swap the reminder strategy, give subagents different tools or a different provider — all through configuration, not code changes.

**`AgentRunnerConfig` is the single DI surface.** All non-invariant runner behavior flows through this struct. The Harness is deliberately thin — it wires a main runner and optionally registers the subagent tool. Nothing else.

**The loop never changes.** Perception → action → observation is the single primitive. New capabilities are added by registering tools or wrapping the loop with new mechanisms (planning, subagents, context compression) — never by modifying the loop itself.

**Tools are the only extension point.** Adding a capability = one registry entry, zero loop changes. Tool descriptions are instructions, not documentation — precise wording controls model tool selection. Tools never throw; errors are strings the agent interprets.

**Provider agnosticism.** All LLM interaction flows through canonical types (`Message`, `ContentBlock`, `CompletionRequest/Response`). Provider implementations convert to/from SDK-specific types. Swapping providers requires zero changes above the provider layer.

**Risk changes the process.** Tools carry a risk classification (read_only, draft, external_write). Read-only tools execute autonomously. Draft tools simulate without side effects. External writes require human approval before finalization. This is the draft-commit pattern: dangerous actions are first drafted, then explicitly committed. The permission check happens in the Runner before tool execution — not in the tool itself, not in the Agent. Injectable via `AgentRunnerConfig` so different runners can enforce different policies. *(Not yet implemented — Phase 4.)*

**Long tasks have budgets.** Every agent loop enforces hard limits: step budget (max iterations), time budget (wall-clock), token budget (per turn and cumulative), and cost budget (USD). When a budget is exhausted, the harness terminates gracefully and returns a structured result — not a crash. Budget checks sit alongside `ctx.Err()` at the top of the loop, injectable via config. Without budgets, a runaway loop burns money silently. *(Not yet implemented — Phase 4.)*

**Context is assembled, not dumped.** The system prompt is ordered by stability for cache efficiency: Layer 0 (system policies, stable prefix, cached) → Layer 1 (skill definitions, rarely change, cached) → Layer 2 (session instructions, per conversation, not cached) → Layer 3 (JIT-retrieved tool outputs, fresh, not cached). Untrusted data (user input, tool output from external sources) is marked with trust labels so the harness can treat it differently. *(Partially implemented — skills use progressive disclosure, but no cache-aware ordering or trust labels yet.)*

**Registries own implementations, Agent gets metadata.** Tools and skills follow the same wiring pattern: registries load from disk at startup, the Agent receives metadata (tool definitions, skill names/descriptions) as data via `AgentConfig`, and the AgentRunner dispatches execution at runtime. The Agent never touches a registry directly — it tells the LLM what capabilities exist, the Runner actually runs them.

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
├── app/                                 App-level wiring shared by cmd/app
│   ├── logsse.go                        LogBroadcaster — io.Writer teeing slog output to /debug SSE
│   └── nexus/                          Input channel monitoring (see "Nexus" below)
│       └── tools/                      Channel tools: list_channels, read_channel, search_channel
├── agent/                              Concrete Agent implementation
│   ├── agent.go                        Agent struct, AgentConfig, DoReasoning, NewWithCompressor
│   └── context/                        Knowledge & context management
│       ├── compression.go              Three-layer context compression + memory persistence
│       ├── context.go                  Context struct (placeholder)
│       └── compressor/                 Context compression + memory persistence
└── harness/                            Core loop & orchestration
    ├── agent.go                        Agent interface + ReasoningResult
    ├── agent_runner.go                 AgentRunner: FSM-driven loop, DI config
    ├── loop_fsm.go                     Per-runner FSM (6 states, 6 transitions)
    ├── harness.go                      Thin orchestrator, config types, RunTurn
    ├── subagent_factory.go             SubAgentFactory — builds child AgentRunner+Agent
    ├── defaults.go                     DefaultReminderBuilder, DefaultMainConfig
    ├── prompts/                        System prompt construction
    ├── skills/                         Skill discovery & lazy loading
    │   └── registry.go                 Discover frontmatter at startup, Load on demand
    ├── tools/                          Tool dispatch system
    │   └── registry.go                 Name→Definition map, Execute(), GetDefaultToolDefs()
    ├── blackboard/                     Shared Python REPL package (Blackboard, REPL, Querier)
    │   ├── blackboard.go               Blackboard: lazy-start REPL, Execute/Deposit
    │   ├── bootstrap.py                Embedded Python REPL (//go:embed)
    │   ├── preview.go                  Preview (fixed-truncation summaries)
    │   ├── querier.go                  Querier interface + llmQuerier (stateless one-shot LLM calls)
    │   ├── repl.go                     Python subprocess + JSON-line IPC
    │   └── tool_repl.go                repl tool (REPLTool)
    └── subagent/                       Sub-agent delegation
        └── tool_spawn_agent.go         spawn_agent tool + AgentFactory interface

LLM provider layer: external module github.com/tab58/llm-providers
├── common/                             LLM interface + canonical message types
├── anthropic/, openai/, cerebras/,     One package per provider (constructors,
│   lightning/, openrouter/, ollama/    model definitions)
├── openai_compat/                      Shared OpenAI-compatible base + 429 retry
├── ratelimit/                          TokenBucket, Semaphore, Wrap decorator
└── logger/                             Optional diagnostics logger

internal/harness/tools/tooldef/         Tool implementations
├── definition.go                       Definition interface, Schema, ToolCall, ToolResult
├── tool_bash.go                        Shell command execution (120s timeout)
├── tool_read.go                        File read with line numbers
├── tool_write.go                       File write with pre-write snapshot
├── tool_edit.go                        String replacement in file
├── tool_grep.go                        Regex search across files (cap 500)
├── tool_glob.go                        File pattern matching
├── tool_revert.go                      Restore file from snapshot
├── tool_list_skills.go                 List available skills (interface: SkillLister)
├── tool_load_skill.go                  Load skill content (interface: SkillContentLoader)
├── todo/                               In-memory planning system
│   ├── todo_file.go                    TodoFile — per-instance in-memory store with IDs, deps, priorities, topo sort
│   ├── tool_todowrite.go              Bulk-write plan with dependency-by-index
│   ├── tool_todocreate.go             Add single task mid-execution
│   ├── tool_todoupdate.go             Update task status by ID
│   ├── tool_todonext.go               Get next unblocked task
│   └── tool_todoread.go               Read plan in dependency order
├── snapshot_store.go                   In-memory file snapshot store
├── file_tracker.go                     Read-before-edit enforcement (content hashing)
└── fsutil.go                           Atomic file writes, per-path locks
```

## AgentRunner (Core Loop)

The agent loop is an FSM-driven perception→action→observation cycle. The runner never branches on model output beyond `stop_reason`. Each runner owns its own FSM instance — subagents and concurrent loops don't share state.

### Configuration (DI Surface)

```go
type AgentRunnerConfig struct {
    Agent          Agent              // reasoning engine (injectable)
    ToolRegistry   *tools.Registry    // which tools this runner has (injectable)
    Hooks          Hooks              // lifecycle observation (injectable)
    SystemPrompt   string             // instructions for this runner (injectable)
    BuildReminders ReminderBuilder    // system reminders per turn (injectable, nil = none)
}

type ReminderBuilder func() []string
```

Every field is caller-controlled. Main runner and subagent runners can have completely different configurations — different model, different tools, different system prompt, different reminder strategy. The loop code is identical for both; only the config differs.

The runner has no `Cwd` — working directory is a tool execution concern owned by the `Registry`. `ReminderBuilder` closes over any state it needs (e.g. cwd) rather than receiving it from the runner.

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

Six states, six transitions. `Reset` can fire from any state except `started`. FSM is per-runner instance.

### RunLoop Flow

```
RunLoop(ctx, input string) → (string, error)

1. Reset FSM
2. Loop:
   a. Check ctx cancellation
   b. Build system reminders via BuildReminders (injectable)
   c. StartReasoning → agent.DoReasoning(inputs, reminders) → FinishReasoning
   d. If ToolCalls is empty → Stop → return FinalAnswer
   e. StartToolExecution → registry.Execute for EVERY tool call, in order → FinishToolExecution
   f. Fire hooks.OnToolCall(name, input, output) per call
   g. Set inputs to the new tool results only, in call order (agent history holds earlier turns)
   h. Loop to 2a
3. On error: Reset FSM, return error
```

Exit: model produces `ReasoningResult{ToolCalls: nil}` (final answer).
Error: ctx canceled, DoReasoning error, or tool execution error.

## Agent Interface

```go
type Agent interface {
    DoReasoning(ctx context.Context, inputs []string, systemReminders []string) (ReasoningResult, error)
}

type ReasoningResult struct {
    ToolCalls   []tooldef.ToolCall  // empty → final answer ready
    FinalAnswer string              // populated when ToolCalls is empty
}
```

The AgentRunner owns the loop; `Agent` owns the LLM interaction. `inputs` carries the user message on the first iteration and only the newest tool results afterwards — the Agent keeps the conversation history itself, and pairs tool outputs (by position) with the pending `tool_use` ids from the previous response as `tool_result` blocks (required by the Anthropic API). `systemReminders` carries the current todo plan state.

The concrete implementation lives in `internal/agent/`. Tool definitions are injected at construction via `AgentConfig` — the tool registry converts its definitions to `[]common.ToolDefinition` via `Registry.ProviderDefinitions()`.

```go
type AgentConfig struct {
    Model           common.LLM
    ToolDefinitions []common.ToolDefinition
    SystemPrompt    string
    Skills          map[string]string // name → description, injected into system prompt
}
```

`Agent` manages conversation history as `[]common.Message`, builds `CompletionRequest` each reasoning cycle, and parses `CompletionResponse` into `ReasoningResult`.

Constructor: `New(cfg)` — includes context compression. `cfg.InitialMemory` (a string, loaded by the harness for resumed conversations) seeds history with the prior session's summary. After each `DoReasoning` call, `MaybeCompress` keeps history within bounds. The agent performs no memory file I/O — persistence is the harness's job.

## Harness

Deliberately thin orchestrator. Holds the main runner, optionally registers the subagent tool. No loop logic, no tool dispatch, no reminders.

```go
type Harness struct {
    mainAgentRunner *runner.AgentRunner
    toolRegistry    *tools.Registry
    skillRegistry   *skills.Registry
    todoFile        *todo.TodoFile
    eventBus        *events.EventBus
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
- **Shutdown** — `Harness.Shutdown()` closes the blackboard's Python subprocess (`Blackboard.Close`) alongside stopping hook dispatch.

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

Three-layer compression in `internal/agent/context/compressor/compressor.go`. Prevents unbounded history growth during long sessions.

```go
type Compressor struct { llm common.LLM; threshold, summarizeBudget int }
```

- `EstimateSize(messages)` — sums char lengths across all content blocks
- `MaybeCompress(ctx, messages)` — triggers when history exceeds 75% of context window AND more than 6 messages. Splits at `len-6`, summarizes the older portion via LLM (sectioned third-person digest: Decisions / Files touched / Current state / Open work / Last position; input budget = half the context window with a head+tail omission marker), injects current todo state from disk (via `TodoProvider`), returns `[summary, todo_state, ack, ...recent_6]`
- No file I/O. The summary surfaces as `ContextCompressedEvent` on the event bus; the harness persists it per conversation — main agent → `<UserConfigDir>/tenzing/.agent_memory-<YYYYMMDD-HHMM>-<AGENT_ID>.md`, sub-agents → `<UserCacheDir>/tenzing/` (write-only) — with a 7-day TTL sweep at startup. Resume via `WithConversationID(id)`. See `docs/superpowers/specs/2026-07-11-agent-memory-design.md`.

Integrated in `Agent.DoReasoning` — runs after each assistant response. `AgentConfig.InitialMemory` seeds history when the harness resumes a conversation.

Compression is non-fatal: LLM errors are logged, original history preserved.

## Sandboxed Python REPL

`internal/harness/blackboard/` hosts the sandboxed Python REPL subprocess machinery alongside the blackboard that builds on it (see "Blackboard (Shared REPL)" under Tool System): `repl.go` (subprocess + JSON-line IPC over stdin/stdout, callbacks for `llm_query`/`llm_batch`, `read_file`, `grep_file`, `list_files`), `bootstrap.py` (the embedded Python side of the protocol), and `querier.go` (the `Querier` interface + `llmQuerier`, a stateless one-shot LLM caller used for `llm_query`/`llm_batch`). There is no standalone model-facing tool here — REPL access reaches the model only through the blackboard's `repl` tool.

## Sub-Agent Architecture

Recursive delegation via full autonomous sub-agents. The main agent delegates operational tasks (file editing, commands, investigations) to child agents that run their own AgentRunner loop. Children can themselves spawn sub-agents up to a configurable max depth.

Architecture: `spawn_agent` tool → `AgentFactory` interface (in `subagent/`) → `SubAgentFactory` (in `harness/`) builds child AgentRunner + Agent per spawn. Children share the blackboard REPL with their parent when one is configured, so analytical work over large inputs can run there via `llm_query`/`llm_batch`.

Depth control: factory tracks `currentDepth`. At `maxDepth`, child gets all tools except `spawn_agent`. Default max depth is 1 (main → child; no grandchildren) using the main model.

Tool isolation: each child gets fresh tool instances via `tools.NewRegistry(cwd)`. No tool instance is shared between parent and child. `pathLocks` (package-level `sync.Map`) is the one intentional exception — serializes file writes across all agents in-process.

Children get their own empty `TodoFile` (`todo.NewTodoStore()`) and an empty `SkillsRegistry` — nothing is shared with the parent. No event hooks.

Wired via `WithSubagentDepth` (default 1, 0 = disabled) and `WithSubagentMaxIterations` (default 100 per child). Children are built with the same `runner.AgentBuilder` as the main agent — the default built-in agent, or whatever `WithAgentBuilder` set.

## Event System

Typed event bus providing full observability of the agent loop. Events fire at FSM state transitions and business-level boundaries. Package: `internal/harness/events/`.

### Architecture

`EventBus` fans out events to buffered subscriber channels. Async dispatch — if a subscriber's buffer is full, the event is dropped (logged via `slog.Warn`). Thread-safe via `sync.RWMutex`.

Layers emit via the narrow `Emitter` interface (`Emit(Event)`), never importing `EventBus` directly. The Harness creates the bus and passes it down.

### Event Types (21)

Session: `session.started`, `session.ended`. Turn: `turn.started`, `turn.completed`. FSM: `loop.started`, `loop.stopped`, `reasoning.started`, `reasoning.finished`, `tool_execution.started`, `tool_execution.finished`. Business: `llm.response`, `tool.succeeded`, `tool.failed`, `tool.progress`, `context.compressing` (reserved), `context.compressed`, `error`. Subagent: `subagent.started`, `subagent.stopped`. Task: `task.created`, `task.completed`.

All events embed `BaseEvent` (type, timestamp, runner ID) and are JSON-serializable.

### Subscribing

Programmatic: `bus.Subscribe(bufSize)` returns `<-chan Event`. Type-switch on concrete event structs.

Hooks: `events.Hooks` struct has one typed `func(XxxEvent)` field per event type (all optional). `events.StartHooks(bus, hooks)` subscribes with a buffer of 64, dispatches in a goroutine, and returns a `stop func()` that unsubscribes to end dispatch; `Harness.Shutdown` calls it to stop hook dispatch.

### Emit Sites

Runner (`agent_runner.go`): emits turn, loop, reasoning, tool execution, LLM response, compression, and error events. Harness (`harness.go`): emits session events. Subagent (`subagent_factory.go`): emits subagent lifecycle events. Todo (`todo_file.go`): emits task lifecycle events.

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
