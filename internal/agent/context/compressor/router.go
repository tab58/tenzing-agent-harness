package compressor

import "github.com/tab58/llm-providers/common"

type OverflowCause int

const (
	OverflowNone OverflowCause = iota
	OverflowLargeInput
	OverflowHistory
	OverflowBoth
)

func ClassifyOverflow(history []common.Message, pendingInputs []string, threshold int) (OverflowCause, int) {
	historySize := estimateTextSize(history)

	largeIdx := -1
	for i, input := range pendingInputs {
		if len(input) > threshold/2 {
			if largeIdx == -1 || len(input) > len(pendingInputs[largeIdx]) {
				largeIdx = i
			}
		}
	}

	historyOverflows := historySize > threshold*3/4
	hasLargeInput := largeIdx >= 0

	switch {
	case hasLargeInput && historyOverflows:
		return OverflowBoth, largeIdx
	case hasLargeInput:
		return OverflowLargeInput, largeIdx
	case historyOverflows:
		return OverflowHistory, -1
	default:
		return OverflowNone, -1
	}
}

func estimateTextSize(messages []common.Message) int {
	size := 0
	for _, msg := range messages {
		for _, block := range msg.Content {
			size += len(block.Text)
		}
	}
	return size
}
