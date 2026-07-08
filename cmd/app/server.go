package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	httpserver "github.com/tab58/huma-http-server"
	srverrors "github.com/tab58/huma-http-server/errors"
	"github.com/tab58/huma-http-server/router"

	"tenzing-agent/internal/app"
	"tenzing-agent/internal/app/nexus"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/events"

	"github.com/tab58/llm-providers/common"
)

// agentServer exposes the harness over HTTP: an index page, an SSE event
// stream, and JSON endpoints to start/cancel agent turns.
type agentServer struct {
	harness   *harness.Harness
	bus       *events.EventBus
	nexus     *nexus.Nexus // nil when no channels configured
	logB      *app.LogBroadcaster
	onTurnEnd func() // trigger flush hook; called after every turn

	mu       sync.Mutex
	cancelFn context.CancelFunc
	closing  bool

	clients   map[*sseClient]struct{}
	clientsMu sync.RWMutex
}

func newAgentServer(model common.ModelDefinition, bus *events.EventBus, nx *nexus.Nexus, logB *app.LogBroadcaster, onTurnEnd func(), extraOpts ...harness.HarnessOption) (*agentServer, error) {
	s := &agentServer{
		bus:       bus,
		nexus:     nx,
		logB:      logB,
		onTurnEnd: onTurnEnd,
		clients:   make(map[*sseClient]struct{}),
	}

	opts := append([]harness.HarnessOption{
		harness.WithEventBus(bus),
		harness.WithTextDeltaHandler(func(text string) { s.broadcastSSE("text_delta", text) }),
		harness.WithThinkingDeltaHandler(func(text string) { s.broadcastSSE("thinking_delta", text) }),
	}, extraOpts...)

	h, err := harness.New(model, opts...)
	if err != nil {
		return nil, fmt.Errorf("harness init: %w", err)
	}
	s.harness = h

	evCh := bus.Subscribe(256)
	go s.forwardEvents(evCh)

	return s, nil
}

// registerRoutes mounts the API on the huma server. The index page and SSE
// stream are raw routes (they bypass huma middleware and the OpenAPI spec);
// the JSON endpoints are typed routes.
func (s *agentServer) registerRoutes(srv *httpserver.Server[router.MapAuthInfo]) {
	srv.Handle("GET /", http.HandlerFunc(s.handleIndex))
	srv.Handle("GET /events", http.HandlerFunc(s.handleSSE))

	httpserver.RegisterRoute(srv, router.RegisterRouteArgs[queryInput, statusOutput, router.MapAuthInfo]{
		Operation: huma.Operation{
			OperationID: "query",
			Method:      http.MethodPost,
			Path:        "/query",
		},
		Handler: s.handleQuery,
	})
	httpserver.RegisterRoute(srv, router.RegisterRouteArgs[struct{}, statusOutput, router.MapAuthInfo]{
		Operation: huma.Operation{
			OperationID: "cancel",
			Method:      http.MethodPost,
			Path:        "/cancel",
		},
		Handler: s.handleCancel,
	})
	httpserver.RegisterRoute(srv, router.RegisterRouteArgs[struct{}, infoOutput, router.MapAuthInfo]{
		Operation: huma.Operation{
			OperationID: "info",
			Method:      http.MethodGet,
			Path:        "/info",
		},
		Handler: s.handleInfo,
	})

	srv.Handle("GET /debug", s.logB.SSEHandler())
	if s.nexus != nil {
		srv.Handle("POST /ingest/{name}", s.nexus.WebhookHandler())
	}
}

// --- SSE broadcast ---

type sseClient struct {
	ch chan sseMessage
}

type sseMessage struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

func (s *agentServer) addSSEClient(c *sseClient) {
	s.clientsMu.Lock()
	s.clients[c] = struct{}{}
	s.clientsMu.Unlock()
}

func (s *agentServer) removeSSEClient(c *sseClient) {
	s.clientsMu.Lock()
	delete(s.clients, c)
	s.clientsMu.Unlock()
}

func (s *agentServer) broadcastSSE(event, data string) {
	msg := sseMessage{Event: event, Data: data}
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	for c := range s.clients {
		select {
		case c.ch <- msg:
		default:
		}
	}
}

func (s *agentServer) broadcastSSEJSON(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("sse json marshal", "error", err)
		return
	}
	s.broadcastSSE(event, string(b))
}

func (s *agentServer) forwardEvents(ch <-chan events.Event) {
	for ev := range ch {
		switch e := ev.(type) {
		case events.ToolExecutionStartedEvent:
			s.broadcastSSEJSON("tool_start", map[string]string{
				"name":  e.ToolName,
				"input": e.Input,
			})
		case events.ToolSucceededEvent:
			s.broadcastSSEJSON("tool_result", map[string]string{
				"name":   e.ToolName,
				"input":  e.Input,
				"output": e.Output,
			})
		case events.ToolFailedEvent:
			s.broadcastSSEJSON("tool_result", map[string]string{
				"name":   e.ToolName,
				"input":  e.Input,
				"output": e.Error,
				"error":  "true",
			})
		case events.LLMResponseEvent:
			s.broadcastSSEJSON("llm_meta", map[string]int64{
				"input_tokens":  e.InputTokens,
				"output_tokens": e.OutputTokens,
			})
		case events.ToolProgressEvent:
			s.broadcastSSEJSON("tool_progress", map[string]string{
				"tool":   e.ToolName,
				"phase":  e.Phase,
				"detail": e.Detail,
			})
		case nexus.ChannelErrorEvent:
			s.broadcastSSEJSON("channel_error", map[string]any{
				"channel": e.Channel,
				"text":    e.Text,
				"seq":     e.Seq,
			})
		case nexus.ChannelStatusEvent:
			s.broadcastSSEJSON("channel_status", map[string]string{
				"channel": e.Channel,
				"state":   e.State,
			})
		case nexus.TriggerEvent:
			s.broadcastSSEJSON("nexus_trigger", map[string]any{
				"channels": e.Channels,
			})
		}
	}
}

// --- Handlers ---

func (s *agentServer) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := &sseClient{
		ch: make(chan sseMessage, 128),
	}
	s.addSSEClient(client)
	defer s.removeSSEClient(client)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-client.ch:
			data := strings.ReplaceAll(msg.Data, "\n", "\ndata: ")
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.Event, data)
			flusher.Flush()
		}
	}
}

type queryInput struct {
	Body struct {
		Query string `json:"query" doc:"Prompt to run the agent with"`
	}
}

type statusOutput struct {
	Body struct {
		Status string `json:"status" doc:"Result of the request"`
	}
}

type infoOutput struct {
	Body struct {
		Tools int `json:"tools" doc:"Number of registered tools"`
	}
}

// startTurn begins an agent turn for query. Returns false when a turn is
// already running. Used by both the HTTP /query handler and nexus wakes.
func (s *agentServer) startTurn(query string) bool {
	s.mu.Lock()
	if s.closing || s.cancelFn != nil {
		s.mu.Unlock()
		return false
	}
	// the turn outlives the HTTP request, so it gets its own context
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelFn = cancel
	s.mu.Unlock()

	s.broadcastSSEJSON("status", map[string]string{"state": "running", "query": query})

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("agent panic", "error", rec)
				s.broadcastSSEJSON("error", map[string]string{"error": fmt.Sprintf("panic: %v", rec)})
			}
			cancel()
			s.mu.Lock()
			s.cancelFn = nil
			s.mu.Unlock()
			s.broadcastSSEJSON("status", map[string]string{"state": "idle"})
			if s.onTurnEnd != nil {
				s.onTurnEnd()
			}
		}()

		answer, err := s.harness.RunTurn(ctx, query)
		if err != nil {
			s.broadcastSSEJSON("error", map[string]string{"error": err.Error()})
			return
		}
		s.broadcastSSEJSON("answer", map[string]string{"text": answer})
	}()
	return true
}

func (s *agentServer) handleQuery(_ context.Context, _ router.MapAuthInfo, in *queryInput) (*statusOutput, error) {
	query := strings.TrimSpace(in.Body.Query)
	if query == "" {
		return nil, srverrors.Wrap(srverrors.ErrBadRequest, "empty query")
	}
	if !s.startTurn(query) {
		return nil, srverrors.Wrap(srverrors.ErrConflict, "agent already running")
	}
	out := &statusOutput{}
	out.Body.Status = "started"
	return out, nil
}

// startNexusTurn is the trigger wake callback: builds an investigation
// prompt from recent channel errors and starts a turn. Returns false when
// the agent is busy (trigger keeps the channels pending).
func (s *agentServer) startNexusTurn(channels []string) bool {
	if s.nexus == nil {
		return false
	}
	prompt := s.nexusPrompt(channels)
	if !s.startTurn(prompt) {
		return false
	}
	s.bus.Emit(nexus.TriggerEvent{
		BaseEvent: events.NewBaseEvent(nexus.EventTrigger, "nexus"),
		Channels:  channels,
	})
	return true
}

func (s *agentServer) nexusPrompt(channels []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Error detected in channel(s) %s.\n", strings.Join(channels, ", "))
	for _, name := range channels {
		entries, err := s.nexus.Read(name, 5, true)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "\nRecent errors from %q:\n", name)
		for _, e := range entries {
			fmt.Fprintf(&b, "  [%d] %s\n", e.Seq, e.Text)
		}
	}
	b.WriteString("\nUse the read_channel and search_channel tools for more context. Investigate the root cause and report what you find.")
	return b.String()
}

// cancelActiveTurn cancels the in-flight agent turn, if any. Used by
// container shutdown so a running turn doesn't outlive the harness.
func (s *agentServer) cancelActiveTurn() {
	s.mu.Lock()
	s.closing = true
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.mu.Unlock()
}

func (s *agentServer) handleCancel(_ context.Context, _ router.MapAuthInfo, _ *struct{}) (*statusOutput, error) {
	s.mu.Lock()
	cancel := s.cancelFn
	s.mu.Unlock()

	if cancel == nil {
		return nil, srverrors.Wrap(srverrors.ErrBadRequest, "nothing running")
	}
	cancel()
	out := &statusOutput{}
	out.Body.Status = "cancelled"
	return out, nil
}

func (s *agentServer) handleInfo(_ context.Context, _ router.MapAuthInfo, _ *struct{}) (*infoOutput, error) {
	out := &infoOutput{}
	out.Body.Tools = len(s.harness.ToolDefinitions())
	return out, nil
}

func (s *agentServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}
