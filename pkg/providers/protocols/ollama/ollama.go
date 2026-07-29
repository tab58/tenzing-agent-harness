package ollama

import (
	"bytes"
	"context"
	"encoding/json"

	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/tab58/tenzing-agent-harness/pkg/common"
	"github.com/tab58/tenzing-agent-harness/pkg/providers/ratelimit"
)

type Model = common.Model

var thinkBlockRe = regexp.MustCompile(`(?s)<think>(.*?)</think>\s*`)

const OLLAMA_CLOUD_BASE_URL = "https://ollama.com/"

// ollamaLocalBaseURL is the default address of a locally running Ollama server.
const ollamaLocalBaseURL = "http://localhost:11434"

// Client implements the LLM interface using Client's native /api/* endpoints.
type Client struct {
	baseURL     string
	apiKey      string
	client      *http.Client
	model       Model
	contextSize int64
	log         *slog.Logger
}

// logf writes a diagnostic line when a Logger is configured.
func (o *Client) logf(format string, args ...any) {
	logf(o.log, format, args...)
}

// logf writes a diagnostic line when log is non-nil.
func logf(log *slog.Logger, format string, args ...any) {
	if log != nil {
		log.Debug(fmt.Sprintf(format, args...))
	}
}

type ClientOption func(*clientOptions)

type clientOptions struct {
	model  Model
	apiKey string
	// maxConcurrency bounds concurrent requests; 0 means unlimited.
	maxConcurrency int
	baseURL        string
	// Ollama num_ctx: total context window (input+output). 0 uses model default.
	contextSize int64
	// Logger receives request/response diagnostics. Nil disables them.
	logger *slog.Logger
}

func loadClientOptions(model Model, opts []ClientOption) *clientOptions {
	o := &clientOptions{
		model:   model,
		baseURL: OLLAMA_CLOUD_BASE_URL,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithAPIKey sets the API key sent to the Ollama server (Ollama Cloud).
func WithAPIKey(key string) ClientOption {
	return func(o *clientOptions) {
		o.apiKey = key
	}
}

// WithConcurrencyLimit bounds concurrent requests. Values <= 0 remove the
// bound (the default).
func WithConcurrencyLimit(limit int) ClientOption {
	return func(o *clientOptions) {
		o.maxConcurrency = max(limit, 0)
	}
}

func WithContextSize(size int64) ClientOption {
	return func(o *clientOptions) {
		o.contextSize = size
	}
}

func WithLogger(log *slog.Logger) ClientOption {
	return func(o *clientOptions) {
		o.logger = log
	}
}

// WithBaseURL sets the base URL for the Ollama client. This is useful for testing or using a self-hosted Ollama server.
func WithBaseURL(baseURL string) ClientOption {
	return func(o *clientOptions) {
		if baseURL != "" {
			o.baseURL = baseURL
		}
	}
}

// WithLocalURL points the client at a locally running Ollama server instead
// of Ollama Cloud. Takes precedence over WithBaseURL when applied last.
func WithLocalURL() ClientOption {
	return func(o *clientOptions) {
		o.baseURL = ollamaLocalBaseURL
	}
}

// NewClient creates an Ollama LLM client using the native Ollama API. Model
// is required; there is no default.
func NewClient(model Model, options ...ClientOption) (common.LLM, error) {
	opts := loadClientOptions(model, options)
	if opts.model == nil {
		return nil, fmt.Errorf("ollama: Model is required")
	}

	raw := &Client{
		baseURL:     strings.TrimSuffix(opts.baseURL, "/"),
		apiKey:      opts.apiKey,
		client:      http.DefaultClient,
		model:       opts.model,
		contextSize: opts.contextSize,
		log:         opts.logger,
	}
	if opts.maxConcurrency <= 0 {
		return raw, nil
	}

	return ratelimit.Wrap(raw, ratelimit.NewSemaphore(opts.maxConcurrency), ratelimit.CostPerRequest), nil
}

// apiError wraps an HTTP-level failure into *common.APIError so callers can
// classify it with errors.As. statusCode 0 means no HTTP response arrived
// (connection failure, mid-stream drop).
func apiError(statusCode int, message string, err error) *common.APIError {
	return &common.APIError{
		StatusCode: statusCode,
		Provider:   "ollama",
		Message:    message,
		Err:        err,
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
	Role    string `json:"role"`
	Content string `json:"content"`
	// Thinking carries the model's chain-of-thought when the server separates
	// it natively (thinking models on Ollama Cloud). Response-only: requests
	// never set it, so omitempty keeps it off the wire.
	Thinking  string           `json:"thinking,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	// ToolName labels a role-"tool" message with the tool that produced it.
	// Ollama's native API has no tool_call_id; this is its only linkage.
	ToolName string `json:"tool_name,omitempty"`
	// Images carries base64-encoded image bytes for vision models. Ollama's
	// native API takes bare base64 strings — no media type, no data: URI.
	Images []string `json:"images,omitempty"`
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

func (o *Client) SendSyncMessage(ctx context.Context, req common.CompletionRequest) (common.CompletionResponse, error) {
	tools, err := toOllamaTools(req.Tools, o.log)
	if err != nil {
		return common.CompletionResponse{}, err
	}

	chatReq := ollamaChatRequest{
		Model:    string(req.Model),
		Messages: toOllamaMessages(req, o.log),
		Stream:   false,
		Think:    req.Think,
		Tools:    tools,
		Options:  o.ollamaOptions(req),
	}

	var chatRes ollamaChatResponse
	if err := o.postJSON(ctx, "/api/chat", chatReq, &chatRes); err != nil {
		return common.CompletionResponse{}, fmt.Errorf("ollama send message: %w", err)
	}

	return fromOllamaResponse(chatRes, o.log), nil
}

func (o *Client) SendStreamingMessage(ctx context.Context, req common.CompletionRequest, events chan<- common.StreamEvent) error {
	defer close(events)

	tools, err := toOllamaTools(req.Tools, o.log)
	if err != nil {
		return err
	}

	chatReq := ollamaChatRequest{
		Model:    string(req.Model),
		Messages: toOllamaMessages(req, o.log),
		Stream:   true,
		Think:    req.Think,
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
		wrapped := apiError(0, err.Error(), err)
		events <- common.StreamEvent{Type: common.StreamEventError, Err: wrapped}
		return fmt.Errorf("ollama streaming request: %w", wrapped)
	}
	defer resp.Body.Close()

	o.logf("ollama: response status=%d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		wrapped := apiError(resp.StatusCode, readErrorBody(resp.Body), nil)
		events <- common.StreamEvent{Type: common.StreamEventError, Err: wrapped}
		return fmt.Errorf("ollama streaming: %w", wrapped)
	}

	var started bool
	var accumulatedToolCalls []ollamaToolCall
	var accumulatedText strings.Builder
	var accumulatedThinking strings.Builder
	thinkParser := newThinkBlockParser(events)
	decoder := json.NewDecoder(resp.Body)
	for decoder.More() {
		var chunk ollamaChatResponse
		if err := decoder.Decode(&chunk); err != nil {
			wrapped := apiError(0, err.Error(), err)
			events <- common.StreamEvent{Type: common.StreamEventError, Err: wrapped}
			return fmt.Errorf("ollama streaming decode: %w", wrapped)
		}

		if !started {
			events <- common.StreamEvent{Type: common.StreamEventStart}
			started = true
		}

		// Native thinking deltas (server-separated chain-of-thought).
		// Inline think tags in content deltas are handled by thinkParser;
		// note a bare </think> closer with no opener can't be reclassified
		// mid-stream (earlier deltas are already emitted) — the terminal
		// response is still classified correctly via fromOllamaResponse.
		if chunk.Message.Thinking != "" {
			events <- common.StreamEvent{Type: common.StreamEventThinking, Text: chunk.Message.Thinking}
			accumulatedThinking.WriteString(chunk.Message.Thinking)
		}

		if chunk.Message.Content != "" {
			thinkParser.Feed(chunk.Message.Content)
			accumulatedText.WriteString(chunk.Message.Content)
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
			// Streamed text arrives spread across chunks and the done chunk's
			// content is typically empty — the terminal response must carry
			// the full accumulated text and thinking (classified into blocks
			// inside fromOllamaResponse) or callers see an empty final answer.
			chunk.Message.Content = accumulatedText.String()
			chunk.Message.Thinking = accumulatedThinking.String()
			o.logf("ollama: done chunk content=%q tool_calls=%d done_reason=%s eval_count=%d",
				chunk.Message.Content, len(chunk.Message.ToolCalls), chunk.DoneReason, chunk.EvalCount)
			res := fromOllamaResponse(chunk, o.log)
			thinkParser.Flush()
			events <- common.StreamEvent{
				Type:     common.StreamEventStop,
				Response: &res,
			}
		}
	}

	thinkParser.Flush()
	return nil
}

// SendMessageWithTools sends a completion request with the given tools,
// overriding any tools already set on the request.
func (o *Client) SendMessageWithTools(ctx context.Context, req common.CompletionRequest, tools []common.ToolDefinition) (common.CompletionResponse, error) {
	ollamaTools, err := toOllamaTools(tools, o.log)
	if err != nil {
		return common.CompletionResponse{}, err
	}

	chatReq := ollamaChatRequest{
		Model:    string(req.Model),
		Messages: toOllamaMessages(req, o.log),
		Stream:   false,
		Think:    req.Think,
		Tools:    ollamaTools,
		Options:  o.ollamaOptions(req),
	}

	o.logf("ollama: POST %s/api/chat stream=false model=%s messages=%d tools=%d",
		o.baseURL, chatReq.Model, len(chatReq.Messages), len(chatReq.Tools))

	var chatRes ollamaChatResponse
	if err := o.postJSON(ctx, "/api/chat", chatReq, &chatRes); err != nil {
		o.logf("ollama: SendMessageWithTools error: %v", err)
		return common.CompletionResponse{}, fmt.Errorf("ollama send message with tools: %w", err)
	}

	o.logf("ollama: SendMessageWithTools done_reason=%s tool_calls=%d",
		chatRes.DoneReason, len(chatRes.Message.ToolCalls))

	return fromOllamaResponse(chatRes, o.log), nil
}

func (o *Client) GetCurrentModel() string {
	return o.model.GetName()
}

// effectiveContextSize is the context window the server actually runs at:
// Config.ContextSize when set, else the model's default operating window.
func (o *Client) effectiveContextSize() int64 {
	if o.contextSize > 0 {
		return o.contextSize
	}
	return int64(o.model.GetDefaultContextWindow())
}

func (o *Client) GetContextWindowSize() int {
	return int(o.effectiveContextSize())
}

func (o *Client) GetModel() common.Model {
	return o.model
}

// CountTokens is not supported by Ollama. Returns ErrNotSupported.
func (o *Client) CountTokens(_ context.Context, _ common.CompletionRequest) (common.TokenCount, error) {
	return common.TokenCount{}, fmt.Errorf("ollama count tokens: %w", common.ErrNotSupported)
}

func (o *Client) ListModels(ctx context.Context) ([]common.ModelInfo, error) {
	var modelsRes ollamaModelsResponse

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("ollama create request: %w", err)
	}

	resp, err := o.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama list models: %w", apiError(0, err.Error(), err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama list models: %w", apiError(resp.StatusCode, readErrorBody(resp.Body), nil))
	}

	if err := json.NewDecoder(resp.Body).Decode(&modelsRes); err != nil {
		return nil, fmt.Errorf("ollama list models decode: %w", err)
	}

	models := make([]common.ModelInfo, 0, len(modelsRes.Models))
	for _, m := range modelsRes.Models {
		models = append(models, common.ModelInfo{
			ID:   m.Model,
			Name: m.Name,
		})
	}

	return models, nil
}

// -- HTTP helper --

func (o *Client) postJSON(ctx context.Context, path string, input any, output any) error {
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
		return apiError(0, err.Error(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return apiError(resp.StatusCode, readErrorBody(resp.Body), nil)
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

func (o *Client) ollamaOptions(req common.CompletionRequest) map[string]any {
	opts := map[string]any{}
	if req.MaxTokens > 0 {
		opts["num_predict"] = req.MaxTokens
	}
	// num_ctx sizes the server's KV cache, so default to the model's
	// DefaultContextWindow rather than its full context window (256K+ would
	// exhaust memory on typical hardware). Config.ContextSize overrides.
	if contextSize := o.effectiveContextSize(); contextSize > 0 {
		opts["num_ctx"] = contextSize
	}
	if len(opts) == 0 {
		return nil
	}
	return opts
}

// -- converters --

func toOllamaMessages(req common.CompletionRequest, log *slog.Logger) []ollamaChatMessage {
	var msgs []ollamaChatMessage

	if req.System != "" {
		msgs = append(msgs, ollamaChatMessage{
			Role:    "system",
			Content: req.System,
		})
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case common.RoleSystem:
			msgs = append(msgs, ollamaChatMessage{
				Role:    "system",
				Content: common.CombinedText(msg.Content),
			})
		case common.RoleUser:
			user := ollamaChatMessage{
				Role:    "user",
				Content: common.CombinedText(msg.Content),
			}
			for _, block := range msg.Content {
				if block.Type == common.ContentTypeImage && block.Image != nil {
					user.Images = append(user.Images, block.Image.Data)
				}
			}
			msgs = append(msgs, user)
		case common.RoleAssistant:
			assistant := ollamaChatMessage{
				Role:    "assistant",
				Content: common.CombinedText(msg.Content),
			}
			for _, block := range msg.Content {
				if block.Type != common.ContentTypeToolUse {
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
		case common.RoleTool:
			for _, block := range msg.Content {
				if block.Type == common.ContentTypeToolResult {
					msgs = append(msgs, ollamaChatMessage{
						Role:     "tool",
						Content:  block.ToolOutput,
						ToolName: block.ToolName,
					})
				}
			}
		}
	}

	return msgs
}

func toOllamaTools(tools []common.ToolDefinition, log *slog.Logger) ([]ollamaTool, error) {
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

// StripThinkBlocks returns only the answer text of a response that inlines
// think tags (models that ignore think:false). Handles both paired
// <think>...</think> blocks and the bare closers GLM/Qwen templates produce
// when the opening tag lives in the prompt. Exported so callers accumulating
// streaming text can also strip think blocks; use splitThinkBlocks-backed
// responses (thinking content blocks) when the reasoning must be kept.
func StripThinkBlocks(s string) string {
	_, text := splitThinkBlocks(s)
	return text
}

// splitThinkBlocks classifies inline-tagged model output into chain-of-thought
// and answer text. Paired <think>...</think> blocks are extracted first; if a
// bare </think> remains (the opening tag was part of the prompt template),
// everything before the last closer is reasoning. Nothing is discarded.
func splitThinkBlocks(s string) (thinking, text string) {
	var th strings.Builder
	for _, m := range thinkBlockRe.FindAllStringSubmatch(s, -1) {
		th.WriteString(m[1])
	}
	text = thinkBlockRe.ReplaceAllString(s, "")
	if idx := strings.LastIndex(text, thinkClose); idx >= 0 {
		th.WriteString(text[:idx])
		text = text[idx+len(thinkClose):]
	}
	return strings.TrimSpace(th.String()), strings.TrimSpace(text)
}

type thinkBlockParser struct {
	events  chan<- common.StreamEvent
	inThink bool
	buf     strings.Builder
}

const thinkOpen = "<think>"
const thinkClose = "</think>"

func newThinkBlockParser(events chan<- common.StreamEvent) *thinkBlockParser {
	return &thinkBlockParser{events: events}
}

func (p *thinkBlockParser) Feed(text string) {
	if p.buf.Len() > 0 {
		text = p.buf.String() + text
		p.buf.Reset()
	}
	for len(text) > 0 {
		if p.inThink {
			idx := strings.Index(text, thinkClose)
			if idx >= 0 {
				if idx > 0 {
					p.events <- common.StreamEvent{Type: common.StreamEventThinking, Text: text[:idx]}
				}
				p.inThink = false
				text = text[idx+len(thinkClose):]
			} else {
				n := p.matchTagPrefix(text, thinkClose)
				if n > 0 {
					if len(text)-n > 0 {
						p.events <- common.StreamEvent{Type: common.StreamEventThinking, Text: text[:len(text)-n]}
					}
					p.buf.WriteString(text[len(text)-n:])
				} else {
					p.events <- common.StreamEvent{Type: common.StreamEventThinking, Text: text}
				}
				return
			}
		} else {
			idx := strings.Index(text, thinkOpen)
			if idx >= 0 {
				if idx > 0 {
					p.events <- common.StreamEvent{Type: common.StreamEventDelta, Text: text[:idx]}
				}
				p.inThink = true
				text = text[idx+len(thinkOpen):]
			} else {
				n := p.matchTagPrefix(text, thinkOpen)
				if n > 0 {
					if len(text)-n > 0 {
						p.events <- common.StreamEvent{Type: common.StreamEventDelta, Text: text[:len(text)-n]}
					}
					p.buf.WriteString(text[len(text)-n:])
				} else {
					p.events <- common.StreamEvent{Type: common.StreamEventDelta, Text: text}
				}
				return
			}
		}
	}
}

func (p *thinkBlockParser) Flush() {
	if p.buf.Len() > 0 {
		evType := common.StreamEventDelta
		if p.inThink {
			evType = common.StreamEventThinking
		}
		p.events <- common.StreamEvent{Type: evType, Text: p.buf.String()}
		p.buf.Reset()
	}
}

func (p *thinkBlockParser) matchTagPrefix(text, tag string) int {
	for i := 1; i < len(tag) && i <= len(text); i++ {
		if text[len(text)-i:] == tag[:i] {
			return i
		}
	}
	return 0
}

func fromOllamaResponse(res ollamaChatResponse, log *slog.Logger) common.CompletionResponse {
	var content []common.ContentBlock

	// Thinking arrives either in the native field (server separated it) or
	// inline as think tags (model ignored think:false). Classify both as
	// thinking blocks — surface, don't sanitize.
	thinking, text := splitThinkBlocks(res.Message.Content)
	if res.Message.Thinking != "" {
		content = append(content, common.NewThinkingContent(res.Message.Thinking))
	}
	if thinking != "" {
		content = append(content, common.NewThinkingContent(thinking))
	}
	if text != "" {
		content = append(content, common.NewTextContent(text))
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
		content = append(content, common.NewToolUseContent(id, tc.Function.Name, args))
	}

	responseID := fmt.Sprintf("ollama-%s-%d", res.Model, res.PromptEvalCount+res.EvalCount)

	return common.CompletionResponse{
		ID:         responseID,
		Content:    content,
		StopReason: fromOllamaStopReason(res),
		Usage: common.Usage{
			InputTokens:  int64(res.PromptEvalCount),
			OutputTokens: int64(res.EvalCount),
		},
		Model: res.Model,
	}
}

func fromOllamaStopReason(res ollamaChatResponse) common.StopReason {
	if len(res.Message.ToolCalls) > 0 {
		return common.StopReasonToolUse
	}
	switch res.DoneReason {
	case "stop":
		return common.StopReasonStop
	case "length":
		return common.StopReasonMaxTokens
	default:
		return common.StopReason(res.DoneReason)
	}
}
