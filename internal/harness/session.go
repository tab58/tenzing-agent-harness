package harness

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

func (h *Harness) RunSession(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)

	for {
		if ctx.Err() != nil {
			return fmt.Errorf("context canceled: %w", ctx.Err())
		}

		if !scanner.Scan() {
			break
		}

		query := strings.TrimSpace(scanner.Text())
		if query == "" {
			continue
		}
		if query == "q" || query == "exit" {
			break
		}

		answer, err := h.RunLoop(ctx, query)
		if err != nil {
			return fmt.Errorf("agent loop error: %w", err)
		}
		fmt.Fprint(out, answer)
	}

	return scanner.Err()
}
