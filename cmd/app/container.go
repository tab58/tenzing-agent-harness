package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	httpserver "github.com/tab58/huma-http-server"
	"github.com/tab58/huma-http-server/config"
	"github.com/tab58/huma-http-server/router"
	"github.com/tab58/llm-providers/common"
	"github.com/tab58/llm-providers/ollama"

	"tenzing-agent/internal/agent"
	"tenzing-agent/internal/harness"
	"tenzing-agent/internal/harness/runner"
)

type Config struct {
	APIKeyOllama string `mapstructure:"OLLAMA_API_KEY" sensitive:"true"`
	ServerPort   int    `mapstructure:"SERVER_PORT" default:"8080"`
	LogDebug     bool   `mapstructure:"LOG_DEBUG"`
}

// AppContainer wires all app-level dependencies for cmd/app: config,
// logging, the agent harness (which owns the LLM), and the HTTP server.
type AppContainer struct {
	cfg     *Config
	cwd     string
	logFile *os.File
	harness *harness.Harness
	server  *httpserver.Server[router.MapAuthInfo]
}

// NewAppContainer builds the container eagerly: config → cwd → logging →
// LLM → agent → harness → HTTP server. Any failure after the log file
// opens closes it before returning.
func NewAppContainer() (*AppContainer, error) {
	cfg := &Config{}
	if err := config.Load(cfg); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}

	logFile, err := setupLogging(cwd, cfg.LogDebug)
	if err != nil {
		return nil, err
	}

	llm := ollama.NewClient(ollama.Config{
		APIKey: cfg.APIKeyOllama,
		Model: common.ModelDefinition{
			Name:                 "glm-5.2",
			MaxTokens:            32_768,
			ContextWindowSize:    131_072,
			DefaultContextWindow: 32_768,
		},
	})

	mainAgent, err := agent.New(agent.AgentConfig{
		Model: llm,
	})
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("agent init: %w", err)
	}

	agentHarness, err := harness.New(harness.HarnessConfig{
		Cwd:      cwd,
		Agent:    mainAgent,
		RLMModel: llm,
	})
	if err != nil {
		logFile.Close()
		return nil, fmt.Errorf("harness init: %w", err)
	}

	server := httpserver.New(httpserver.ServerConfig{
		ServiceName:    "tenzing-agent",
		ServiceVersion: "0.1.0",
	}, router.MapAuthInfoBuilder)

	slog.Info("container ready", "model", llm.GetCurrentModel(), "cwd", cwd, "tools", len(agentHarness.ToolDefinitions()))

	return &AppContainer{
		cfg:     cfg,
		cwd:     cwd,
		logFile: logFile,
		harness: agentHarness,
		server:  server,
	}, nil
}

// setupLogging opens the log file and installs it as the slog default.
// Debug runs get a fresh timestamped file at trace level; normal runs
// append to the standard file at info level.
func setupLogging(cwd string, debug bool) (*os.File, error) {
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

	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: level})))
	return logFile, nil
}

// Start runs the HTTP server and the interactive session concurrently.
// A server error cancels the session; the server error wins.
func (ac *AppContainer) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh, err := ac.server.Start(fmt.Sprintf(":%d", ac.cfg.ServerPort))
	if err != nil {
		return fmt.Errorf("http server start: %w", err)
	}

	srvErr := make(chan error, 1)
	go func() {
		if e := <-errCh; e != nil {
			srvErr <- e
			cancel()
		}
	}()

	sessErr := ac.harness.RunSession(ctx, os.Stdin, os.Stdout)

	select {
	case e := <-srvErr:
		return fmt.Errorf("http server: %w", e)
	default:
	}
	return sessErr
}

// Shutdown stops the HTTP server, the harness, and closes the log file.
// Called once from main's defer.
func (ac *AppContainer) Shutdown() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ac.server.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown", "error", err)
	}
	ac.harness.Shutdown()
	ac.logFile.Close()
}

func (ac *AppContainer) Harness() *harness.Harness {
	return ac.harness
}

func (ac *AppContainer) Cwd() string {
	return ac.cwd
}

func (ac *AppContainer) Server() *httpserver.Server[router.MapAuthInfo] {
	return ac.server
}
