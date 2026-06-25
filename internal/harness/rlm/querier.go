package rlm

import (
	"context"

	"tenzing-agent/internal/provider"
)

type Querier interface {
	Query(ctx context.Context, prompt string, maxTokens int64) (string, error)
}

type llmQuerier struct {
	llm provider.LLM
}

func NewLLMQuerier(llm provider.LLM) Querier {
	return &llmQuerier{llm: llm}
}

func (q *llmQuerier) Query(ctx context.Context, prompt string, maxTokens int64) (string, error) {
	resp, err := q.llm.SendSyncMessage(ctx, provider.CompletionRequest{
		Model:     q.llm.GetCurrentModel(),
		System:    "Answer concisely and accurately.",
		Messages:  []provider.Message{provider.NewUserMessage(prompt)},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
}
