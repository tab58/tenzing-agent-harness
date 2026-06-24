# System Architecture

## Design Goals

**Three-layer separation: Harness → AgentRunner → Agent.**

- **Harness** — the outermost shell. CLI/TUI, process lifecycle, session management, user I/O. Knows how to talk to humans. Does not know how to run an agent loop.
- **AgentRunner** — the reusable loop primitive. Owns the FSM, drives perception→action→observation, dispatches tools via the registry. Knows how to run *any* agent through the cycle. Does not know about CLI, sessions, or users.
- **Agent** — the reasoning engine. Decides what to do, which tools to call, how to interpret results. Talks to the LLM. Does not touch the filesystem or manage processes directly.

The Harness creates an AgentRunner for the main session. A subagent spawns a fresh AgentRunner with its own FSM and message history. This separation means you can swap the agent (different LLM, different strategy) without touching execution infrastructure, swap the harness (CLI → server, local → remote) without touching decision logic, and reuse the loop for subagents without coupling them to the CLI layer.

**Everything that isn't invariant is configurable.** The loop (perception → action → observation), the FSM, and the dispatch pattern (`name → handler(input)`) are structural invariants — they never change. Everything else is injectable via `AgentRunnerConfig`: which agent, which tools, which system prompt, which reminders get injected, how subagents are constructed. This allows piece-by-piece optimization of the harness without touching the loop. Swap the model, swap the reminder strategy, give subagents different tools or a different provider — all through configuration, not code changes.

**`AgentRunnerConfig` is the single DI surface.** All non-invariant runner behavior flows through this struct. The Harness is deliberately thin — it wires a main runner, optionally registers the subagent tool, and owns the session REPL. Nothing else.

**The loop never changes.** Perception → action → observation is the single primitive. New capabilities are added by registering tools or wrapping the loop with new mechanisms (planning, subagents, context compression) — never by modifying the loop itself.

**Tools are the only extension point.** Adding a capability = one registry entry, zero loop changes. Tool descriptions are instructions, not documentation — precise wording controls model tool selection. Tools never throw; errors are strings the agent interprets.

**Provider agnosticism.** All LLM interaction flows through canonical types (`Message`, `ContentBlock`, `CompletionRequest/Response`). Provider implementations convert to/from SDK-specific types. Swapping providers requires zero changes above the provider layer.

**Risk changes the process.** Tools carry a risk classification (read_only, draft, external_write). Read-only tools execute autonomously. Draft tools simulate without side effects. External writes require human approval before finalization. This is the draft-commit pattern: dangerous actions are first drafted, then explicitly committed. The permission check happens in the Runner before tool execution — not in the tool itself, not in the Agent. Injectable via `AgentRunnerConfig` so different runners can enforce different policies. *(Not yet implemented — Phase 4.)*

**Long tasks have budgets.** Every agent loop enforces hard limits: step budget (max iterations), time budget (wall-clock), token budget (per turn and cumulative), and cost budget (USD). When a budget is exhausted, the harness terminates gracefully and returns a structured result — not a crash. Budget checks sit alongside `ctx.Err()` at the top of the loop, injectable via config. Without budgets, a runaway loop burns money silently. *(Not yet implemented — Phase 4.)*

**Context is assembled, not dumped.** The system prompt is ordered by stability for cache efficiency: Layer 0 (system policies, stable prefix, cached) → Layer 1 (skill definitions, rarely change, cached) → Layer 2 (session instructions, per conversation, not cached) → Layer 3 (JIT-retrieved tool outputs, fresh, not cached). Untrusted data (user input, tool output from external sources) is marked with trust labels so the harness can treat it differently. *(Partially implemented — skills use progressive disclosure, but no cache-aware ordering or trust labels yet.)*

**Registries own implementations, Agent gets metadata.** Tools and skills follow the same wiring pattern: registries load from disk at startup, the Agent receives metadata (tool definitions, skill names/descriptions) as data via `AgentConfig`, and the AgentRunner dispatches execution at runtime. The Agent never touches a registry directly — it tells the LLM what capabilities exist, the Runner actually runs them.

```
main.go
  ├── skills.NewRegistry(skillsDir)     → discovers skill metadata from disk
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

Go module: `tenzing-agent` (go 1.25.9)

```
cmd/app/main.go                         Entry point — wires agent, tools, skills, task graph

internal/
├── agent/                              Concrete Agent implementation
│   ├── agent.go                        Agent struct, AgentConfig, DoReasoning, NewWithCompressor
│   └── context/                        Knowledge & context management
│       ├── compression.go              Three-layer context compression + memory persistence
│       ├── context.go                  Context struct (placeholder)
│       └── graph.go                    TaskGraph — persistent dependency-aware task graph
├── errors/errors.go                    Wrap() helper
├── harness/                            Core loop & orchestration
│   ├── agent.go                        Agent interface + ReasoningResult
│   ├── agent_runner.go                 AgentRunner: FSM-driven loop, DI config
│   ├── loop_fsm.go                     Per-runner FSM (6 states, 6 transitions)
│   ├── harness.go                      Thin orchestrator, config types, RunSession REPL
│   ├── defaults.go                     DefaultReminderBuilder, DefaultMainConfig
│   ├── prompts/                        System prompt construction
│   ├── skills/                         Skill discovery & lazy loading
│   │   └── registry.go                 Discover frontmatter at startup, Load on demand
│   ├── tools/                          Tool dispatch system
│   │   └── registry.go                 Name→Definition map, Execute(), GetDefaultToolDefs()
│   └── rlm/                            Recursive Language Model engine
│       ├── bootstrap.py                Embedded Python REPL (//go:embed)
│       ├── repl.go                     Python subprocess + JSON-line IPC
│       ├── engine.go                   RLM loop: LLM→code→REPL→feedback→repeat
│       ├── truncate.go                 First/last-half truncation
│       └── prompts/
│           └── system.md.tmpl          RLM system prompt template
├── provider/                           LLM abstraction layer
│   ├── llm.go                          LLM interface (6 implementations)
│   ├── chat.go                         Provider-agnostic message types
│   ├── logger.go                       Optional diagnostics logger
│   ├── anthropic.go                    Anthropic SDK wrapper
│   ├── openai.go                       OpenAI SDK wrapper
│   ├── openai_compat.go               Shared OpenAI-compatible base
│   ├── openai_compat_convert.go        Message/tool conversion helpers
│   ├── openai_compat_retry.go          Rate-limit retry with exponential backoff
│   ├── cerebras.go                     Cerebras (OpenAI-compatible)
│   ├── lightning.go                    Lightning (OpenAI-compatible)
│   ├── ollama.go                       Ollama (direct HTTP)
│   ├── openrouter.go                   OpenRouter (multi-backend routing)
│   └── utils/
│       ├── token_bucket.go             Token-bucket rate limiter
│       └── semaphore.go                Concurrency semaphore
└── utils/strings.go                    Generic Strings() helper

internal/harness/tools/tooldef/         Tool implementations
├── definition.go                       Definition interface, Schema, ToolCall, ToolResult
├── tool_bash.go                        Shell command execution (120s timeout)
├── tool_read.go                        File read with line numbers
├── tool_write.go                       File write with pre-write snapshot
├── tool_edit.go                        String replacement in file
├── tool_grep.go                        Regex search across files (cap 500)
├── tool_glob.go                        File pattern matching
├── tool_revert.go                      Restore file from snapshot
├── tool_rlm.go                         RLM tool (Python REPL loop wrapper)
├── tool_list_skills.go                 List available skills (interface: SkillLister)
├── tool_load_skill.go                  Load skill content (interface: SkillContentLoader)
├── tool_task_create.go                 Create task in graph (interface: TaskCreator)
├── tool_task_next.go                   Get next unblocked task (interface: TaskNexter)
├── tool_task_update.go                 Update task status (interface: TaskUpdater)
├── tool_task_list.go                   List all tasks (interface: TaskLister)
├── tool_todowrite.go                   Write plan as JSON task list
├── tool_todoupdate.go                  Mark task status (done/in_progress/blocked)
├── tool_todoread.go                    Read and display current plan
├── todo.go                             Todo file I/O + reminder formatting
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
   d. If ToolCall == nil → Stop → return FinalAnswer
   e. StartToolExecution → registry.Execute(toolName, args) → FinishToolExecution
   f. Fire hooks.OnToolCall(name, input, output)
   g. Append tool result to inputs
   h. Loop to 2a
3. On error: Reset FSM, return error
```

Exit: model produces `ReasoningResult{ToolCall: nil}` (final answer).
Error: ctx canceled, DoReasoning error, or tool execution error.

## Agent Interface

```go
type Agent interface {
    DoReasoning(ctx context.Context, inputs []string, systemReminders []string) (ReasoningResult, error)
}

type ReasoningResult struct {
    ToolCall    *tooldef.ToolCall   // nil → final answer ready
    FinalAnswer string              // populated when ToolCall is nil
}
```

The AgentRunner owns the loop; `Agent` owns the LLM interaction. `inputs` accumulates user message + tool results as raw strings. `systemReminders` carries the current todo plan state.

The concrete implementation lives in `internal/agent/`. Tool definitions are injected at construction via `AgentConfig` — the tool registry converts its definitions to `[]provider.ToolDefinition` via `Registry.ProviderDefinitions()`.

```go
type AgentConfig struct {
    Model           provider.LLM
    ToolDefinitions []provider.ToolDefinition
    SystemPrompt    string
    Skills          map[string]string // name → description, injected into system prompt
}
```

`Agent` manages conversation history as `[]provider.Message`, builds `CompletionRequest` each reasoning cycle, and parses `CompletionResponse` into `ReasoningResult`.

Two constructors:
- `New(cfg)` — basic agent, no compression
- `NewWithCompressor(cfg, cwd)` — adds context compression. Loads prior memory from `.agent_memory.md` into history at startup. After each `DoReasoning` call, runs `MaybeCompress` to keep history within bounds.

## Harness

Deliberately thin orchestrator. Holds the main runner, optionally registers the subagent tool, owns the session REPL. No loop logic, no tool dispatch, no reminders.

```go
type Harness struct {
    mainRunner *AgentRunner
}

type HarnessConfig struct {
    MainRunner AgentRunnerConfig
    RLM        *RLMConfig // nil = rlm tool not registered
}

type RLMConfig struct {
    RootModel provider.LLM // LLM for the root RLM reasoning loop
    SubModel  provider.LLM // LLM for llm_query calls inside REPL; nil = use RootModel
    MaxDepth  int          // 0=REPL only, 1=llm_query, 2+=rlm_query recursive
}

type Hooks struct {
    OnToolCall func(name string, input string, output string)
}
```

Constructor: `New(HarnessConfig) → (*Harness, error)`. If `RLM` config is provided, creates an RLM engine and registers the `rlm` tool in the main runner's registry. Appends RLM guidance to the system prompt. Creates the main `AgentRunner` from `MainRunner` config.

### RLM Architecture

The RLM (Recursive Language Model) is the sole delegation mechanism, implementing Zhang et al. (2025). The main agent delegates via the `rlm` tool. Inside the REPL, `llm_query()` provides single-shot sub-LM calls, and `rlm_query()` (at depth>1) spawns recursive child RLM loops. At max depth, `rlm_query` falls back to `llm_query`.

Convenience helpers:
- `DefaultMainConfig(agent, registry, hooks, cwd)` — default system prompt + default reminders
- `DefaultReminderBuilder()` — injects todo plan state

### Session

`RunSession(ctx, in io.Reader, out io.Writer)` — line-oriented REPL. Reads stdin, skips empty lines, handles `q`/`exit`, calls `RunLoop`, prints answer. This is the true Harness responsibility — everything below it belongs to AgentRunner.

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
- `ProviderDefinitions()` — convert registered tools to `[]provider.ToolDefinition` (name, description, JSON schema) for injection into Agent

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

### Tool Inventory (18 tools)

| Tool | Description | Key behavior |
|------|-------------|--------------|
| `bash` | Shell command | 120s timeout, exit code in output |
| `Read` | File contents | Line-numbered output, default 2000 lines |
| `Write` | Write file | Snapshots previous content first |
| `Edit` | String replace | Unique match required unless `replace_all` |
| `Grep` | Regex search | Caps at 500 matches |
| `Glob` | File patterns | Supports `**` wildcard |
| `Revert` | Restore file | Pops from snapshot store (one-shot) |
| `rlm` | Recursive Language Model | Python REPL loop with llm_query, rlm_query (recursive), file access, FINAL termination |
| `list_skills` | List skills | Returns name→description map from skill registry |
| `load_skill` | Load skill | Lazy-loads full `SKILL.md` content by name |
| `task_create` | Create task | Persistent task graph, validates dependencies |
| `task_next` | Next task | Highest-priority pending with all deps done |
| `task_update` | Update task | Status change by ID or prefix match |
| `task_list` | List tasks | All tasks with IDs, statuses, priorities, deps |
| `TodoWrite` | Write plan | JSON array → `.agent_todo.json`, all `pending` |
| `TodoUpdate` | Update status | By index: `done`, `in_progress`, `blocked` |
| `TodoRead` | Show plan | Formatted task list |

### Shared Infrastructure

**SnapshotStore** — in-memory `map[string][]byte` behind mutex. `Write` saves before overwriting; `Revert` pops (clears entry). Shared across all tools within a registry.

**FileTracker** — SHA-256 content stamps per file path. Enforces read-before-edit (not yet wired into Edit/Write tools, but available). Returns `ErrNotRead` or `ErrChangedSinceRead`.

**fsutil** — per-path mutex locks (`sync.Map`) for concurrent file access. `writeFileAtomic` uses temp-file + rename for crash safety.

**todo.go** — `ReadTodoReminder(workingDir)` reads `.agent_todo.json`, formats as `<system-reminder>` block. Returns `""` if no file. Called by harness after each tool execution and in `buildSystemReminders()`.

## Skill System

### Registry

```go
type Definition struct {
    Name        string
    Description string
    path        string   // unexported — callers use Load()
}

type Registry struct {
    skills    map[string]Definition
    skillsDir string
}
```

- `NewRegistry(skillsDir)` — scans directory at construction, discovers all skills via frontmatter parsing
- `Discover()` — returns a copy of the skills map (metadata only, no file content)
- `Load(name)` — lazy-loads full `SKILL.md` content from disk on demand

Skills are subdirectories of `skillsDir`, each containing a `SKILL.md` with YAML frontmatter (`name`, `description`) between `---` fences. Discovery reads only frontmatter — zero full-body reads at startup.

Skill metadata is passed as data into `AgentConfig` for system prompt injection. The `SkillRegistry` itself is passed to `list_skills`/`load_skill` tool constructors for runtime access.

## Task Graph

Persistent, dependency-aware task tracking in `internal/agent/context/graph.go`. Persists to `.agent_tasks.json` in the working directory. Survives session restarts.

```go
type Task struct {
    ID, Description, Status, Result string
    Priority    TaskPriority       // high, medium, low
    DependsOn   []string           // task IDs
}

type TaskGraph struct { file string; mu sync.Mutex }
```

- `CreateTask(desc, dependsOn, priority)` — validates dependencies exist, assigns random hex ID
- `NextTask()` — returns highest-priority pending task with all deps done (JSON string)
- `UpdateTask(taskID, status, result)` — supports prefix matching on task ID
- `ListTasks()` — all tasks as indented JSON
- `Reminder()` — formatted `<system-reminder>` block injected per turn

Four tools (`task_create`, `task_next`, `task_update`, `task_list`) use narrow interfaces (`TaskCreator`, `TaskNexter`, `TaskUpdater`, `TaskLister`) to avoid import cycles. `TaskGraph` satisfies all four.

Coexists with `TodoWrite`/`TodoUpdate`/`TodoRead` — task graph is for persistent multi-step work with dependencies; todo is simpler session-scoped planning.

## Context Compression

Three-layer compression in `internal/agent/context/compression.go`. Prevents unbounded history growth during long sessions.

```go
type Compressor struct { llm provider.LLM; memoryFile string }
```

- `EstimateSize(messages)` — sums char lengths across all content blocks
- `MaybeCompress(ctx, messages)` — triggers when history exceeds 40k chars AND more than 6 messages. Splits at `len-6`, summarizes older portion via LLM, persists summary to `.agent_memory.md`, returns `[summary, ack, ...recent_6]`
- `LoadMemory()` / `SaveMemory(summary)` — disk persistence with timestamp header

Integrated in `Agent.DoReasoning` — runs after each assistant response. `NewWithCompressor` loads prior memory at startup, seeding history with previous session context.

Compression is non-fatal: LLM errors are logged, original history preserved.

## Recursive Language Model (RLM) Engine

Full RLM implementation based on Zhang et al. (2025). Processes arbitrarily large inputs by loading them into a Python REPL as a variable. The model writes Python code to programmatically decompose, analyze (via `llm_query()` calls in loops), and aggregate results. Supports recursive depth: `rlm_query()` spawns child RLM loops at depth>1, falling back to `llm_query()` at max depth.

Architecture: Engine (Go) drives a loop — call root LLM → extract ```repl code blocks → send to Python subprocess → handle callbacks (llm_query, rlm_query, read_file, grep_file, list_files) over JSON-line protocol on stdin/stdout → capture stdout → truncate → feed back to LLM → repeat until `FINAL()`.

Single tool: `rlm` — the sole delegation mechanism. All sub-agent communication flows through the REPL. Depth parameter controls recursion:
- depth=0: REPL only, no sub-LLM calls
- depth=1: `llm_query()` available (default)
- depth=2+: `rlm_query()` available, spawning child RLM loops

Wired via `HarnessConfig.RLM`. Configurable sub-LLM model via `RLMConfig.SubModel` (defaults to RootModel). Python 3 required on PATH.

## Provider Layer

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

**TokenBucket** (`provider/utils/token_bucket.go`) — token-bucket algorithm with configurable:
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
| `anthropics/anthropic-sdk-go` | Anthropic API client |
| `openai/openai-go/v3` | OpenAI API client |
| `looplab/fsm` | Finite state machine for loop transitions |
| `golang.org/x/sync` | Weighted semaphore for concurrency limiting |

## Known Design Issues

- **`FileTracker`** — exists but isn't wired into Edit/Write tools.

## What's Not Built Yet

- `cmd/app/main.go` wires everything but Agent model is `nil` — needs a provider instance
- No async execution, multi-agent teams (Phase 3)
- No permission governance, event bus, session persistence (Phase 4)
- No parallel tool execution, prompt caching, MCP integration (Phase 5)
