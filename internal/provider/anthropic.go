package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"tenzing-agent/internal/provider/utils"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
)

// anthropicNonStreamingCap is the largest max_tokens the SDK accepts on a
// non-streaming request: its guard rejects requests expected to take longer
// than 10 minutes, scaled at 128000 tokens per hour.
const anthropicNonStreamingCap int64 = 128000 / 6

// maxNonStreamingTokens returns the largest max_tokens the SDK permits for a
// non-streaming request to the given model, honoring the SDK's per-model
// limits on top of the 10-minute guard.
func maxNonStreamingTokens(model string) int64 {
	limit := anthropicNonStreamingCap
	if modelLimit, ok := constant.ModelNonStreamingTokens[model]; ok {
		limit = min(limit, int64(modelLimit))
	}
	return limit
}

type AnthropicModel string

const (
	AnthropicModelClaudeOpus4_6   = AnthropicModel(anthropic.ModelClaudeOpus4_6)
	AnthropicModelClaudeSonnet4_6 = AnthropicModel(anthropic.ModelClaudeSonnet4_6)
	AnthropicModelClaudeHaiku4_5  = AnthropicModel(anthropic.ModelClaudeHaiku4_5)

	MaxTokensClaudeOpus4_6   int64 = 128000
	MaxTokensClaudeSonnet4_6 int64 = 64000

	ContextWindowClaudeOpus4_6   = 200_000
	ContextWindowClaudeSonnet4_6 = 200_000
	ContextWindowClaudeHaiku4_5  = 200_000
	contextWindowAnthropicDefault = 200_000
)

var anthropicContextWindows = map[AnthropicModel]int{
	AnthropicModelClaudeOpus4_6:   ContextWindowClaudeOpus4_6,
	AnthropicModelClaudeSonnet4_6: ContextWindowClaudeSonnet4_6,
	AnthropicModelClaudeHaiku4_5:  ContextWindowClaudeHaiku4_5,
}

type Anthropic struct {
	client      *anthropic.Client
	rateLimiter *utils.TokenBucket
	model       AnthropicModel
}

type AnthropicConfig struct {
	APIKey string
	// BaseURL overrides the API endpoint when set. Used for testing.
	BaseURL string
	Model   AnthropicModel
}

type anthropicOptions struct {
	rateLimiter *utils.TokenBucket
	haveLimiter bool
}

type AnthropicOption func(*anthropicOptions)

func WithAnthropicNoRateLimit() AnthropicOption {
	return func(o *anthropicOptions) {
		o.rateLimiter = nil
		o.haveLimiter = true
	}
}

func NewAnthropicClient(cfg AnthropicConfig, opts ...AnthropicOption) *Anthropic {
	clientOpts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(cfg.BaseURL))
	}
	client := anthropic.NewClient(clientOpts...)
	model := cfg.Model
	if model == "" {
		model = AnthropicModelClaudeSonnet4_6
	}

	var o anthropicOptions
	for _, opt := range opts {
		opt(&o)
	}
	if !o.haveLimiter {
		o.rateLimiter = utils.NewTokenBucket(utils.TokenBucketConfig{
			Rate:           10_000.0 / 60.0, // 10K input tokens per minute
			BurstSize:      10_000,          // 10K possible to pull in one request
			MaxConcurrency: 10,              // max 10 calls concurrent
		})
	}

	return &Anthropic{
		client:      &client,
		rateLimiter: o.rateLimiter,
		model:       model,
	}
}

func (a *Anthropic) enforceRateLimit(ctx context.Context, req CompletionRequest) error {
	if a.rateLimiter == nil {
		return nil
	}

	// set up rate limiting based on input token count
	tokenCount, err := a.CountTokens(ctx, req)
	if err != nil {
		return fmt.Errorf("anthropic count tokens: %w", err)
	}
	if err = a.rateLimiter.Acquire(ctx, tokenCount.InputTokens); err != nil {
		return fmt.Errorf("rate limiter acquire failed: %w", err)
	}
	return nil
}

func (a *Anthropic) releaseRateLimit() {
	if a.rateLimiter != nil {
		a.rateLimiter.Release()
	}
}

func (a *Anthropic) SendSyncMessage(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	params, err := a.buildParams(req)
	if err != nil {
		return CompletionResponse{}, err
	}
	params.MaxTokens = min(params.MaxTokens, maxNonStreamingTokens(req.Model))

	// enforce rate limit before sending the message
	if err := a.enforceRateLimit(ctx, req); err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic rate limit enforcement failed: %w", err)
	}
	defer a.releaseRateLimit()

	// send the message
	res, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic send message: %w", err)
	}

	return fromAnthropicResponse(res), nil
}

func (a *Anthropic) SendStreamingMessage(ctx context.Context, req CompletionRequest, events chan<- StreamEvent) error {
	defer close(events)

	params, err := a.buildParams(req)
	if err != nil {
		return err
	}

	// enforce rate limit before sending the message
	if err := a.enforceRateLimit(ctx, req); err != nil {
		return fmt.Errorf("anthropic rate limit enforcement failed: %w", err)
	}
	defer a.releaseRateLimit()

	stream := a.client.Messages.NewStreaming(ctx, params)

	var accumulated CompletionResponse
	// Content blocks under construction, keyed by stream index. Tool input
	// JSON arrives as partial fragments that must be buffered until
	// content_block_stop.
	blocks := map[int64]*ContentBlock{}
	jsonParts := map[int64][]string{}

	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "message_start":
			accumulated.ID = event.Message.ID
			accumulated.Model = event.Message.Model
			accumulated.Usage.InputTokens = event.Message.Usage.InputTokens
			events <- StreamEvent{Type: StreamEventStart}

		case "content_block_start":
			switch event.ContentBlock.Type {
			case "text":
				blocks[event.Index] = &ContentBlock{Type: ContentTypeText}
			case "tool_use":
				blocks[event.Index] = &ContentBlock{
					Type:      ContentTypeToolUse,
					ToolUseID: event.ContentBlock.ID,
					ToolName:  event.ContentBlock.Name,
				}
			}

		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				if block := blocks[event.Index]; block != nil {
					block.Text += event.Delta.Text
				}
				events <- StreamEvent{
					Type: StreamEventDelta,
					Text: event.Delta.Text,
				}
			case "input_json_delta":
				jsonParts[event.Index] = append(jsonParts[event.Index], event.Delta.PartialJSON)
			}

		case "content_block_stop":
			block := blocks[event.Index]
			if block == nil {
				continue
			}
			if block.Type == ContentTypeToolUse {
				input := strings.Join(jsonParts[event.Index], "")
				if input == "" {
					input = "{}"
				}
				block.ToolInput = json.RawMessage(input)
			}
			accumulated.Content = append(accumulated.Content, *block)

		case "message_delta":
			accumulated.StopReason = fromAnthropicStopReason(event.Delta.StopReason)
			accumulated.Usage.OutputTokens = event.Usage.OutputTokens
		}
	}

	if err := stream.Err(); err != nil {
		events <- StreamEvent{Type: StreamEventError, Err: err}
		return fmt.Errorf("anthropic streaming: %w", err)
	}

	// Emitted after the loop rather than on message_stop so consumers always
	// get a stop event when the stream ends cleanly.
	events <- StreamEvent{
		Type:     StreamEventStop,
		Response: &accumulated,
	}

	return nil
}

// SendMessageWithTools sends a completion request with the given tools,
// overriding any tools already set on the request.
func (a *Anthropic) SendMessageWithTools(ctx context.Context, req CompletionRequest, tools []ToolDefinition) (CompletionResponse, error) {
	req.Tools = tools
	params, err := a.buildParams(req)
	if err != nil {
		return CompletionResponse{}, err
	}
	params.MaxTokens = min(params.MaxTokens, maxNonStreamingTokens(req.Model))

	// enforce rate limit before sending the message
	if err := a.enforceRateLimit(ctx, req); err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic rate limit enforcement failed: %w", err)
	}
	defer a.releaseRateLimit()

	res, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("anthropic send message with tools: %w", err)
	}

	return fromAnthropicResponse(res), nil
}

func (a *Anthropic) GetCurrentModel() string {
	return string(a.model)
}

func (a *Anthropic) GetContextWindowSize() int {
	if w, ok := anthropicContextWindows[a.model]; ok {
		return w
	}
	return contextWindowAnthropicDefault
}

func (a *Anthropic) CountTokens(ctx context.Context, req CompletionRequest) (TokenCount, error) {
	params := anthropic.MessageCountTokensParams{
		Model:    anthropic.Model(req.Model),
		Messages: toAnthropicMessages(req.Messages),
	}

	if req.System != "" {
		params.System = anthropic.MessageCountTokensParamsSystemUnion{
			OfTextBlockArray: []anthropic.TextBlockParam{
				{Text: req.System},
			},
		}
	}

	res, err := a.client.Messages.CountTokens(ctx, params)
	if err != nil {
		return TokenCount{}, fmt.Errorf("anthropic count tokens: %w", err)
	}

	return TokenCount{InputTokens: res.InputTokens}, nil
}

func (a *Anthropic) ListModels(ctx context.Context) ([]ModelInfo, error) {
	page, err := a.client.Models.List(ctx, anthropic.ModelListParams{})
	if err != nil {
		return nil, fmt.Errorf("anthropic list models: %w", err)
	}

	models := make([]ModelInfo, 0, len(page.Data))
	for _, m := range page.Data {
		models = append(models, ModelInfo{
			ID:        m.ID,
			Name:      m.DisplayName,
			MaxTokens: m.MaxTokens,
		})
	}

	return models, nil
}

func (a *Anthropic) buildParams(req CompletionRequest) (anthropic.MessageNewParams, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = MaxTokensStdResponse
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: maxTokens,
		Messages:  toAnthropicMessages(req.Messages),
	}

	if req.System != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: req.System},
		}
	}

	if req.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Temperature)
	}

	if len(req.Tools) > 0 {
		tools, err := toAnthropicTools(req.Tools)
		if err != nil {
			return anthropic.MessageNewParams{}, err
		}
		params.Tools = tools
	}

	return params, nil
}

func toAnthropicMessages(msgs []Message) []anthropic.MessageParam {
	result := make([]anthropic.MessageParam, 0, len(msgs))
	for _, msg := range msgs {
		blocks := toAnthropicContentBlocks(msg.Content)
		switch msg.Role {
		case RoleUser:
			result = append(result, anthropic.NewUserMessage(blocks...))
		case RoleAssistant:
			result = append(result, anthropic.NewAssistantMessage(blocks...))
		case RoleTool:
			// Anthropic has no tool role; tool_result blocks ride in a
			// user message.
			result = append(result, anthropic.NewUserMessage(blocks...))
		case RoleSystem:
			continue
		}
	}
	return result
}

func toAnthropicContentBlocks(blocks []ContentBlock) []anthropic.ContentBlockParamUnion {
	result := make([]anthropic.ContentBlockParamUnion, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentTypeText:
			result = append(result, anthropic.NewTextBlock(block.Text))
		case ContentTypeToolUse:
			result = append(result, anthropic.NewToolUseBlock(block.ToolUseID, block.ToolInput, block.ToolName))
		case ContentTypeToolResult:
			result = append(result, anthropic.NewToolResultBlock(block.ToolResultID, block.ToolOutput, false))
		}
	}
	return result
}

func toAnthropicTools(tools []ToolDefinition) ([]anthropic.ToolUnionParam, error) {
	result := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		// InputSchema is a full JSON Schema object; Anthropic's ToolInputSchemaParam
		// wants its properties and required fields split out.
		var schema struct {
			Properties json.RawMessage `json:"properties"`
			Required   []string        `json:"required"`
		}
		if tool.InputSchema != nil {
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				return nil, fmt.Errorf("anthropic tool %q: parse input schema: %w", tool.Name, err)
			}
		}

		var props any
		if schema.Properties != nil {
			if err := json.Unmarshal(schema.Properties, &props); err != nil {
				return nil, fmt.Errorf("anthropic tool %q: parse schema properties: %w", tool.Name, err)
			}
		}

		result = append(result, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        tool.Name,
				Description: anthropic.String(tool.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: props,
					Required:   schema.Required,
				},
			},
		})
	}
	return result, nil
}

func fromAnthropicResponse(res *anthropic.Message) CompletionResponse {
	content := make([]ContentBlock, 0, len(res.Content))
	for _, block := range res.Content {
		switch block.Type {
		case "text":
			content = append(content, NewTextContent(block.Text))
		case "tool_use":
			content = append(content, NewToolUseContent(block.ID, block.Name, block.Input))
		}
	}

	return CompletionResponse{
		ID:         res.ID,
		Content:    content,
		StopReason: fromAnthropicStopReason(res.StopReason),
		Usage: Usage{
			InputTokens:  res.Usage.InputTokens,
			OutputTokens: res.Usage.OutputTokens,
		},
		Model: res.Model,
	}
}

func fromAnthropicStopReason(reason anthropic.StopReason) StopReason {
	switch reason {
	case anthropic.StopReasonEndTurn:
		return StopReasonEndTurn
	case anthropic.StopReasonMaxTokens:
		return StopReasonMaxTokens
	case anthropic.StopReasonToolUse:
		return StopReasonToolUse
	default:
		return StopReason(reason)
	}
}
