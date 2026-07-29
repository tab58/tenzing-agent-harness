package common

import (
	"strings"
)

// combinedText concatenates all text blocks, ignoring tool blocks.
func CombinedText(blocks []ContentBlock) string {
	var text strings.Builder
	for _, block := range blocks {
		if block.Type == ContentTypeText {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}
