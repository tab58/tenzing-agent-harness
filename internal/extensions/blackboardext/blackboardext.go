// Package blackboardext surfaces the shared blackboard REPL as a
// core.Extension: the `repl` tool via core.ToolProvider and lifecycle
// shutdown via core.SessionEndHook. The *blackboard.Blackboard instance is
// constructed at the composition root and SHARED — the main agent and each
// subagent get their own Ext wrapping the same blackboard under their own
// agent ID (write-own-slot enforcement lives in the blackboard itself).
package blackboardext

import (
	"context"

	"github.com/tab58/tenzing-agent-harness/internal/adapters/toolport"
	"github.com/tab58/tenzing-agent-harness/internal/core"
	"github.com/tab58/tenzing-agent-harness/internal/harness/blackboard"
)

const origin = "extension:blackboard"

var (
	_ core.Extension      = (*Ext)(nil)
	_ core.ToolProvider   = (*Ext)(nil)
	_ core.SessionEndHook = (*Ext)(nil)
)

type Ext struct {
	bb      *blackboard.Blackboard
	agentID string
}

func New(bb *blackboard.Blackboard, agentID string) *Ext {
	return &Ext{bb: bb, agentID: agentID}
}

func (e *Ext) Name() string { return "blackboard" }

// Tools wraps the existing REPL tool implementation — same behavior, new
// mount path through the composite ToolPort.
func (e *Ext) Tools() []core.ToolSpec {
	return []core.ToolSpec{
		toolport.SpecFromDefinition(blackboard.NewREPLTool(e.bb, e.agentID), origin),
	}
}

// SessionEnd shuts the shared REPL subprocess down. The blackboard starts
// lazily, so there is no SessionStart counterpart.
func (e *Ext) SessionEnd(ctx context.Context) error {
	return e.bb.Close()
}
