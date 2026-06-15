package provider

import (
	"bytes"
	"context"
	"encoding/json"

	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"tenzing-agent/internal/errors"
	"tenzing-agent/internal/provider/utils"
)

var thinkBlockRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

const OLLAMA_CLOUD_BASE_URL = "https://ollama.com/"

type OllamaModel string

const MAX_CONCURRENT_REQUESTS = 3

// Ollama model constants
const OllamaModelQwen35_9B = OllamaModel("qwen3.5:9b")
const OllamaModelQwen35_35B = OllamaModel("qwen3.5:35b")
const OllamaModelQwen35_122B = OllamaModel("qwen3.5:122b")
const OllamaModelGemma4_31B = OllamaModel("gemma4:31b")

// Ollama implements the LLM interface using Ollama's native /api/* endpoints.
type Ollama struct {
	baseURL     string
	apiKey      string
	client      *http.Client
	rateLimiter *utils.Semaphore
	model       OllamaModel
	contextSize int64
	log         Logger
}

// OllamaConfig holds configuration for connecting to an Ollama server.
type OllamaConfig struct {
	BaseURL     string
	APIKey      string
	Model       OllamaModel
	ContextSize int64 // Ollama num_ctx: total context window (input+output). 0 uses model default.
	// Logger receives request/response diagnostics. Nil disables them.
	Logger Logger
}

// logf writes a diagnostic line when a Logger is configured.
func (o *Ollama) logf(format string, args ...any) {
	if o.log != nil {
		o.log.Logf(format, args...)
	}
}

// NewOllamaClient creates an Ollama LLM client using the native Ollama API.
func NewOllamaClient(cfg OllamaConfig) *Ollama {
	model := cfg.Model
	if model == "" {
		model = OllamaModelGemma4_31B
	}

	url := cfg.BaseURL
	if url == "" {
		url = OLLAMA_CLOUD_BASE_URL
	}

	return &Ollama{
		baseURL:     strings.TrimSuffix(url, "/"),
		apiKey:      cfg.APIKey,
		client:      http.DefaultClient,
		rateLimiter: utils.NewSemaphore(MAX_CONCURRENT_REQUESTS),
		model:       model,
		contextSize: cfg.ContextSize,
		log:         cfg.Logger,
	}
}

// -- internal request/response types --

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Think    *bool               `json:"think,omitempty"`
	Tools    []ollamaTool        `json:"tools,omitempty"`
	Options  map[string]any      `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Model           string            `json:"model"`
	Message         ollamaChatMessage `json:"message"`
	Done            bool              `json:"done"`
	DoneReason      string            `json:"done_reason"`
	PromptEvalCount int               `json:"prompt_eval_count"`
	EvalCount       int               `json:"eval_count"`
}

type ollamaChatMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ollamaToolCall struct {
	Function ollamaToolCallFunction `json:"function"`
}

type ollamaToolCallFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ollamaModelsResponse struct {
	Models []ollamaModelEntry `json:"models"`
}

type ollamaModelEntry struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

// -- LLM interface implementation --

func (o *Ollama) SendSyncMessage(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	tools, err := toOllamaTools(req.Tools, o.log)
	if err != nil {
		return CompletionResponse{}, err
	}

	chatReq := ollamaChatRequest{
		Model:    string(req.Model),
		Messages: toOllamaMessages(req, o.log),
		Stream:   false,
		Think:    boolPtr(false),
		Tools:    tools,
		Options:  o.ollamaOptions(req),
	}

	if err := o.rateLimiter.Acquire(ctx); err != nil {
		return CompletionResponse{}, errors.Wrap(err, "unable to acquire semaphore")
	}
	defer o.rateLimiter.Release()

	var chatRes ollamaChatResponse
	if err := o.postJSON(ctx, "/api/chat", chatReq, &chatRes); err != nil {
		return CompletionResponse{}, errors.Wrap(err, "ollama send message")
	}

	return fromOllamaResponse(chatRes, o.log), nil
}

func (o *Ollama) SendStreamingMessage(ctx context.Context, req CompletionRequest, events chan<- StreamEvent) error {
	defer close(events)

	tools, err := toOllamaTools(req.Tools, o.log)
	if err != nil {
		return err
	}

	if err := o.rateLimiter.Acquire(ctx); err != nil {
		return errors.Wrap(err, "unable to acquire semaphore")
	}
	defer o.rateLimiter.Release()

	chatReq := ollamaChatRequest{
		Model:    string(req.Model),
		Messages: toOllamaMessages(req, o.log),
		Stream:   true,
		Think:    boolPtr(false),
		Tools:    tools,
		Options:  o.ollamaOptions(req),
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return fmt.Errorf("ollama marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	o.logf("ollama: POST %s/api/chat stream=true model=%s messages=%d tools=%d",
		o.baseURL, chatReq.Model, len(chatReq.Messages), len(chatReq.Tools))

	resp, err := o.client.Do(httpReq)
	if err != nil {
		o.logf("ollama: request error: %v", err)
		return fmt.Errorf("ollama streaming request: %w", err)
	}
	defer resp.Body.Close()

	o.logf("ollama: response status=%d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama streaming: unexpected status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	var started bool
	var accumulatedToolCalls []ollamaToolCall
	decoder := json.NewDecoder(resp.Body)
	for decoder.More() {
		var chunk ollamaChatResponse
		if err := decoder.Decode(&chunk); err != nil {
			events <- StreamEvent{Type: StreamEventError, Err: err}
			return fmt.Errorf("ollama streaming decode: %w", err)
		}

		if !started {
			events <- StreamEvent{Type: StreamEventStart}
			started = true
		}

		if chunk.Message.Content != "" {
			events <- StreamEvent{
				Type: StreamEventDelta,
				Text: chunk.Message.Content,
			}
		}

		if len(chunk.Message.ToolCalls) > 0 {
			o.logf("ollama: chunk has %d tool_calls", len(chunk.Message.ToolCalls))
			accumulatedToolCalls = append(accumulatedToolCalls, chunk.Message.ToolCalls...)
		}

		if chunk.Done {
			// accumulatedToolCalls already includes this chunk's tool calls,
			// so using it never drops calls seen in earlier chunks.
			if len(accumulatedToolCalls) > 0 {
				chunk.Message.ToolCalls = accumulatedToolCalls
			}
			o.logf("ollama: done chunk content=%q tool_calls=%d done_reason=%s eval_count=%d",
				chunk.Message.Content, len(chunk.Message.ToolCalls), chunk.DoneReason, chunk.EvalCount)
			res := fromOllamaResponse(chunk, o.log)
			events <- StreamEvent{
				Type:     StreamEventStop,
				Response: &res,
			}
		}
	}

	return nil
}

// SendMessageWithTools sends a completion request with the given tools,
// overriding any tools already set on the request.
func (o *Ollama) SendMessageWithTools(ctx context.Context, req CompletionRequest, tools []ToolDefinition) (CompletionResponse, error) {
	ollamaTools, err := toOllamaTools(tools, o.log)
	if err != nil {
		return CompletionResponse{}, err
	}

	if err := o.rateLimiter.Acquire(ctx); err != nil {
		return CompletionResponse{}, errors.Wrap(err, "unable to acquire semaphore")
	}
	defer o.rateLimiter.Release()

	chatReq := ollamaChatRequest{
		Model:    string(req.Model),
		Messages: toOllamaMessages(req, o.log),
		Stream:   false,
		Think:    boolPtr(false),
		Tools:    ollamaTools,
		Options:  o.ollamaOptions(req),
	}

	o.logf("ollama: POST %s/api/chat stream=false model=%s messages=%d tools=%d",
		o.baseURL, chatReq.Model, len(chatReq.Messages), len(chatReq.Tools))

	var chatRes ollamaChatResponse
	if err := o.postJSON(ctx, "/api/chat", chatReq, &chatRes); err != nil {
		o.logf("ollama: SendMessageWithTools error: %v", err)
		return CompletionResponse{}, fmt.Errorf("ollama send message with tools: %w", err)
	}

	o.logf("ollama: SendMessageWithTools done_reason=%s tool_calls=%d",
		chatRes.DoneReason, len(chatRes.Message.ToolCalls))

	return fromOllamaResponse(chatRes, o.log), nil
}

func (o *Ollama) GetCurrentModel() string {
	return string(o.model)
}

// CountTokens is not supported by Ollama. Returns ErrNotSupported.
func (o *Ollama) CountTokens(_ context.Context, _ CompletionRequest) (TokenCount, error) {
	return TokenCount{}, fmt.Errorf("ollama count tokens: %w", ErrNotSupported)
}

func (o *Ollama) ListModels(ctx context.Context) ([]ModelInfo, error) {
	var modelsRes ollamaModelsResponse

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("ollama create request: %w", err)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama list models: unexpected status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsRes); err != nil {
		return nil, fmt.Errorf("ollama list models decode: %w", err)
	}

	models := make([]ModelInfo, 0, len(modelsRes.Models))
	for _, m := range modelsRes.Models {
		models = append(models, ModelInfo{
			ID:   m.Model,
			Name: m.Name,
		})
	}

	return models, nil
}

// -- HTTP helper --

func (o *Ollama) postJSON(ctx context.Context, path string, input any, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, readErrorBody(resp.Body))
	}

	return json.NewDecoder(resp.Body).Decode(output)
}

// readErrorBody reads a truncated error response body for inclusion in
// error messages.
func readErrorBody(r io.Reader) string {
	const maxErrorBody = 512
	body, err := io.ReadAll(io.LimitReader(r, maxErrorBody))
	if err != nil || len(body) == 0 {
		return "<no body>"
	}
	return strings.TrimSpace(string(body))
}

func boolPtr(v bool) *bool { return &v }

func (o *Ollama) ollamaOptions(req CompletionRequest) map[string]any {
	opts := map[string]any{}
	if req.MaxTokens > 0 {
		opts["num_predict"] = req.MaxTokens
	}
	if o.contextSize > 0 {
		opts["num_ctx"] = o.contextSize
	}
	if len(opts) == 0 {
		return nil
	}
	return opts
}

// -- converters --

func toOllamaMessages(req CompletionRequest, log Logger) []ollamaChatMessage {
	var msgs []ollamaChatMessage

	if req.System != "" {
		msgs = append(msgs, ollamaChatMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleSystem:
			msgs = append(msgs, ollamaChatMessage{
				Role:    "system",
				Content: combinedText(msg.Content),
			})
		case RoleUser:
			msgs = append(msgs, ollamaChatMessage{
				Role:    "user",
				Content: combinedText(msg.Content),
			})
		case RoleAssistant:
			assistant := ollamaChatMessage{
				Role:    "assistant",
				Content: combinedText(msg.Content),
			}
			for _, block := range msg.Content {
				if block.Type != ContentTypeToolUse {
					continue
				}
				var args map[string]any
				if block.ToolInput != nil {
					if err := json.Unmarshal(block.ToolInput, &args); err != nil {
						logf(log, "ollama: skip malformed tool input for %s: %v", block.ToolName, err)
						continue
					}
				}
				assistant.ToolCalls = append(assistant.ToolCalls, ollamaToolCall{
					Function: ollamaToolCallFunction{
						Name:      block.ToolName,
						Arguments: args,
					},
				})
			}
			msgs = append(msgs, assistant)
		case RoleTool:
			for _, block := range msg.Content {
				if block.Type == ContentTypeToolResult {
					msgs = append(msgs, ollamaChatMessage{
						Role:    "tool",
						Content: block.ToolOutput,
					})
				}
			}
		}
	}

	return msgs
}

func toOllamaTools(tools []ToolDefinition, log Logger) ([]ollamaTool, error) {
	result := make([]ollamaTool, 0, len(tools))
	for _, tool := range tools {
		var params map[string]any
		if tool.InputSchema != nil {
			if err := json.Unmarshal(tool.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("ollama tool %q: parse input schema: %w", tool.Name, err)
			}
		}

		result = append(result, ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		})
	}
	return result, nil
}

// StripThinkBlocks removes <think>...</think> blocks that Qwen 3.5 models
// emit in thinking mode. This is a safety net in case think:false is ignored.
// Exported so callers accumulating streaming text can also strip think blocks.
func StripThinkBlocks(s string) string {
	return strings.TrimSpace(thinkBlockRe.ReplaceAllString(s, ""))
}

func fromOllamaResponse(res ollamaChatResponse, log Logger) CompletionResponse {
	var content []ContentBlock

	text := StripThinkBlocks(res.Message.Content)
	if text != "" {
		content = append(content, NewTextContent(text))
	}

	for i, tc := range res.Message.ToolCalls {
		args, err := json.Marshal(tc.Function.Arguments)
		if err != nil {
			logf(log, "ollama: marshal tool arguments for %s: %v", tc.Function.Name, err)
			args = json.RawMessage("{}")
		}
		// Ollama returns no tool-call IDs, so this ID is synthetic
		// (name+index, unique within one response). It only needs to pair
		// tool results with calls — don't expect provider-native formats
		// like Anthropic's "toolu_..." when debugging result matching.
		id := fmt.Sprintf("call_%s_%d", tc.Function.Name, i)
		content = append(content, NewToolUseContent(id, tc.Function.Name, args))
	}

	return CompletionResponse{
		Content:    content,
		StopReason: fromOllamaStopReason(res),
		Usage: Usage{
			InputTokens:  int64(res.PromptEvalCount),
			OutputTokens: int64(res.EvalCount),
		},
		Model: res.Model,
	}
}

func fromOllamaStopReason(res ollamaChatResponse) StopReason {
	if len(res.Message.ToolCalls) > 0 {
		return StopReasonToolUse
	}
	switch res.DoneReason {
	case "stop":
		return StopReasonStop
	case "length":
		return StopReasonMaxTokens
	default:
		return StopReason(res.DoneReason)
	}
}
