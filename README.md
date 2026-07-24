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
- **Context compression** — three-layer system: recent messages kept verbatim, older messages summarized via LLM, summaries persisted per conversation to `<UserConfigDir>/tenzing/.agent_memory-<date>-<agent-id>.md` (resume with `WithConversationID`)
- **Shared blackboard REPL** — one persistent, sandboxed Python REPL shared by the main agent and subagents, for processing inputs beyond the context window (`llm_query`/`llm_batch` sub-LLM calls in loops over shared state)
- **Todo planning** — model commits a plan before acting (dependency-aware, in-memory task board, one plan per harness or subagent), progress re-injected as reminders after every tool call

## Prerequisites

- Go 1.25.9+
- [Task](https://taskfile.dev) (optional, for `task app`)
- Python 3 (for the blackboard REPL)

## Quick Start

```bash
# Build
go build ./...

# Run the app (HTTP/SSE server with embedded chat UI)
task app
# or directly:
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
  app/                  Entry point — HTTP/SSE server with embedded chat UI

internal/
  core/                 Invariant domain: types, FSM, events, loop, all ports, the Agent
                        contract, tool authoring contract (core/tooldef); imports nothing
                        from internal/
  adapters/             Port implementations (import core only)
    agent/              core.Agent: stateless ModelPort-side brain
    contextstore/       ContextPort: history, pairing, compression (+ compressor/)
    eventbus/           core.Emitter implementation + typed Hooks dispatcher
    toolport/           ToolPort: Composite/Wrap + the native tool Registry
  features/             core.Extension implementations (import core only), each with an
                        ext.go registration: advisor, blackboard, budgets, builtins,
                        mcp, permissions, prompts, reminders, skills, snapshot, todo
  harness/              Composition root: wiring, config, memory persistence
    runner/             AgentRunner facade over core.Loop
    subagent/           Subagent spawning — a child composition root
  app/
    nexus/              Input channel monitoring (file-tail/command/webhook → agent wake-ups)
      tools/            Channel tools (list_channels, read_channel, search_channel)

skills/                 Skill definitions (SKILL.md files)
docs/                   Forward-looking design docs and reference papers
pkg/tenzing/            Public API facade (aliases over internal/harness)
```

## Docs

- `SYSTEM_ARCHITECTURE.md` — full system design
- `AGENTS.md` — conventions for contributing (tools, providers, skills, testing)
- `CLAUDE.md` — AI agent working guidelines
- `docs/PHASE_3_IMPL.md` — async execution and multi-agent team design (not yet implemented)
- `docs/superpowers/specs/2025-06-25-permission-gates-design.md` — permission gates design (not yet implemented)
