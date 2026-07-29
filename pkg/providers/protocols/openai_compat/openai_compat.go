package openai_compat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/tab58/tenzing-agent-harness/pkg/common"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
)

const (
	compatMaxRetries    = 5
	compatBaseBackoff   = 2 * time.Second
	compatMaxBackoff    = 60 * time.Second
	compatBackoffJitter = 0.5
)

type Model = common.Model

type ClientOption func(*clientOptions)

type clientOptions struct {
	name                   string
	model                  Model
	apiKey                 string
	baseURL                string
	retryRateLimit         bool
	useMaxCompletionTokens bool
	baseBackoff            time.Duration
	maxBackoff             time.Duration
}

func loadClientOptions(model Model, opts []ClientOption) *clientOptions {
	o := &clientOptions{
		model:       model,
		baseBackoff: compatBaseBackoff,
		maxBackoff:  compatMaxBackoff,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithName sets the provider name used in error messages and logs.
func WithName(name string) ClientOption {
	return func(o *clientOptions) {
		o.name = name
	}
}

// WithAPIKey sets the API key sent to the provider.
func WithAPIKey(key string) ClientOption {
	return func(o *clientOptions) {
		o.apiKey = key
	}
}

// WithBaseURL points the client at the provider's OpenAI-compatible endpoint.
func WithBaseURL(baseURL string) ClientOption {
	return func(o *clientOptions) {
		o.baseURL = baseURL
	}
}

// WithRetryRateLimit retries requests that fail with HTTP 429 using
// exponential backoff.
func WithRetryRateLimit() ClientOption {
	return func(o *clientOptions) {
		o.retryRateLimit = true
	}
}

// WithMaxCompletionTokens sends max_completion_tokens instead of the
// deprecated max_tokens, required by newer OpenAI models.
func WithMaxCompletionTokens() ClientOption {
	return func(o *clientOptions) {
		o.useMaxCompletionTokens = true
	}
}

// WithBackoff overrides retry backoff timing. Test seam.
func WithBackoff(base, maxB time.Duration) ClientOption {
	return func(o *clientOptions) {
		o.baseBackoff = base
		o.maxBackoff = maxB
	}
}

type Client struct {
	// Name identifies the provider in error messages and logs.
	Name   string
	Client *openai.Client
	// Model is required; it supplies the default max tokens and the
	// context window size.
	Model Model
	// RetryRateLimit retries requests that fail with HTTP 429 using
	// exponential backoff.
	RetryRateLimit bool
	// UseMaxCompletionTokens sends max_completion_tokens instead of the
	// deprecated max_tokens, required by newer OpenAI models.
	UseMaxCompletionTokens bool
	// BaseBackoff and maxBackoff override retry backoff timing; zero values
	// fall back to compatBaseBackoff/compatMaxBackoff. Test seam.
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
}

// NewClient builds an OpenAI-compatible client. Model is required; there is
// no default.
func NewClient(model Model, options ...ClientOption) (*Client, error) {
	opts := loadClientOptions(model, options)
	if opts.model == nil {
		return nil, fmt.Errorf("%s: Model is required", opts.name)
	}

	var reqOpts []option.RequestOption
	if opts.apiKey != "" {
		reqOpts = append(reqOpts, option.WithAPIKey(opts.apiKey))
	}
	if opts.baseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(opts.baseURL))
	}
	sdk := openai.NewClient(reqOpts...)

	return &Client{
		Name:                   opts.name,
		Client:                 &sdk,
		Model:                  opts.model,
		RetryRateLimit:         opts.retryRateLimit,
		UseMaxCompletionTokens: opts.useMaxCompletionTokens,
		BaseBackoff:            opts.baseBackoff,
		MaxBackoff:             opts.maxBackoff,
	}, nil
}

// backoff sleeps before retry attempt+1 using the client's backoff bounds.
func (c *Client) backoff(ctx context.Context, attempt int) error {
	base, maxB := c.BaseBackoff, c.MaxBackoff
	if base == 0 {
		base = compatBaseBackoff
	}
	if maxB == 0 {
		maxB = compatMaxBackoff
	}
	return rateLimitBackoff(ctx, c.Name, attempt, base, maxB)
}

func (c *Client) maxAttempts() int {
	if c.RetryRateLimit {
		return compatMaxRetries
	}
	return 1
}

// apiError wraps an SDK or transport error into *common.APIError so callers
// can classify it with errors.As. The thin provider wrappers set Name to
// their provider string, so it doubles as the APIError Provider.
func (c *Client) apiError(err error) *common.APIError {
	status := 0
	var sdkErr *openai.Error
	if errors.As(err, &sdkErr) {
		status = sdkErr.StatusCode
	}
	return &common.APIError{
		StatusCode: status,
		Provider:   c.Name,
		Message:    err.Error(),
		Err:        err,
	}
}

func (c *Client) SendSyncMessage(ctx context.Context, req common.CompletionRequest) (common.CompletionResponse, error) {
	return c.send(ctx, req)
}

// SendMessageWithTools sends a completion request with the given tools,
// overriding any tools already set on the request.
func (c *Client) SendMessageWithTools(ctx context.Context, req common.CompletionRequest, tools []common.ToolDefinition) (common.CompletionResponse, error) {
	req.Tools = tools
	return c.send(ctx, req)
}

func (c *Client) send(ctx context.Context, req common.CompletionRequest) (common.CompletionResponse, error) {
	params, err := c.buildParams(req)
	if err != nil {
		return common.CompletionResponse{}, err
	}

	return retryOnRateLimit(ctx, c.Name, c.maxAttempts(), c.backoff, func() (common.CompletionResponse, error) {
		res, err := c.Client.Chat.Completions.New(ctx, params)
		if err != nil {
			return common.CompletionResponse{}, fmt.Errorf("%s send message: %w", c.Name, c.apiError(err))
		}
		return fromOpenAIResponse(res), nil
	})
}

// SendStreamingMessage streams a completion. The events channel is always
// closed before returning, including on error. Rate-limited attempts are
// retried only if no events have been emitted yet, so consumers never see
// duplicated deltas.
func (c *Client) SendStreamingMessage(ctx context.Context, req common.CompletionRequest, events chan<- common.StreamEvent) error {
	defer close(events)

	params, err := c.buildParams(req)
	if err != nil {
		return err
	}
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: param.NewOpt(true),
	}

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
		wrapped := c.apiError(err)
		events <- common.StreamEvent{Type: common.StreamEventError, Err: wrapped}
		return fmt.Errorf("%s streaming: %w", c.Name, wrapped)
	}
	return nil
}

// streamOnce runs a single streaming attempt. It reports whether any events
// were emitted so the caller knows if a retry is safe.
func (c *Client) streamOnce(ctx context.Context, params openai.ChatCompletionNewParams, events chan<- common.StreamEvent) (bool, error) {
	stream := c.Client.Chat.Completions.NewStreaming(ctx, params)

	type pendingToolCall struct {
		id   string
		name string
		args strings.Builder
	}

	var accumulated common.CompletionResponse
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
			events <- common.StreamEvent{Type: common.StreamEventStart}
			emitted = true
		}

		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			if choice.Delta.Content != "" {
				text.WriteString(choice.Delta.Content)
				events <- common.StreamEvent{
					Type: common.StreamEventDelta,
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
			accumulated.Usage = common.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
		}
	}

	if err := stream.Err(); err != nil {
		return emitted, err
	}

	if text.Len() > 0 {
		accumulated.Content = append(accumulated.Content, common.NewTextContent(text.String()))
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
		accumulated.Content = append(accumulated.Content, common.NewToolUseContent(call.id, call.name, json.RawMessage(args)))
	}

	events <- common.StreamEvent{
		Type:     common.StreamEventStop,
		Response: &accumulated,
	}
	return emitted, nil
}

func (c *Client) GetCurrentModel() string {
	return c.Model.GetName()
}

func (c *Client) GetContextWindowSize() int {
	return c.Model.GetContextWindowSize()
}

func (c *Client) GetModel() common.Model {
	return c.Model
}

// CountTokens estimates input tokens using the ~4 chars per token rule of
// thumb; OpenAI-compatible APIs have no token counting endpoint.
func (c *Client) CountTokens(_ context.Context, req common.CompletionRequest) (common.TokenCount, error) {
	var totalChars int
	for _, msg := range req.Messages {
		for _, block := range msg.Content {
			totalChars += len(block.Text)
		}
	}
	totalChars += len(req.System)

	return common.TokenCount{InputTokens: int64(totalChars / 4)}, nil
}

func (c *Client) ListModels(ctx context.Context) ([]common.ModelInfo, error) {
	page, err := c.Client.Models.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s list models: %w", c.Name, c.apiError(err))
	}

	models := make([]common.ModelInfo, 0, len(page.Data))
	for _, m := range page.Data {
		models = append(models, common.ModelInfo{
			ID:   m.ID,
			Name: m.ID,
		})
	}

	return models, nil
}

func (c *Client) buildParams(req common.CompletionRequest) (openai.ChatCompletionNewParams, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = int64(c.Model.GetMaxTokens())
	}

	msgs := toOpenAIMessages(req.Messages)
	if req.System != "" {
		msgs = append([]openai.ChatCompletionMessageParamUnion{openai.SystemMessage(req.System)}, msgs...)
	}

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(req.Model),
		Messages: msgs,
	}

	if c.UseMaxCompletionTokens {
		params.MaxCompletionTokens = param.NewOpt(maxTokens)
	} else {
		params.MaxTokens = param.NewOpt(maxTokens)
	}

	if req.Temperature != nil {
		params.Temperature = param.NewOpt(*req.Temperature)
	}

	if req.ThinkingBudget != nil {
		params.ReasoningEffort = reasoningEffortForBudget(*req.ThinkingBudget)
	}

	if len(req.Tools) > 0 {
		tools, err := toOpenAITools(req.Tools)
		if err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("%s: %w", c.Name, err)
		}
		params.Tools = tools
	}

	return params, nil
}
