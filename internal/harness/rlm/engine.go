package rlm

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"text/template"
	"time"

	"tenzing-agent/internal/harness/tools/tooldef"
)

const levelTrace = slog.Level(-8)

//go:embed prompts/system.md.tmpl
var systemPromptTemplate string

var systemTmpl = template.Must(template.New("rlm_system").Parse(systemPromptTemplate))

var (
	codeBlockRe         = regexp.MustCompile("(?s)```repl\\s*\n(.*?)```")
	codeBlockFallbackRe = regexp.MustCompile("(?s)```python\\s*\n(.*?)```")
	finalQuotRe         = regexp.MustCompile(`FINAL\("([^"]+)"\)`)
	finalVarRe          = regexp.MustCompile(`FINAL_VAR\("([^"]+)"\)`)
	finalRe             = regexp.MustCompile(`FINAL\(([^)]+)\)`)
)

type ProgressEvent struct {
	Iteration  int
	Phase      string // "repl_exec", "repl_result", "llm_call", "callback"
	CodeBlock  string
	Output     string
	Depth      int
	TokensIn   int64
	TokensOut  int64
}

type EngineConfig struct {
	NewFetcher    FetcherFactory
	Querier       Querier
	WorkingDir    string
	MaxIterations int
	TruncateMax   int
	MaxDepth      int
	OnProgress    func(ProgressEvent)
}

type Engine struct {
	newFetcher    FetcherFactory
	querier       Querier
	workingDir    string
	maxIterations int
	truncateMax   int
	maxDepth      int
	currentDepth  int
	onProgress    func(ProgressEvent)
}

func NewEngine(cfg EngineConfig) (*Engine, error) {
	if cfg.NewFetcher == nil {
		return nil, fmt.Errorf("fetcher factory is required")
	}
	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = 30
	}
	truncMax := cfg.TruncateMax
	if truncMax <= 0 {
		truncMax = 2000
	}
	maxDepth := cfg.MaxDepth
	if maxDepth < 0 {
		maxDepth = 0
	}
	return &Engine{
		newFetcher:    cfg.NewFetcher,
		querier:       cfg.Querier,
		workingDir:    cfg.WorkingDir,
		maxIterations: maxIter,
		truncateMax:   truncMax,
		maxDepth:      maxDepth,
		currentDepth:  0,
		onProgress:    cfg.OnProgress,
	}, nil
}

func (e *Engine) GetTools() []tooldef.Definition {
	return []tooldef.Definition{
		NewRLMTool(e.Run),
	}
}

func (e *Engine) emitProgress(ev ProgressEvent) {
	if e.onProgress != nil {
		e.onProgress(ev)
	}
}

func (e *Engine) childEngine() *Engine {
	return &Engine{
		newFetcher:    e.newFetcher,
		querier:       e.querier,
		workingDir:    e.workingDir,
		maxIterations: e.maxIterations,
		truncateMax:   e.truncateMax,
		maxDepth:      e.maxDepth,
		currentDepth:  e.currentDepth + 1,
		onProgress:    e.onProgress,
	}
}

type promptData struct {
	PromptLength int
	LineCount    int
	TruncateMax  int
	HasSubLM     bool
	HasRLMQuery  bool
	CurrentDepth int
	MaxDepth     int
}

func (e *Engine) Run(ctx context.Context, prompt string) (string, error) {
	var rlmQueryFn RLMQueryFunc
	if e.currentDepth < e.maxDepth {
		child := e.childEngine()
		rlmQueryFn = func(ctx context.Context, childPrompt string) (string, error) {
			return child.Run(ctx, childPrompt)
		}
	}

	repl, err := NewREPL(e.querier, e.workingDir, rlmQueryFn)
	if err != nil {
		return "", fmt.Errorf("create repl: %w", err)
	}
	defer repl.Close()

	if err := repl.SetVar("prompt", prompt); err != nil {
		return "", fmt.Errorf("set prompt var: %w", err)
	}

	systemPrompt, err := e.buildSystemPrompt(prompt)
	if err != nil {
		return "", fmt.Errorf("build system prompt: %w", err)
	}

	fetcher, err := e.newFetcher(systemPrompt)
	if err != nil {
		return "", fmt.Errorf("create fetcher: %w", err)
	}

	rlmStart := time.Now()
	d := e.currentDepth
	slog.Info("[RLM] started", "prompt_len", len(prompt), "max_iterations", e.maxIterations, "depth", d, "max_depth", e.maxDepth)

	userContent := "Process the input loaded in the `prompt` variable and provide your answer."

	for i := 0; i < e.maxIterations; i++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		e.emitProgress(ProgressEvent{Iteration: i + 1, Phase: "llm_call", Depth: d})
		resp, err := fetcher.Send(ctx, userContent)
		if err != nil {
			return "", fmt.Errorf("fetcher error on turn %d: %w", i+1, err)
		}

		slog.Debug("[RLM] iteration", "depth", d, "iter", i+1, "model", resp.Model, "input_tokens", resp.InputTokens, "output_tokens", resp.OutputTokens)
		slog.Debug("[RLM] assistant text", "depth", d, "iter", i+1, "text", resp.Text)
		e.emitProgress(ProgressEvent{Iteration: i + 1, Phase: "llm_call", Depth: d, TokensIn: resp.InputTokens, TokensOut: resp.OutputTokens})

		if answer, ok := detectFinalInText(resp.Text); ok {
			slog.Info("[RLM] completed", "depth", d, "iter", i+1, "duration", time.Since(rlmStart).Round(time.Millisecond), "answer_len", len(answer), "reason", "final_in_text")
			return answer, nil
		}

		codeBlocks := extractCodeBlocks(resp.Text)
		if len(codeBlocks) == 0 {
			codeBlocks = extractCodeBlocksFallback(resp.Text)
		}

		if len(codeBlocks) == 0 {
			slog.Debug("[RLM] nudge", "depth", d, "iter", i+1, "reason", "no_code_blocks")
			userContent = "[No code block detected. Write ```repl code to process the prompt, or use FINAL(answer) to return your answer.]"
			continue
		}

		var allOutput strings.Builder
		for j, code := range codeBlocks {
			slog.Debug("[RLM] repl execute", "depth", d, "iter", i+1, "block", j+1, "code", code)
			e.emitProgress(ProgressEvent{Iteration: i + 1, Phase: "repl_exec", Depth: d, CodeBlock: code})
			stdout, done, final, err := repl.Execute(ctx, code)
			if err != nil {
				return "", fmt.Errorf("repl execute: %w", err)
			}
			slog.Debug("[RLM] repl result", "depth", d, "iter", i+1, "block", j+1, "stdout_len", len(stdout), "done", done)
			e.emitProgress(ProgressEvent{Iteration: i + 1, Phase: "repl_result", Depth: d, Output: stdout})
			slog.Log(ctx, levelTrace, "[RLM] repl stdout", "depth", d, "iter", i+1, "block", j+1, "stdout", stdout)
			allOutput.WriteString(stdout)
			if done {
				slog.Info("[RLM] completed", "depth", d, "iter", i+1, "duration", time.Since(rlmStart).Round(time.Millisecond), "answer_len", len(final), "reason", "final_in_repl")
				return final, nil
			}
		}

		slog.Debug("[RLM] repl output", "depth", d, "iter", i+1, "code_blocks", len(codeBlocks), "output_len", allOutput.Len())

		userContent = "REPL output:\n" + Truncate(allOutput.String(), e.truncateMax)
	}

	slog.Error("[RLM] failed", "depth", e.currentDepth, "reason", "max_iterations", "max", e.maxIterations, "duration", time.Since(rlmStart).Round(time.Millisecond))
	return "", fmt.Errorf("exceeded max iterations (%d)", e.maxIterations)
}

func (e *Engine) buildSystemPrompt(prompt string) (string, error) {
	data := promptData{
		PromptLength: len(prompt),
		LineCount:    strings.Count(prompt, "\n") + 1,
		TruncateMax:  e.truncateMax,
		HasSubLM:     e.maxDepth >= 1 || e.currentDepth < e.maxDepth,
		HasRLMQuery:  e.currentDepth+1 < e.maxDepth,
		CurrentDepth: e.currentDepth,
		MaxDepth:     e.maxDepth,
	}
	var buf bytes.Buffer
	if err := systemTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func extractCodeBlocks(response string) []string {
	matches := codeBlockRe.FindAllStringSubmatch(response, -1)
	blocks := make([]string, 0, len(matches))
	for _, m := range matches {
		blocks = append(blocks, m[1])
	}
	return blocks
}

func extractCodeBlocksFallback(response string) []string {
	matches := codeBlockFallbackRe.FindAllStringSubmatch(response, -1)
	blocks := make([]string, 0, len(matches))
	for _, m := range matches {
		blocks = append(blocks, m[1])
	}
	return blocks
}

func detectFinalInText(response string) (string, bool) {
	stripped := codeBlockRe.ReplaceAllString(response, "")

	if m := finalVarRe.FindStringSubmatch(stripped); m != nil {
		return m[1], true
	}
	if m := finalQuotRe.FindStringSubmatch(stripped); m != nil {
		return m[1], true
	}
	if m := finalRe.FindStringSubmatch(stripped); m != nil {
		return strings.TrimSpace(m[1]), true
	}
	return "", false
}
