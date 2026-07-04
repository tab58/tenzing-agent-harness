package rlm

import (
	"context"

	"github.com/tab58/llm-providers/common"
)

type Querier interface {
	Query(ctx context.Context, prompt string, maxTokens int64) (string, error)
}

type llmQuerier struct {
	llm common.LLM
}

func NewLLMQuerier(llm common.LLM) Querier {
	return &llmQuerier{llm: llm}
}

func (q *llmQuerier) Query(ctx context.Context, prompt string, maxTokens int64) (string, error) {
	resp, err := q.llm.SendSyncMessage(ctx, common.CompletionRequest{
		Model:     q.llm.GetCurrentModel(),
		System:    "Answer concisely and accurately.",
		Messages:  []common.Message{common.NewUserMessage(prompt)},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	return resp.Text(), nil
}
