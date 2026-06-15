package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"tenzing-agent/internal/provider/utils"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

const (
	compatMaxRetries    = 5
	compatBaseBackoff   = 2 * time.Second
	compatMaxBackoff    = 60 * time.Second
	compatBackoffJitter = 0.5
)

// openAICompat implements the LLM interface for any OpenAI-compatible chat
// completions API. Provider types (OpenAI, Cerebras, Lightning, OpenRouter)
// embed it and differ only in configuration.
type openAICompat struct {
	// name identifies the provider in error messages and logs.
	name        string
	client      *openai.Client
	model       string
	rateLimiter *utils.TokenBucket
	// tokenCostLimit acquires estimated input tokens from the rate limiter
	// instead of one unit per request.
	tokenCostLimit bool
	// retryRateLimit retries requests that fail with HTTP 429 using
	// exponential backoff.
	retryRateLimit bool
	// useMaxCompletionTokens sends max_completion_tokens instead of the
	// deprecated max_tokens, required by newer OpenAI models.
	useMaxCompletionTokens bool
	// baseBackoff and maxBackoff override retry backoff timing; zero values
	// fall back to compatBaseBackoff/compatMaxBackoff. Test seam.
	baseBackoff time.Duration
	maxBackoff  time.Duration
}

// backoff sleeps before retry attempt+1 using the client's backoff bounds.
func (c *openAICompat) backoff(ctx context.Context, attempt int) error {
	base, maxB := c.baseBackoff, c.maxBackoff
	if base == 0 {
		base = compatBaseBackoff
	}
	if maxB == 0 {
		maxB = compatMaxBackoff
	}
	return rateLimitBackoff(ctx, c.name, attempt, base, maxB)
}

func (c *openAICompat) enforceRateLimit(ctx context.Context, req CompletionRequest) error {
	if c.rateLimiter == nil {
		return nil
	}

	cost := int64(1)
	if c.tokenCostLimit {
		tokenCount, err := c.CountTokens(ctx, req)
		if err != nil {
			return fmt.Errorf("%s count tokens: %w", c.name, err)
		}
		cost = tokenCount.InputTokens
	}
	if err := c.rateLimiter.Acquire(ctx, cost); err != nil {
		return fmt.Errorf("%s rate limiter acquire: %w", c.name, err)
	}
	return nil
}

func (c *openAICompat) releaseRateLimit() {
	if c.rateLimiter != nil {
		c.rateLimiter.Release()
	}
}

func (c *openAICompat) maxAttempts() int {
	if c.retryRateLimit {
		return compatMaxRetries
	}
	return 1
}

func (c *openAICompat) SendSyncMessage(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	return c.send(ctx, req)
}

// SendMessageWithTools sends a completion request with the given tools,
// overriding any tools already set on the request.
func (c *openAICompat) SendMessageWithTools(ctx context.Context, req CompletionRequest, tools []ToolDefinition) (CompletionResponse, error) {
	req.Tools = tools
	return c.send(ctx, req)
}

func (c *openAICompat) send(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	params, err := c.buildParams(req)
	if err != nil {
		return CompletionResponse{}, err
	}

	if err := c.enforceRateLimit(ctx, req); err != nil {
		return CompletionResponse{}, err
	}
	defer c.releaseRateLimit()

	return retryOnRateLimit(ctx, c.name, c.maxAttempts(), c.backoff, func() (CompletionResponse, error) {
		res, err := c.client.Chat.Completions.New(ctx, params)
		if err != nil {
			return CompletionResponse{}, fmt.Errorf("%s send message: %w", c.name, err)
		}
		return fromOpenAIResponse(res), nil
	})
}

// SendStreamingMessage streams a completion. The events channel is always
// closed before returning, including on error. Rate-limited attempts are
// retried only if no events have been emitted yet, so consumers never see
// duplicated deltas.
func (c *openAICompat) SendStreamingMessage(ctx context.Context, req CompletionRequest, events chan<- StreamEvent) error {
	defer close(events)

	params, err := c.buildParams(req)
	if err != nil {
		return err
	}
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: param.NewOpt(true),
	}

	if err := c.enforceRateLimit(ctx, req); err != nil {
		return err
	}
	defer c.releaseRateLimit()

	attempts := c.maxAttempts()
	for attempt := range attempts {
		emitted, err := c.streamOnce(ctx, params, events)
		if err == nil {
			return nil
		}
		if !emitted && isRateLimitError(err) && attempt < attempts-1 {
			if backoffErr := c.backoff(ctx, attempt); backoffErr != nil {
				return backoffErr
			}
			continue
		}
		events <- StreamEvent{Type: StreamEventError, Err: err}
		return fmt.Errorf("%s streaming: %w", c.name, err)
	}
	return nil
}

// streamOnce runs a single streaming attempt. It reports whether any events
// were emitted so the caller knows if a retry is safe.
func (c *openAICompat) streamOnce(ctx context.Context, params openai.ChatCompletionNewParams, events chan<- StreamEvent) (bool, error) {
	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	type pendingToolCall struct {
		id   string
		name string
		args strings.Builder
	}

	var accumulated CompletionResponse
	var text strings.Builder
	toolCalls := map[int64]*pendingToolCall{}
	emitted := false

	for stream.Next() {
		chunk := stream.Current()

		if accumulated.ID == "" && chunk.ID != "" {
			accumulated.ID = chunk.ID
			accumulated.Model = chunk.Model
		}
		if !emitted {
			events <- StreamEvent{Type: StreamEventStart}
			emitted = true
		}

		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			if choice.Delta.Content != "" {
				text.WriteString(choice.Delta.Content)
				events <- StreamEvent{
					Type: StreamEventDelta,
					Text: choice.Delta.Content,
				}
			}
			for _, tc := range choice.Delta.ToolCalls {
				call := toolCalls[tc.Index]
				if call == nil {
					call = &pendingToolCall{}
					toolCalls[tc.Index] = call
				}
				if tc.ID != "" {
					call.id = tc.ID
				}
				if tc.Function.Name != "" {
					call.name = tc.Function.Name
				}
				call.args.WriteString(tc.Function.Arguments)
			}
			if choice.FinishReason != "" {
				accumulated.StopReason = fromOpenAIFinishReason(choice.FinishReason)
			}
		}

		if chunk.Usage.TotalTokens > 0 {
			accumulated.Usage = Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
		}
	}

	if err := stream.Err(); err != nil {
		return emitted, err
	}

	if text.Len() > 0 {
		accumulated.Content = append(accumulated.Content, NewTextContent(text.String()))
	}

	indexes := make([]int64, 0, len(toolCalls))
	for idx := range toolCalls {
		indexes = append(indexes, idx)
	}
	slices.Sort(indexes)
	for _, idx := range indexes {
		call := toolCalls[idx]
		args := call.args.String()
		if args == "" {
			args = "{}"
		}
		accumulated.Content = append(accumulated.Content, NewToolUseContent(call.id, call.name, json.RawMessage(args)))
	}

	events <- StreamEvent{
		Type:     StreamEventStop,
		Response: &accumulated,
	}
	return emitted, nil
}

func (c *openAICompat) GetCurrentModel() string {
	return c.model
}

// CountTokens estimates input tokens using the ~4 chars per token rule of
// thumb; OpenAI-compatible APIs have no token counting endpoint.
func (c *openAICompat) CountTokens(_ context.Context, req CompletionRequest) (TokenCount, error) {
	var totalChars int
	for _, msg := range req.Messages {
		for _, block := range msg.Content {
			totalChars += len(block.Text)
		}
	}
	totalChars += len(req.System)

	return TokenCount{InputTokens: int64(totalChars / 4)}, nil
}

func (c *openAICompat) ListModels(ctx context.Context) ([]ModelInfo, error) {
	page, err := c.client.Models.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s list models: %w", c.name, err)
	}

	models := make([]ModelInfo, 0, len(page.Data))
	for _, m := range page.Data {
		models = append(models, ModelInfo{
			ID:   m.ID,
			Name: m.ID,
		})
	}

	return models, nil
}

func (c *openAICompat) buildParams(req CompletionRequest) (openai.ChatCompletionNewParams, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = MaxTokensStdResponse
	}

	msgs := toOpenAIMessages(req.Messages)
	if req.System != "" {
		msgs = append([]openai.ChatCompletionMessageParamUnion{openai.SystemMessage(req.System)}, msgs...)
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(req.Model),
		Messages: msgs,
	}

	if c.useMaxCompletionTokens {
		params.MaxCompletionTokens = param.NewOpt(maxTokens)
	} else {
		params.MaxTokens = param.NewOpt(maxTokens)
	}

	if req.Temperature != nil {
		params.Temperature = param.NewOpt(*req.Temperature)
	}

	if len(req.Tools) > 0 {
		tools, err := toOpenAITools(req.Tools)
		if err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("%s: %w", c.name, err)
		}
		params.Tools = tools
	}

	return params, nil
}
