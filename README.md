# tenzing-agent-harness

An AI agent harness built in Go. The harness is the environment around the model — the loop, tools, context management, and orchestration — not a framework. The core loop (perception → action → observation) never changes; capabilities grow by registering tools and layering mechanisms around the loop.

## Architecture

Three layers, strict dependency direction:

```
Harness → AgentRunner → Agent
```

- **Harness** — CLI/TUI, process lifecycle, session management, user I/O
- **AgentRunner** — reusable loop primitive with FSM, tool dispatch, reminder injection
- **Agent** — reasoning engine that talks to the LLM and decides what to do

See `SYSTEM_ARCHITECTURE.md` for the full design.

## Providers

Provider-agnostic via canonical types (`Message`, `ContentBlock`, `CompletionRequest/Response`). Supported:

- Anthropic
- OpenAI
- Cerebras
- Lightning
- OpenRouter
- Ollama

## Features

- **Tool system** — bash, read, write, edit, grep, glob, revert (file snapshots)
- **Skill system** — lazy-loaded domain knowledge via YAML-frontmatter Markdown files
- **Subagents** — spawn isolated agent loops with fresh context; only the final summary returns to the parent
- **Task graph** — persistent, dependency-aware task board (`.agent_tasks.json`) with mutex-guarded atomic operations
- **Context compression** — three-layer system: recent messages kept verbatim, older messages summarized via LLM, summaries persisted to `.agent_memory.md`
- **RLM engine** — recursive language model execution with a sandboxed Python REPL for processing inputs beyond the context window (probe → decompose → sub-LLM query → aggregate)
- **Todo planning** — model commits a plan before acting, progress re-injected as reminders after every tool call

## Prerequisites

- Go 1.25.9+
- [Task](https://taskfile.dev) (optional, for `task repl`)
- Python 3 (for the RLM REPL sandbox)

## Quick Start

```bash
# Build
go build ./...

# Run the interactive TUI REPL
task repl
# or directly:
go run ./cmd/repl

# Run headless
go run ./cmd/app
```

Set your provider API key in the environment (e.g. `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`).

## Testing

```bash
go test ./...           # unit tests
go test -race ./...     # race detector
go vet ./...            # static analysis
```

## Project Layout

```
cmd/
  app/                  Headless entry point
  repl/                 Bubble Tea TUI REPL

internal/
  agent/                Agent implementation + context management
    context/            Compression, overflow routing
    context/compressor/ Three-layer compressor + RLM router
  harness/              Core harness wiring
    runner/             AgentRunner, FSM loop
    tools/              Tool registry
    tools/tooldef/      Tool implementations (bash, read, write, edit, grep, glob)
    skills/             Skill registry + tools (list_skills, load_skill)
    subagent/           Subagent spawning
    taskgraph/          Persistent task graph + tools
    todo/               Todo planning + tools
    snapshot/           File snapshot store + write/revert tools
    rlm/                Recursive language model engine, Python REPL, sub-LM querier
    prompts/            System prompt templates
  provider/             LLM provider implementations
    utils/              Rate limiting (token bucket, semaphore)
  errors/               Error wrapping

skills/                 Skill definitions (SKILL.md files)
docs/                   Design docs and implementation plans
```

## Docs

- `SYSTEM_ARCHITECTURE.md` — full system design
- `AGENTS.md` — conventions for contributing (tools, providers, skills, testing)
- `CLAUDE.md` — AI agent working guidelines
- `docs/PHASE_3_IMPL.md` — async execution and multi-agent team design (not yet implemented)
