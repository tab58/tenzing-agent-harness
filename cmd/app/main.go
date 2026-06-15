package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// TODO: build harness (agent, registry) and call h.RunSession(ctx, os.Stdin, os.Stdout)

	_ = ctx
	fmt.Println("Hello, World!")
}
