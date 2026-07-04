package rlm

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/tab58/llm-providers/common"
	agentctx "tenzing-agent/internal/agent/context"
)

// maxTokensStdResponse caps output tokens per LLM request.
const maxTokensStdResponse int64 = 32768

type Response struct {
	Text         string
	Model        string
	InputTokens  int64
	OutputTokens int64
}

type Fetcher interface {
	Send(ctx context.Context, content string) (Response, error)
}

type FetcherFactory func(systemPrompt string) (Fetcher, error)

type llmFetcher struct {
	llm          common.LLM
	history      *agentctx.Context
	systemPrompt string
}

func NewLLMFetcherFactory(llm common.LLM) FetcherFactory {
	return func(systemPrompt string) (Fetcher, error) {
		history, err := agentctx.NewContext(agentctx.ContextConfig{LLM: llm})
		if err != nil {
			return nil, fmt.Errorf("create context: %w", err)
		}
		return &llmFetcher{
			llm:          llm,
			history:      history,
			systemPrompt: systemPrompt,
		}, nil
	}
}

func (f *llmFetcher) Send(ctx context.Context, content string) (Response, error) {
	f.history.AppendMessages(ctx, common.NewUserMessage(content))

	model := f.llm.GetCurrentModel()
	resp, err := f.llm.SendSyncMessage(ctx, common.CompletionRequest{
		Model:     model,
		System:    f.systemPrompt,
		Messages:  f.history.Messages(),
		MaxTokens: maxTokensStdResponse,
	})
	if err != nil {
		return Response{}, err
	}

	if _, err := f.history.AppendMessages(ctx, common.NewAssistantMessage(resp.Text())); err != nil {
		slog.Warn("[RLM] compression failed", "error", err)
	}

	return Response{
		Text:         resp.Text(),
		Model:        model,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}, nil
}

type simpleFetcher struct {
	llm          common.LLM
	messages     []common.Message
	systemPrompt string
}

// NewSimpleFetcherFactory creates a fetcher that stores conversation history
// in a plain message slice without context compression. Use this when the RLM's
// REPL-based context bounding is sufficient and compression would interfere
// with the intended algorithm behavior.
func NewSimpleFetcherFactory(llm common.LLM) FetcherFactory {
	return func(systemPrompt string) (Fetcher, error) {
		return &simpleFetcher{
			llm:          llm,
			systemPrompt: systemPrompt,
		}, nil
	}
}

func (f *simpleFetcher) Send(ctx context.Context, content string) (Response, error) {
	f.messages = append(f.messages, common.NewUserMessage(content))

	model := f.llm.GetCurrentModel()
	resp, err := f.llm.SendSyncMessage(ctx, common.CompletionRequest{
		Model:     model,
		System:    f.systemPrompt,
		Messages:  f.messages,
		MaxTokens: maxTokensStdResponse,
	})
	if err != nil {
		return Response{}, err
	}

	f.messages = append(f.messages, common.NewAssistantMessage(resp.Text()))

	return Response{
		Text:         resp.Text(),
		Model:        model,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}, nil
}
