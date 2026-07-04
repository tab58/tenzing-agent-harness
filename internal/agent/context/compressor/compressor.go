package compressor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tab58/llm-providers/common"
)

const (
	MemoryFileName    = ".agent_memory.md"
	KeepRecent        = 6
	maxSummarizeInput = 20_000
	toolOutputWeight  = 4
	// ~4 chars per token, compress at 75% of context window
	charsPerToken        = 4
	compressAtFraction   = 3      // numerator; denominator is charsPerToken (i.e. 3/4 = 75%)
	defaultContextWindow = 10_000 // tokens, fallback when caller doesn't specify
)

type Compressor struct {
	llm          common.LLM
	memoryFile   string
	threshold    int
	todoProvider func() string
}

func (c *Compressor) SetTodoProvider(fn func() string) {
	c.todoProvider = fn
}

func NewCompressor(llm common.LLM, contextWindow int) *Compressor {
	memFile := MemoryFileName

	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}

	return &Compressor{
		llm:        llm,
		memoryFile: memFile,
		threshold:  contextWindow * compressAtFraction,
	}
}

// MaybeCompress checks whether the message history exceeds the threshold.
// If so, it summarizes the older portion via LLM, persists it to disk,
// and returns a shorter history with the summary prepended.
func (c *Compressor) MaybeCompress(ctx context.Context, messages []common.Message) ([]common.Message, bool, error) {
	if c.EstimateSize(messages) < c.threshold {
		return messages, false, nil
	}
	if len(messages) <= KeepRecent {
		return messages, false, nil
	}

	splitIdx := len(messages) - KeepRecent
	old := messages[:splitIdx]
	recent := messages[splitIdx:]

	summary, err := c.summarize(ctx, old)
	if err != nil {
		return messages, false, fmt.Errorf("compression summarize: %w", err)
	}

	if err := c.SaveToMemoryFile(summary); err != nil {
		return messages, false, fmt.Errorf("save memory: %w", err)
	}

	compressed := make([]common.Message, 0, 3+len(recent))
	compressed = append(compressed,
		common.NewUserMessage("[Context summary from previous conversation]\n\n"+summary),
	)

	if c.todoProvider != nil {
		if todoState := c.todoProvider(); todoState != "" {
			compressed = append(compressed,
				common.NewUserMessage("[Current plan state — persisted from disk]\n\n"+todoState),
			)
		}
	}

	compressed = append(compressed,
		common.NewAssistantMessage("Understood. I have the full context from our previous work."),
	)
	compressed = append(compressed, recent...)

	return compressed, true, nil
}

func (c *Compressor) Threshold() int { return c.threshold }

func (c *Compressor) EstimateSize(messages []common.Message) int {
	size := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			size += len(block.Text)
			size += len(block.ToolOutput) / toolOutputWeight
			size += len(block.ToolInput)
		}
	}
	return size
}

func (c *Compressor) LoadFromMemoryFile() (string, error) {
	data, err := os.ReadFile(c.memoryFile)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read memory: %w", err)
	}
	return string(data), nil
}

func (c *Compressor) SaveToMemoryFile(summary string) error {
	content := fmt.Sprintf("# Agent Memory\nUpdated: %s\n\n%s\n",
		time.Now().Format(time.RFC3339), summary)
	return os.WriteFile(c.memoryFile, []byte(content), 0644)
}

func (c *Compressor) summarize(ctx context.Context, messages []common.Message) (string, error) {
	var sb strings.Builder
	for _, msg := range messages {
		fmt.Fprintf(&sb, "[%s] ", msg.Role)
		for _, block := range msg.Content {
			if block.Text != "" {
				sb.WriteString(block.Text)
			}
			if block.ToolOutput != "" {
				sb.WriteString(block.ToolOutput)
			}
		}
		sb.WriteString("\n")
	}

	text := sb.String()
	if len(text) > maxSummarizeInput {
		text = text[:maxSummarizeInput]
	}

	resp, err := c.llm.SendSyncMessage(ctx, common.CompletionRequest{
		Model:     c.llm.GetCurrentModel(),
		System:    "Summarise this conversation. Keep all important decisions, code changes, file paths, and context. Be concise but complete.",
		Messages:  []common.Message{common.NewUserMessage(text)},
		MaxTokens: 4096,
	})
	if err != nil {
		return "", fmt.Errorf("summarize call: %w", err)
	}
	return resp.Text(), nil
}
