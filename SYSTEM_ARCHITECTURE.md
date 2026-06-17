# System Architecture

## Design Goals

**Three-layer separation: Harness → AgentRunner → Agent.**

- **Harness** — the outermost shell. CLI/TUI, process lifecycle, session management, user I/O. Knows how to talk to humans. Does not know how to run an agent loop.
- **AgentRunner** — the reusable loop primitive. Owns the FSM, drives perception→action→observation, dispatches tools via the registry. Knows how to run *any* agent through the cycle. Does not know about CLI, sessions, or users.
- **Agent** — the reasoning engine. Decides what to do, which tools to call, how to interpret results. Talks to the LLM. Does not touch the filesystem or manage processes directly.

The Harness creates an AgentRunner for the main session. A subagent spawns a fresh AgentRunner with its own FSM and message history — same registry, no Harness involved. This separation means you can swap the agent (different LLM, different strategy) without touching execution infrastructure, swap the harness (CLI → server, local → remote) without touching decision logic, and reuse the loop for subagents without coupling them to the CLI layer.

**The loop never changes.** Perception → action → observation is the single primitive. New capabilities are added by registering tools or wrapping the loop with new mechanisms (planning, subagents, context compression) — never by modifying the loop itself.

**Tools are the only extension point.** Adding a capability = one registry entry, zero loop changes. Tool descriptions are instructions, not documentation — precise wording controls model tool selection. Tools never throw; errors are strings the agent interprets.

**Provider agnosticism.** All LLM interaction flows through canonical types (`Message`, `ContentBlock`, `CompletionRequest/Response`). Provider implementations convert to/from SDK-specific types. Swapping providers requires zero changes above the provider layer.

---

Go module: `tenzing-agent` (go 1.25.9)

```
cmd/app/main.go                         Entry point (stub — TODO: wire harness)

internal/
├── agent/agent.go                      Empty package (placeholder)
├── errors/errors.go                    Wrap() helper
├── harness/                            Core loop & orchestration
│   ├── agent.go                        Agent interface + ReasoningResult
│   ├── agent_loop.go                   FSM-driven perception-action loop
│   ├── harness.go                      Harness struct, constructor, system prompt
│   └── session.go                      REPL stdin→loop→stdout driver
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
├── tools/                              Tool dispatch system
│   └── registry.go                     Name→Definition map, Execute()
└── utils/strings.go                    Generic Strings() helper

internal/tools/tooldef/                 Tool implementations
├── definition.go                       Definition interface, Schema, ToolCall, ToolResult
├── tool_bash.go                        Shell command execution (120s timeout)
├── tool_read.go                        File read with line numbers
├── tool_write.go                       File write with pre-write snapshot
├── tool_edit.go                        String replacement in file
├── tool_grep.go                        Regex search across files (cap 500)
├── tool_glob.go                        File pattern matching
├── tool_revert.go                      Restore file from snapshot
├── tool_todowrite.go                   Write plan as JSON task list
├── tool_todoupdate.go                  Mark task status (done/in_progress/blocked)
├── tool_todoread.go                    Read and display current plan
├── todo.go                             Todo file I/O + reminder formatting
├── snapshot_store.go                   In-memory file snapshot store
├── file_tracker.go                     Read-before-edit enforcement (content hashing)
└── fsutil.go                           Atomic file writes, per-path locks
```

## AgentRunner (Core Loop)

The agent loop is an FSM-driven perception→action→observation cycle. The runner never branches on model output beyond `stop_reason`. Currently implemented as methods on `Harness` — will be extracted to its own `AgentRunner` type as part of Phase 1.4 (subagent support).

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

Six states, six transitions. `Reset` can fire from any state except `started`.

**Known issue:** FSM is a package-level singleton (`var loopFSM`). Concurrent or nested `RunLoop` calls (e.g. subagents) would corrupt shared state. Must be moved to per-AgentRunner instance as part of the extraction.

### RunLoop Flow

```
RunLoop(ctx, input string) → (string, error)

1. Reset FSM
2. Loop:
   a. Check ctx cancellation
   b. StartReasoning → agent.DoReasoning(inputs, reminders) → FinishReasoning
   c. If ToolCall == nil → Stop → return FinalAnswer
   d. StartToolExecution → registry.Execute(toolName, args) → FinishToolExecution
   e. Fire hooks.OnToolCall(name, input, output)
   f. Append tool result to inputs
   g. Append todo reminder to inputs (if .agent_todo.json exists)
   h. Loop to 2a
3. On error: Reset FSM, return error
```

Exit: model produces `ReasoningResult{ToolCall: nil}` (final answer).
Error: ctx canceled, DoReasoning error, or tool execution error.

## Agent Interface

```go
type Agent interface {
    DoReasoning(inputs []string, systemReminders []string) (ReasoningResult, error)
}

type ReasoningResult struct {
    ToolCall    *tooldef.ToolCall   // nil → final answer ready
    FinalAnswer string              // populated when ToolCall is nil
}
```

The AgentRunner owns the loop; `Agent` owns the LLM interaction. `inputs` accumulates user message + tool results as raw strings. `systemReminders` carries the current todo plan state.

**Gap:** `Agent` interface has no access to tool definitions. The `DoReasoning` implementation must get `[]provider.ToolDefinition` from somewhere external to this interface (e.g. constructor injection). This is not yet wired up — `cmd/app/main.go` is a stub.

## Harness

The outermost shell — CLI/session/process concerns only. Currently also contains the AgentRunner logic (loop + FSM); these will be extracted into a separate `AgentRunner` type.

```go
type Harness struct {
    agent        Agent              // will move to AgentRunner
    toolRegistry *tools.Registry    // will move to AgentRunner
    hooks        Hooks              // will move to AgentRunner
    cwd          string             // shared by both layers
    systemPrompt string             // will move to AgentRunner
}

type Hooks struct {
    OnToolCall func(name string, input string, output string)
}
```

Constructor: `New(agent, registry, hooks, cwd) → (*Harness, error)`

### Session

`RunSession(ctx, in io.Reader, out io.Writer)` — line-oriented REPL. Reads stdin, skips empty lines, handles `q`/`exit`, calls `RunLoop`, prints answer. No prompt display or history. This is the true Harness responsibility — everything below it (loop, tool dispatch, reminders) belongs to AgentRunner.

### System Prompt & Reminders

Default system prompt enforces TodoWrite-first planning:
```
"You are a coding agent at {cwd}. Before working on any multi-step task,
 ALWAYS call TodoWrite first..."
```

`buildSystemReminders()` reads `.agent_todo.json` and injects current plan state as a `<system-reminder>` block after every tool call. These are AgentRunner concerns (they shape loop behavior, not session management) and will move with the extraction.

## Tool System

### Registry

```go
type Registry struct {
    tools map[string]tooldef.Definition
}
```

- `Register(def)` — adds tool, fails if name exists
- `Execute(ctx, name, exctx)` — lookup + run, returns `ToolResult`
- `NewDefaultRegistry()` — pre-populates 10 tools

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

### Tool Inventory (10 tools)

| Tool | Description | Key behavior |
|------|-------------|--------------|
| `bash` | Shell command | 120s timeout, exit code in output |
| `Read` | File contents | Line-numbered output, default 2000 lines |
| `Write` | Write file | Snapshots previous content first |
| `Edit` | String replace | Unique match required unless `replace_all` |
| `Grep` | Regex search | Caps at 500 matches |
| `Glob` | File patterns | Supports `**` wildcard |
| `Revert` | Restore file | Pops from snapshot store (one-shot) |
| `TodoWrite` | Write plan | JSON array → `.agent_todo.json`, all `pending` |
| `TodoUpdate` | Update status | By index: `done`, `in_progress`, `blocked` |
| `TodoRead` | Show plan | Formatted task list |

### Shared Infrastructure

**SnapshotStore** — in-memory `map[string][]byte` behind mutex. `Write` saves before overwriting; `Revert` pops (clears entry). Shared across all tools within a registry.

**FileTracker** — SHA-256 content stamps per file path. Enforces read-before-edit (not yet wired into Edit/Write tools, but available). Returns `ErrNotRead` or `ErrChangedSinceRead`.

**fsutil** — per-path mutex locks (`sync.Map`) for concurrent file access. `writeFileAtomic` uses temp-file + rename for crash safety.

**todo.go** — `ReadTodoReminder(workingDir)` reads `.agent_todo.json`, formats as `<system-reminder>` block. Returns `""` if no file. Called by harness after each tool execution and in `buildSystemReminders()`.

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

## What's Not Built Yet

- `cmd/app/main.go` is a stub — no Agent implementation wired to a provider
- `internal/agent/agent.go` is an empty package
- `Agent.DoReasoning` has no way to receive tool definitions from the registry
- AgentRunner not yet extracted from Harness — FSM is a package-level singleton, loop logic is Harness methods
- FSM must become per-AgentRunner instance (blocks subagent/concurrent loop work)
- `FileTracker` exists but isn't wired into Edit/Write tools
- No subagent support (Phase 1.4)
- No context compression, skill loading, async execution (Phases 2-6)
