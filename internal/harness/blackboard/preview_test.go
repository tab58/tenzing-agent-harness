package blackboard

import (
	"strings"
	"testing"
)

func TestNewPreview(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantHead  string
		wantTail  string
		wantChars int
		wantLines int
	}{
		{
			name:      "short value fits entirely in head",
			value:     "hello\nworld",
			wantHead:  "hello\nworld",
			wantTail:  "",
			wantChars: 11,
			wantLines: 2,
		},
		{
			name:      "boundary value exactly head+tail is not split",
			value:     strings.Repeat("x", 20),
			wantHead:  strings.Repeat("x", 20),
			wantTail:  "",
			wantChars: 20,
			wantLines: 1,
		},
		{
			name:      "long value is split into head and tail",
			value:     strings.Repeat("a", 15) + strings.Repeat("b", 15),
			wantHead:  strings.Repeat("a", 15),
			wantTail:  strings.Repeat("b", 5),
			wantChars: 30,
			wantLines: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPreview("a1", "result", tt.value, 15, 5)
			if p.Head != tt.wantHead {
				t.Errorf("Head = %q, want %q", p.Head, tt.wantHead)
			}
			if p.Tail != tt.wantTail {
				t.Errorf("Tail = %q, want %q", p.Tail, tt.wantTail)
			}
			if p.Chars != tt.wantChars {
				t.Errorf("Chars = %d, want %d", p.Chars, tt.wantChars)
			}
			if p.Lines != tt.wantLines {
				t.Errorf("Lines = %d, want %d", p.Lines, tt.wantLines)
			}
		})
	}
}

func TestPreviewString(t *testing.T) {
	long := strings.Repeat("a", 3000) + strings.Repeat("z", 1000)
	p := NewPreview("a7", "result", long, DefaultHeadChars, DefaultTailChars)
	s := p.String()

	for _, want := range []string{
		`bb["a7"]["result"]`,
		"4000 chars",
		"--- head ---",
		"--- tail ---",
		"peek(",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Preview.String() missing %q\ngot: %s", want, s)
		}
	}
	if len(s) > DefaultHeadChars+DefaultTailChars+500 {
		t.Errorf("Preview.String() too long: %d chars", len(s))
	}
}

func TestPreviewStringShortValueIsVerbatim(t *testing.T) {
	p := NewPreview("a1", "result", "short answer", DefaultHeadChars, DefaultTailChars)
	if got := p.String(); got != "short answer" {
		t.Errorf("String() = %q, want verbatim value", got)
	}
}

func TestTruncateOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		head int
		tail int
		want func(t *testing.T, got string)
	}{
		{
			name: "short output unchanged",
			in:   "hello",
			head: 10, tail: 5,
			want: func(t *testing.T, got string) {
				if got != "hello" {
					t.Errorf("got %q, want %q", got, "hello")
				}
			},
		},
		{
			name: "long output keeps head and tail with omission marker",
			in:   strings.Repeat("a", 50) + strings.Repeat("z", 50),
			head: 10, tail: 10,
			want: func(t *testing.T, got string) {
				if !strings.HasPrefix(got, strings.Repeat("a", 10)) {
					t.Errorf("missing head: %q", got)
				}
				if !strings.HasSuffix(got, strings.Repeat("z", 10)) {
					t.Errorf("missing tail: %q", got)
				}
				if !strings.Contains(got, "80 chars omitted") {
					t.Errorf("missing omission marker: %q", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.want(t, truncateOutput(tt.in, tt.head, tt.tail))
		})
	}
}

func TestNewPreviewCountsRunesNotBytes(t *testing.T) {
	// "✓" is 3 bytes, "é" is 2 — Chars must match python's len() (code
	// points) so the preview agrees with what agents measure in the REPL.
	value := "check ✓ café"
	p := NewPreview("a1", "result", value, DefaultHeadChars, DefaultTailChars)
	if want := len([]rune(value)); p.Chars != want {
		t.Errorf("Chars = %d, want %d (rune count; byte count is %d)", p.Chars, want, len(value))
	}
}

func TestTruncateOutputMarkerCountsRunes(t *testing.T) {
	// 60 "é" (2 bytes each) = 120 bytes; head/tail cut 10 bytes off each
	// end, omitting 100 bytes = 50 code points. Marker must say 50.
	s := strings.Repeat("é", 60)
	got := truncateOutput(s, 10, 10)
	if !strings.Contains(got, "50 chars omitted") {
		t.Errorf("marker should count runes (want %q in %q)", "50 chars omitted", got)
	}
}
