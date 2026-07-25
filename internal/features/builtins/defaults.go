package builtins

import "github.com/tab58/tenzing-agent-harness/internal/core/tooldef"

// Defaults returns the standard builtin tool set.
func Defaults() []tooldef.Definition {
	return []tooldef.Definition{
		&BashTool{},
		&ReadTool{},
		&EditTool{},
		&GrepTool{},
		&GlobTool{},
	}
}
