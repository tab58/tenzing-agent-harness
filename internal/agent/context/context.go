package context

import (
	"context"
	"fmt"
	"tenzing-agent/internal/agent/context/compressor"
	"tenzing-agent/internal/provider"
)

type Context struct {
	messages []provider.Message

	compressor *compressor.Compressor
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

func (c *Context) Messages() []provider.Message {
	out := make([]provider.Message, len(c.messages))
	copy(out, c.messages)
	return out
}

func (c *Context) Len() int {
	return len(c.messages)
}

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
