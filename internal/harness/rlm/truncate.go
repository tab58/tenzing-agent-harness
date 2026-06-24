package rlm

import "fmt"

// Truncate clips text to maxChars (measured in runes), preserving the first half
// and last half with a size annotation in between. If the text fits, it is returned as-is.
func Truncate(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	half := maxChars / 2
	return string(runes[:half]) +
		fmt.Sprintf("\n... [truncated, %d total chars] ...\n", len(runes)) +
		string(runes[len(runes)-half:])
}
