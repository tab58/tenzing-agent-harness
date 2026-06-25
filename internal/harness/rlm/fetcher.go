package rlm

import (
	"context"
	"fmt"
	"log/slog"

	agentctx "tenzing-agent/internal/agent/context"
	"tenzing-agent/internal/provider"
)

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
	llm          provider.LLM
	history      *agentctx.Context
	systemPrompt string
}

func NewLLMFetcherFactory(llm provider.LLM) FetcherFactory {
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
	f.history.AppendMessages(ctx, provider.NewUserMessage(content))

	model := f.llm.GetCurrentModel()
	resp, err := f.llm.SendSyncMessage(ctx, provider.CompletionRequest{
		Model:     model,
		System:    f.systemPrompt,
		Messages:  f.history.Messages(),
		MaxTokens: provider.MaxTokensStdResponse,
	})
	if err != nil {
		return Response{}, err
	}

	if _, err := f.history.AppendMessages(ctx, provider.NewAssistantMessage(resp.Text())); err != nil {
		slog.Warn("[RLM] compression failed", "error", err)
	}

	return Response{
		Text:         resp.Text(),
		Model:        model,
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
	}, nil
}
