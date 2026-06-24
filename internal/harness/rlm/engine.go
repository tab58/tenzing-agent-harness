package rlm

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"text/template"
	"time"

	"tenzing-agent/internal/provider"
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

type EngineConfig struct {
	RootLLM       provider.LLM
	SubLLM        provider.LLM
	WorkingDir    string
	MaxIterations int
	TruncateMax   int
}

type Engine struct {
	rootLLM       provider.LLM
	subLLM        provider.LLM
	workingDir    string
	maxIterations int
	truncateMax   int
}

func NewEngine(cfg EngineConfig) (*Engine, error) {
	if cfg.RootLLM == nil {
		return nil, fmt.Errorf("root LLM is required")
	}
	subLLM := cfg.SubLLM
	if subLLM == nil {
		subLLM = cfg.RootLLM
	}
	maxIter := cfg.MaxIterations
	if maxIter <= 0 {
		maxIter = 30
	}
	truncMax := cfg.TruncateMax
	if truncMax <= 0 {
		truncMax = 2000
	}
	return &Engine{
		rootLLM:       cfg.RootLLM,
		subLLM:        subLLM,
		workingDir:    cfg.WorkingDir,
		maxIterations: maxIter,
		truncateMax:   truncMax,
	}, nil
}

type promptData struct {
	PromptLength int
	LineCount    int
	TruncateMax  int
}

func (e *Engine) Run(ctx context.Context, prompt string) (string, error) {
	repl, err := NewREPL(e.subLLM, e.workingDir)
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

	history := []provider.Message{
		provider.NewUserMessage("Process the input loaded in the `prompt` variable and provide your answer."),
	}

	rlmStart := time.Now()
	slog.Info("[RLM] started", "prompt_len", len(prompt), "max_iterations", e.maxIterations)

	for i := 0; i < e.maxIterations; i++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		model := e.rootLLM.GetCurrentModel()

		if slog.Default().Enabled(ctx, levelTrace) {
			slog.Log(ctx, levelTrace, "[RLM] request system prompt", "iter", i+1, "model", model, "system", systemPrompt)
			if raw, err := json.Marshal(history); err == nil {
				slog.Log(ctx, levelTrace, "[RLM] request messages", "iter", i+1, "model", model, "messages_json", string(raw))
			}
		}

		resp, err := e.rootLLM.SendSyncMessage(ctx, provider.CompletionRequest{
			Model:     model,
			System:    systemPrompt,
			Messages:  history,
			MaxTokens: provider.MaxTokensStdResponse,
		})
		if err != nil {
			return "", fmt.Errorf("root LLM error on turn %d: %w", i+1, err)
		}

		slog.Debug("[RLM] iteration", "iter", i+1, "model", model, "input_tokens", resp.Usage.InputTokens, "output_tokens", resp.Usage.OutputTokens)

		response := resp.Text()
		slog.Debug("[RLM] assistant text", "iter", i+1, "text", response)

		if answer, ok := detectFinalInText(response); ok {
			slog.Info("[RLM] completed", "iter", i+1, "duration", time.Since(rlmStart).Round(time.Millisecond), "answer_len", len(answer), "reason", "final_in_text")
			return answer, nil
		}

		codeBlocks := extractCodeBlocks(response)
		if len(codeBlocks) == 0 {
			codeBlocks = extractCodeBlocksFallback(response)
		}

		if len(codeBlocks) == 0 {
			slog.Debug("[RLM] nudge", "iter", i+1, "reason", "no_code_blocks")
			nudge := "[No code block detected. Write ```repl code to process the prompt, or use FINAL(answer) to return your answer.]"
			history = append(history,
				provider.NewAssistantMessage(response),
				provider.NewUserMessage(nudge),
			)
			continue
		}

		var allOutput strings.Builder
		for j, code := range codeBlocks {
			slog.Debug("[RLM] repl execute", "iter", i+1, "block", j+1, "code", code)
			stdout, done, final, err := repl.Execute(ctx, code)
			if err != nil {
				return "", fmt.Errorf("repl execute: %w", err)
			}
			slog.Debug("[RLM] repl result", "iter", i+1, "block", j+1, "stdout_len", len(stdout), "done", done)
			slog.Log(ctx, levelTrace, "[RLM] repl stdout", "iter", i+1, "block", j+1, "stdout", stdout)
			allOutput.WriteString(stdout)
			if done {
				slog.Info("[RLM] completed", "iter", i+1, "duration", time.Since(rlmStart).Round(time.Millisecond), "answer_len", len(final), "reason", "final_in_repl")
				return final, nil
			}
		}

		slog.Debug("[RLM] repl output", "iter", i+1, "code_blocks", len(codeBlocks), "output_len", allOutput.Len())

		feedback := Truncate(allOutput.String(), e.truncateMax)
		history = append(history,
			provider.NewAssistantMessage(response),
			provider.NewUserMessage("REPL output:\n"+feedback),
		)
	}

	slog.Error("[RLM] failed", "reason", "max_iterations", "max", e.maxIterations, "duration", time.Since(rlmStart).Round(time.Millisecond))
	return "", fmt.Errorf("exceeded max iterations (%d)", e.maxIterations)
}

func (e *Engine) buildSystemPrompt(prompt string) (string, error) {
	data := promptData{
		PromptLength: len(prompt),
		LineCount:    strings.Count(prompt, "\n") + 1,
		TruncateMax:  e.truncateMax,
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
