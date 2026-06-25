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
task repl               # run interactive REPL
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
| Harness (`harness.go`, `agent.go`, `defaults.go`)              | AgentRunner, config wiring, session REPL | LLM, tool implementations             |
| AgentRunner (`agent_runner.go`, `loop_fsm.go`)                 | Agent interface, tool registry, FSM      | CLI, sessions, users                  |
| Agent (`internal/agent/agent.go`, `internal/agent/context/`)   | LLM provider, message types, compression | Filesystem, processes, tools directly |

**Never import upward.** Tools don't import harness. Agent doesn't import runner. If you need cross-layer communication, use an interface injected via config.

## Adding Tools

1. Create `internal/harness/tools/tooldef/tool_<name>.go`
2. Implement `tooldef.Definition` interface: `Name()`, `Description()`, `Schema()`, `Execute()`
3. Register: most tools go in `GetDefaultToolDefs()` in `internal/harness/tools/registry.go`. The `rlm` tool is wired conditionally in `harness.New()` via `HarnessConfig.RLM`
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

## Adding Providers

1. Create `internal/provider/<name>.go`
2. Implement `provider.LLM` interface (6 methods, 6 existing implementations: Anthropic, OpenAI, Cerebras, Lightning, OpenRouter, Ollama)
3. For OpenAI-compatible APIs, embed `OpenAICompatBase` and override as needed
4. Add compile-time interface check: `var _ LLM = (*NewProvider)(nil)`
5. Conversion between canonical types (`Message`, `ContentBlock`) and SDK types stays in the provider file
6. Rate limiting utilities live in `internal/provider/utils/` (token bucket, semaphore)

## Configuration & DI

All non-invariant behavior flows through `AgentRunnerConfig`. To change runner behavior:

- Swap the Agent (different model/provider)
- Swap the ToolRegistry (different tool set)
- Swap the SystemPrompt
- Swap the ReminderBuilder
- Swap the Hooks

Don't modify the loop. Don't add fields to the runner struct. Configure via `AgentRunnerConfig`.

## FSM Rules

Six states, six transitions. Don't add states or transitions without updating `SYSTEM_ARCHITECTURE.md`.

The FSM is per-runner instance — subagents and concurrent loops don't share state.

## File Conventions

| Pattern                  | Location                                              |
| ------------------------ | ----------------------------------------------------- |
| Tool implementations     | `internal/harness/tools/tooldef/tool_*.go`            |
| Provider implementations | `internal/provider/*.go`                              |
| Provider utilities       | `internal/provider/utils/*.go`                        |
| Prompt templates         | `internal/harness/prompts/*.gotmpl`                   |
| RLM engine               | `internal/harness/rlm/` (Fetcher, Querier, Engine)    |
| Context management       | `internal/agent/context/` (compression, task graph)   |
| TUI REPL                 | `cmd/repl/`                                           |
| Test files               | Same directory as source, `*_test.go`                 |
| Shared test helpers      | `**/testutil_test.go`                                 |
| Sub-agent system         | `internal/harness/subagent/`                          |
| Embedded assets          | Adjacent to consumer (e.g. `rlm/bootstrap.py`)       |

## Testing

- Table-driven tests as standard pattern
- Test files live next to source
- Use `testutil_test.go` for shared helpers within a package
- Mock via interfaces, not concrete types
- `go test -race ./...` catches concurrency bugs — run before any change touching goroutines, channels, or shared state

## Common Mistakes to Avoid

- **Mutating the loop.** New capabilities = new tools or new config, never loop changes
- **Importing upward.** Tools → harness or agent → runner = architecture violation
- **Wide interfaces in tools.** Tools accept narrow interfaces (e.g. `TaskCreator`, not `*TaskGraph`)
- **Hardcoding provider behavior.** All provider differences stay in the provider layer; canonical types above
- **Forgetting `SYSTEM_ARCHITECTURE.md`.** If your change affects harness structure, agent interface, tool system, provider layer, or config surface — update the architecture doc in the same PR
