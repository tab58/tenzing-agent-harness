package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	httpserver "github.com/tab58/huma-http-server"
	srverrors "github.com/tab58/huma-http-server/errors"
	"github.com/tab58/huma-http-server/router"

	"github.com/tab58/llm-providers/common"
	"github.com/tab58/llm-providers/ollama"
	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/runner"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	logFile, err := os.OpenFile(filepath.Join(cwd, ".tenzing-agent.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening log file: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: runner.LevelTrace})))

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic", "error", r, "stack", string(debug.Stack()))
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			os.Exit(1)
		}
	}()

	llm := ollama.NewClient(ollama.Config{
		APIKey: os.Getenv("OLLAMA_API_KEY"),
		Model: common.ModelDefinition{
			Name:                 "glm-5.2",
			MaxTokens:            32_768,
			ContextWindowSize:    131_072,
			DefaultContextWindow: 32_768,
		},
	})

	bus := events.NewEventBus()

	mainAgent, err := agent.New(agent.AgentConfig{
		Model: llm,
	})
	if err != nil {
		slog.Error("agent init failed", "error", err)
		fmt.Fprintf(os.Stderr, "agent init failed: %v\n", err)
		os.Exit(1)
	}

	// create the HTTP server

	s := newServer(cwd, llm, mainAgent, bus)

	srv := httpserver.New(httpserver.ServerConfig{
		ServiceName:    "tenzing-agent",
		ServiceVersion: "0.1.0",
	}, router.MapAuthInfoBuilder)
	s.registerRoutes(srv)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	defer s.harness.Shutdown()

	errCh, err := srv.Start(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "listening on %s\n", addr)

	select {
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}
}

type server struct {
	harness *harness.Harness
	bus     *events.EventBus

	mu       sync.Mutex
	cancelFn context.CancelFunc
}

func newServer(cwd string, llm common.LLM, mainAgent runner.Agent, bus *events.EventBus) *server {
	s := &server{
		bus: bus,
	}

	h, err := harness.New(harness.HarnessConfig{
		Cwd:             cwd,
		Agent:           mainAgent,
		EventBus:        bus,
		OnTextDelta:     func(text string) { s.broadcastSSE("text_delta", text) },
		OnThinkingDelta: func(text string) { s.broadcastSSE("thinking_delta", text) },
		RLMModel:        llm,
	})
	if err != nil {
		slog.Error("harness init failed", "error", err)
		fmt.Fprintf(os.Stderr, "harness init failed: %v\n", err)
		os.Exit(1)
	}
	s.harness = h

	evCh := bus.Subscribe(256)
	go s.forwardEvents(evCh)

	return s
}

// registerRoutes mounts the API on the huma server. The index page and SSE
// stream are raw routes (they bypass huma middleware and the OpenAPI spec);
// the JSON endpoints are typed routes.
func (s *server) registerRoutes(srv *httpserver.Server[router.MapAuthInfo]) {
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
}

// --- SSE broadcast ---

type sseClient struct {
	ch   chan sseMessage
	done chan struct{}
}

type sseMessage struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

var (
	sseClients   = make(map[*sseClient]struct{})
	sseClientsMu sync.RWMutex
)

func addSSEClient(c *sseClient) {
	sseClientsMu.Lock()
	sseClients[c] = struct{}{}
	sseClientsMu.Unlock()
}

func removeSSEClient(c *sseClient) {
	sseClientsMu.Lock()
	delete(sseClients, c)
	sseClientsMu.Unlock()
}

func (s *server) broadcastSSE(event, data string) {
	msg := sseMessage{Event: event, Data: data}
	sseClientsMu.RLock()
	defer sseClientsMu.RUnlock()
	for c := range sseClients {
		select {
		case c.ch <- msg:
		default:
		}
	}
}

func (s *server) broadcastSSEJSON(event string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("sse json marshal", "error", err)
		return
	}
	s.broadcastSSE(event, string(b))
}

func (s *server) forwardEvents(ch <-chan events.Event) {
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
		}
	}
}

// --- Handlers ---

func (s *server) handleSSE(w http.ResponseWriter, r *http.Request) {
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
		ch:   make(chan sseMessage, 128),
		done: make(chan struct{}),
	}
	addSSEClient(client)
	defer removeSSEClient(client)

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

func (s *server) handleQuery(_ context.Context, _ router.MapAuthInfo, in *queryInput) (*statusOutput, error) {
	query := strings.TrimSpace(in.Body.Query)
	if query == "" {
		return nil, srverrors.Wrap(srverrors.ErrBadRequest, "empty query")
	}

	s.mu.Lock()
	if s.cancelFn != nil {
		s.mu.Unlock()
		return nil, srverrors.Wrap(srverrors.ErrConflict, "agent already running")
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
			s.mu.Lock()
			s.cancelFn = nil
			s.mu.Unlock()
			s.broadcastSSEJSON("status", map[string]string{"state": "idle"})
		}()

		answer, err := s.harness.RunTurn(ctx, query)
		if err != nil {
			s.broadcastSSEJSON("error", map[string]string{"error": err.Error()})
			return
		}
		s.broadcastSSEJSON("answer", map[string]string{"text": answer})
	}()

	out := &statusOutput{}
	out.Body.Status = "started"
	return out, nil
}

func (s *server) handleCancel(_ context.Context, _ router.MapAuthInfo, _ *struct{}) (*statusOutput, error) {
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

func (s *server) handleInfo(_ context.Context, _ router.MapAuthInfo, _ *struct{}) (*infoOutput, error) {
	out := &infoOutput{}
	out.Body.Tools = len(s.harness.ToolDefinitions())
	return out, nil
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}
