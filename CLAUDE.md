# tenzing-agent-harness

This repo documents itself through `AGENTS.md` files. They are the source of truth for how the codebase is laid out and how each part behaves. Treat them as authoritative over assumptions or stale memory.

## Before working

- Read the **root `AGENTS.md`** for the overview, layout, conventions, and current TODOs.
- Read the **AGENTS.md nearest to the files you're touching**. The nearest file is the most specific and wins on conflicts.
- New `AGENTS.md` files may appear over time - discover them with `find . -name AGENTS.md -not -path '*/vendor/*'` rather than assuming the set is fixed.

## Keep them current (prevent drift)

When a change alters anything an `AGENTS.md` describes, **update that `AGENTS.md` in the same change** - never leave it for later. This includes:

- Behavior changes (what a module/package does, request handling).
- Structure changes (files or directories added, removed, or renamed; the `Files`/`Layout` tables).
- Interface changes (commands, tasks in `Taskfile.yml`, env vars, ports, endpoints).
- Dependency or build changes that affect the documented build/run steps.

After editing code, ask: "Did I just make any `AGENTS.md` statement untrue?" If so, fix it. Also check sibling docs the `AGENTS.md` points to (e.g. `Taskfile.yml`, `README.md`) for the same drift.

When an `AGENTS.md` and the code disagree, the code is reality - surface the discrepancy and correct the doc.

## While working

Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

### 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:

- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

### 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:

- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:

- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:

- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:

```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.

## Notes

- Version control is handled by the repo owner - do not commit or push.
- Clean up any build artifacts you generate (compiled binaries, vendor trees, codegen output, temp files) before finishing, so they can't be committed by accident. If something must stay, add it to `.gitignore` instead of leaving it untracked.
