package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	app, err := NewAppContainer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "startup failed: %v\n", err)
		os.Exit(1)
	}
	defer app.Shutdown()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic", "error", r, "stack", string(debug.Stack()))
			fmt.Fprintf(os.Stderr, "panic: %v\n", r)
			os.Exit(1)
		}
	}()

	fmt.Println("tenzing agent harness")
	fmt.Printf("  model   %s\n", app.Harness().GetCurrentModel())
	fmt.Printf("  cwd     %s\n", app.Cwd())
	fmt.Printf("  tools   %d registered\n", len(app.Harness().ToolDefinitions()))
	fmt.Printf("  listen  http://localhost%s\n", app.Addr())
	fmt.Println()

	if err := app.Start(ctx); err != nil {
		slog.Error("server ended with error", "error", err)
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}
