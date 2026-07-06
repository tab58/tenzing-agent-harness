package rlm

import (
	"context"

	"github.com/tab58/llm-providers/common"
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

type simpleFetcher struct {
	llm          common.LLM
	messages     []common.Message
	systemPrompt string
}

// NewSimpleFetcherFactory creates a fetcher that stores conversation history
// in a plain message slice without context compression. RLM history is bounded
// structurally (truncated REPL output + iteration cap, per the RLM paper), so
// compression is unnecessary — and harmful, since lossy summarization can
// destroy the model's memory of which REPL variables hold what.
// See AGENTS.md "RLM fetchers must not compress".
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
