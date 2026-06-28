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

	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/events"
	"tenzing-agent/internal/harness/runner"
	"tenzing-agent/internal/provider"
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

	llm := provider.NewOllamaClient(provider.OllamaConfig{
		APIKey: os.Getenv("OLLAMA_API_KEY"),
		Model:  "glm-5.2",
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

	srv := newServer(cwd, llm, mainAgent, bus)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	defer srv.harness.Shutdown()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(os.Stderr, "listening on %s\n", addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}

type server struct {
	mux     *http.ServeMux
	harness *harness.Harness
	bus     *events.EventBus

	mu       sync.Mutex
	cancelFn context.CancelFunc
}

func newServer(cwd string, llm provider.LLM, mainAgent runner.Agent, bus *events.EventBus) *server {
	s := &server{
		mux: http.NewServeMux(),
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

	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /events", s.handleSSE)
	s.mux.HandleFunc("POST /query", s.handleQuery)
	s.mux.HandleFunc("POST /cancel", s.handleCancel)
	s.mux.HandleFunc("GET /info", s.handleInfo)

	return s
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

type queryRequest struct {
	Query string `json:"query"`
}

func (s *server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		http.Error(w, "empty query", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if s.cancelFn != nil {
		s.mu.Unlock()
		http.Error(w, "agent already running", http.StatusConflict)
		return
	}
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (s *server) handleCancel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cancel := s.cancelFn
	s.mu.Unlock()

	if cancel == nil {
		http.Error(w, "nothing running", http.StatusBadRequest)
		return
	}
	cancel()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

func (s *server) handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"tools": len(s.harness.ToolDefinitions()),
	})
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}
