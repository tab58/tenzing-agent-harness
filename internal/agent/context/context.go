package context

import (
	"context"
	"fmt"
	"log/slog"
	"tenzing-agent/internal/agent/context/compressor"
	"tenzing-agent/internal/provider"
)

type Context struct {
	messages []provider.Message

	compressor *compressor.Compressor
	offloadFn  func(context.Context, string) (string, error)
}

type ContextConfig struct {
	LLM provider.LLM
}

func NewContext(cfg ContextConfig) (*Context, error) {
	llm := cfg.LLM
	if llm == nil {
		return nil, fmt.Errorf("model is undefined for compressor")
	}
	contextWindowSize := cfg.LLM.GetContextWindowSize()
	compressor := compressor.NewCompressor(llm, contextWindowSize)

	ctx := &Context{
		messages:   make([]provider.Message, 0),
		compressor: compressor,
	}

	if err := ctx.LoadFromMemoryFile(); err != nil {
		return nil, fmt.Errorf("load memory: %w", err)
	}

	return ctx, nil
}

func (c *Context) UpdateOffloadFn(offloadFn func(context.Context, string) (string, error)) {
	c.offloadFn = offloadFn
}

// check for a context overflow
func (c *Context) ClassifyOverflow(ctx context.Context, inputs []string) (string, int, error) {
	if c.offloadFn != nil {
		cause, idx := compressor.ClassifyOverflow(c.Messages(), inputs, c.Threshold())
		if (cause == compressor.OverflowLargeInput || cause == compressor.OverflowBoth) && idx >= 0 {
			slog.Info("[offload] routing large input to RLM", "input_len", len(inputs[idx]), "cause", cause)
			result, err := c.offloadFn(ctx, inputs[idx])
			if err != nil {
				return "", idx, err
			} else {
				return result, idx, nil
			}
		}
	}
	return "", 0, nil
}

func (c *Context) Messages() []provider.Message {
	out := make([]provider.Message, len(c.messages))
	copy(out, c.messages)
	return out
}

func (c *Context) Len() int {
	return len(c.messages)
}

func (c *Context) Threshold() int { return c.compressor.Threshold() }

func (c *Context) LoadFromMemoryFile() error {
	mem, err := c.compressor.LoadFromMemoryFile()
	if err != nil {
		return fmt.Errorf("load memory: %w", err)
	}
	if mem != "" {
		c.messages = append(c.messages,
			provider.NewUserMessage("[Context summary from previous conversation]\n\n"+mem),
			provider.NewAssistantMessage("Understood. I have the full context from our previous work."),
		)
	}
	return nil
}

func (c *Context) AppendMessages(ctx context.Context, messages ...provider.Message) (bool, error) {
	c.messages = append(c.messages, messages...)

	if len(messages) == 0 || messages[len(messages)-1].Role != provider.RoleAssistant {
		return false, nil
	}

	compressed, did, err := c.compressor.MaybeCompress(ctx, c.messages)
	if err != nil {
		return false, fmt.Errorf("compression check: %w", err)
	}
	if did {
		c.messages = compressed
	}
	return did, nil
}
