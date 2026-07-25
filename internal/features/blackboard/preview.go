// Package blackboard provides a persistent shared Python REPL that the main
// agent and all subagents access through the `repl` tool. Large subagent
// results are deposited here and the orchestrating agent receives a
// fixed-truncation preview instead of the full text.
package blackboard

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultHeadChars and DefaultTailChars are the fixed-truncation preview
	// sizes returned to the orchestrating agent.
	DefaultHeadChars = 1500
	DefaultTailChars = 500
)

// Preview is the fixed-truncation summary of a value stored on the
// blackboard. Tail is empty when the value fit entirely in Head.
type Preview struct {
	Slot  string
	Key   string
	Chars int
	Lines int
	Head  string
	Tail  string
}

// NewPreview builds a Preview of value. Values no longer than
// headChars+tailChars are kept whole in Head.
// ponytail: byte slicing may split a multibyte rune at the cut point;
// harmless for LLM previews, revisit if previews must be valid UTF-8.
func NewPreview(slot, key, value string, headChars, tailChars int) Preview {
	p := Preview{
		Slot:  slot,
		Key:   key,
		// rune count, not len(value): agents measure this value with
		// python len(), which counts code points — the two must agree.
		Chars: utf8.RuneCountInString(value),
		Lines: strings.Count(value, "\n") + 1,
	}
	if len(value) <= headChars+tailChars {
		p.Head = value
		return p
	}
	p.Head = value[:headChars]
	p.Tail = value[len(value)-tailChars:]
	return p
}

// String renders the preview as the text returned to the orchestrating
// agent, including instructions for inspecting the full value.
func (p Preview) String() string {
	if p.Tail == "" {
		return p.Head
	}
	ref := fmt.Sprintf("bb[%q][%q]", p.Slot, p.Key)
	return fmt.Sprintf(
		"%s: %d chars, %d lines. Full value lives in the shared REPL — inspect it with the repl tool, e.g. print(peek(%s, 1500)) or print(bb_grep(r\"pattern\", %s)).\n--- head ---\n%s\n--- tail ---\n%s",
		ref, p.Chars, p.Lines, ref, ref, p.Head, p.Tail)
}

// truncateOutput bounds REPL stdout shown to an agent, keeping head and tail
// around an omission marker.
func truncateOutput(s string, head, tail int) string {
	if len(s) <= head+tail {
		return s
	}
	omitted := utf8.RuneCountInString(s[head : len(s)-tail])
	return s[:head] +
		fmt.Sprintf("\n[... %d chars omitted — print slices or use peek() to see more ...]\n", omitted) +
		s[len(s)-tail:]
}
