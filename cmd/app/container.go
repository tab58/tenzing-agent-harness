package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	httpserver "github.com/tab58/huma-http-server"
	"github.com/tab58/huma-http-server/config"
	"github.com/tab58/huma-http-server/router"
	"github.com/tab58/llm-providers/common"

	"github.com/tab58/tenzing-agent-harness/internal/app"
	"github.com/tab58/tenzing-agent-harness/internal/app/nexus"
	nexustools "github.com/tab58/tenzing-agent-harness/internal/app/nexus/tools"
	"github.com/tab58/tenzing-agent-harness/internal/harness"
	"github.com/tab58/tenzing-agent-harness/internal/harness/events"
	"github.com/tab58/tenzing-agent-harness/internal/harness/runner"
)

type Config struct {
	ServerPort  int    `mapstructure:"SERVER_PORT" default:"8080"`
	LogDebug    bool   `mapstructure:"LOG_DEBUG"`
	NexusConfig string `mapstructure:"NEXUS_CONFIG" default:"nexus.yaml"`
}

// AppContainer wires all app-level dependencies for cmd/app: config,
// logging, the agent server (which owns the harness and LLM), and the
// HTTP server it is mounted on.
type AppContainer struct {
	cfg     *Config
	cwd     string
	logFile *os.File
	api     *agentServer
	nexus   *nexus.Nexus
	server  *httpserver.Server[router.MapAuthInfo]
}

// NewAppContainer builds the container eagerly: config → cwd → logging →
// agent server (harness + event bus) → HTTP routes. Any failure after the
// log file opens closes it before returning.
func NewAppContainer() (*AppContainer, error) {
	cfg := &Config{}
	if err := config.Load(cfg); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}

	logB := app.NewLogBroadcaster()
	logFile, err := setupLogging(cwd, cfg.LogDebug, logB)
	if err != nil {
		return nil, err
	}

	model := common.ModelDefinition{
		Name:                 "glm-5.2",
		MaxTokens:            32_768,
		ContextWindowSize:    131_072,
		DefaultContextWindow: 32_768,
		Provider:             common.ProviderOllama,
	}

	bus := events.NewEventBus()

	nexusCfg, err := nexus.LoadConfig(cfg.NexusConfig)
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("nexus config: %w", err)
	}

	// api is late-bound: the trigger's wake closure runs only after
	// nx.Start below, by which time api is set.
	var api *agentServer
	trig := nexus.NewTrigger(30*time.Second, func(channels []string) bool {
		if api == nil {
			return false
		}
		return api.startNexusTurn(channels)
	})

	var nx *nexus.Nexus
	if len(nexusCfg.Channels) > 0 {
		nx, err = nexus.New(nexusCfg, bus.Emit, trig.Notify)
		if err != nil {
			logFile.Close()
			return nil, fmt.Errorf("nexus init: %w", err)
		}
	}

	var extraOpts []harness.HarnessOption
	if nx != nil {
		extraOpts = append(extraOpts,
			harness.WithTool(nexustools.NewListChannelsTool(nx)),
			harness.WithTool(nexustools.NewReadChannelTool(nx)),
			harness.WithTool(nexustools.NewSearchChannelTool(nx)),
		)
	}

	api, err = newAgentServer(model, bus, nx, logB, trig.TurnEnded, extraOpts...)
	if err != nil {
		logFile.Close()
		return nil, err
	}

	if nx != nil {
		nx.Start(context.Background())
	}

	server := httpserver.New(httpserver.ServerConfig{
		ServiceName:    "tenzing-agent",
		ServiceVersion: "0.1.0",
	}, router.MapAuthInfoBuilder)
	api.registerRoutes(server)

	slog.Info("container ready", "model", api.harness.GetCurrentModel(), "cwd", cwd, "tools", len(api.harness.ToolDefinitions()))

	return &AppContainer{
		cfg:     cfg,
		cwd:     cwd,
		logFile: logFile,
		api:     api,
		nexus:   nx,
		server:  server,
	}, nil
}

// setupLogging opens the log file and installs it as the slog default,
// teeing output to the /debug SSE broadcaster. Debug runs get a fresh
// timestamped file at trace level; normal runs append at info level.
func setupLogging(cwd string, debug bool, tee io.Writer) (*os.File, error) {
	name := ".tenzing-agent.log"
	level := slog.LevelInfo
	if debug {
		name = fmt.Sprintf(".tenzing-agent-%s.log", time.Now().UTC().Format("20060102T150405Z"))
		level = runner.LevelTrace
	}

	logFile, err := os.OpenFile(filepath.Join(cwd, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(logFile, tee), &slog.HandlerOptions{Level: level})))
	return logFile, nil
}

// Start runs the HTTP server until ctx is cancelled or the server fails.
func (ac *AppContainer) Start(ctx context.Context) error {
	errCh, err := ac.server.Start(fmt.Sprintf("127.0.0.1:%d", ac.cfg.ServerPort))
	if err != nil {
		return fmt.Errorf("http server start: %w", err)
	}

	select {
	case e := <-errCh:
		if e != nil {
			return fmt.Errorf("http server: %w", e)
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

// Shutdown stops nexus sources (so no new notifies can fire), cancels any
// in-flight turn, stops the HTTP server, the harness, the event bus (which
// ends the SSE forwarding goroutine), and closes the log file. Called once
// from main's defer.
func (ac *AppContainer) Shutdown() {
	if ac.nexus != nil {
		ac.nexus.Stop()
	}
	ac.api.cancelActiveTurn()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ac.server.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown", "error", err)
	}
	ac.api.harness.Shutdown()
	ac.api.bus.Close()
	ac.logFile.Close()
}

func (ac *AppContainer) Harness() *harness.Harness {
	return ac.api.harness
}

func (ac *AppContainer) Cwd() string {
	return ac.cwd
}

func (ac *AppContainer) Addr() string {
	return fmt.Sprintf(":%d", ac.cfg.ServerPort)
}
