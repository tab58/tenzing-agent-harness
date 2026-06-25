package compressor

import (
	"strings"
	"testing"
)

func TestClassifyOverflow(t *testing.T) {
	const threshold = 30_000

	tests := []struct {
		name      string
		history   int
		charsPer  int
		inputs    []string
		wantCause OverflowCause
		wantIdx   int
	}{
		{
			name:      "below threshold",
			history:   5,
			charsPer:  100,
			inputs:    []string{"hello"},
			wantCause: OverflowNone,
			wantIdx:   -1,
		},
		{
			name:      "large input only",
			history:   5,
			charsPer:  100,
			inputs:    []string{strings.Repeat("x", 20_000)},
			wantCause: OverflowLargeInput,
			wantIdx:   0,
		},
		{
			name:      "history overflow only",
			history:   20,
			charsPer:  2000,
			inputs:    []string{"hello"},
			wantCause: OverflowHistory,
			wantIdx:   -1,
		},
		{
			name:      "both",
			history:   20,
			charsPer:  2000,
			inputs:    []string{strings.Repeat("x", 20_000)},
			wantCause: OverflowBoth,
			wantIdx:   0,
		},
		{
			name:      "multiple inputs one large",
			history:   5,
			charsPer:  100,
			inputs:    []string{"small", strings.Repeat("x", 20_000), "small"},
			wantCause: OverflowLargeInput,
			wantIdx:   1,
		},
		{
			name:      "at threshold exactly",
			history:   5,
			charsPer:  100,
			inputs:    []string{strings.Repeat("x", threshold/2)},
			wantCause: OverflowNone,
			wantIdx:   -1,
		},
		{
			name:      "empty history large input",
			history:   0,
			charsPer:  0,
			inputs:    []string{strings.Repeat("x", 20_000)},
			wantCause: OverflowLargeInput,
			wantIdx:   0,
		},
		{
			name:      "empty inputs history overflow",
			history:   20,
			charsPer:  2000,
			inputs:    nil,
			wantCause: OverflowHistory,
			wantIdx:   -1,
		},
		{
			name:      "multiple large inputs picks largest",
			history:   5,
			charsPer:  100,
			inputs:    []string{strings.Repeat("x", 16_000), strings.Repeat("x", 20_000)},
			wantCause: OverflowLargeInput,
			wantIdx:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history := makeMessages(tt.history, tt.charsPer)
			cause, idx := ClassifyOverflow(history, tt.inputs, threshold)
			if cause != tt.wantCause {
				t.Errorf("cause = %d, want %d", cause, tt.wantCause)
			}
			if idx != tt.wantIdx {
				t.Errorf("idx = %d, want %d", idx, tt.wantIdx)
			}
		})
	}
}
