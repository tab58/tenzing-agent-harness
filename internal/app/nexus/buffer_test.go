package nexus

import (
	"fmt"
	"testing"
)

func TestRingAppendAndLast(t *testing.T) {
	tests := []struct {
		name       string
		size       int
		appends    int // appends "line-0".."line-N-1"; every 3rd is an error
		lastN      int
		errorsOnly bool
		wantTexts  []string
	}{
		{"under capacity", 10, 5, 3, false, []string{"line-2", "line-3", "line-4"}},
		{"exact wraparound", 3, 5, 3, false, []string{"line-2", "line-3", "line-4"}},
		{"n larger than count", 10, 2, 5, false, []string{"line-0", "line-1"}},
		{"errors only", 10, 7, 10, true, []string{"line-0", "line-3", "line-6"}},
		{"errors only after wrap", 3, 7, 10, true, []string{"line-6"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRing(tt.size)
			for i := 0; i < tt.appends; i++ {
				r.Append(fmt.Sprintf("line-%d", i), i%3 == 0)
			}
			got := r.Last(tt.lastN, tt.errorsOnly)
			if len(got) != len(tt.wantTexts) {
				t.Fatalf("Last() returned %d entries, want %d", len(got), len(tt.wantTexts))
			}
			for i, e := range got {
				if e.Text != tt.wantTexts[i] {
					t.Errorf("entry %d = %q, want %q", i, e.Text, tt.wantTexts[i])
				}
			}
		})
	}
}

func TestRingSeqMonotonic(t *testing.T) {
	r := NewRing(2)
	for i := 0; i < 5; i++ {
		e := r.Append("x", false)
		if e.Seq != uint64(i) {
			t.Fatalf("Seq = %d, want %d", e.Seq, i)
		}
	}
}

func TestRingCounts(t *testing.T) {
	r := NewRing(3)
	// 5 appends into size 3; errors at 0 and 3; entry 0 evicted → 1 error left
	for i := 0; i < 5; i++ {
		r.Append("x", i%3 == 0)
	}
	total, errs := r.Counts()
	if total != 3 || errs != 1 {
		t.Errorf("Counts() = (%d, %d), want (3, 1)", total, errs)
	}
}

func TestRingSnapshot(t *testing.T) {
	r := NewRing(3)
	for i := 0; i < 4; i++ {
		r.Append(fmt.Sprintf("line-%d", i), false)
	}
	got := r.Snapshot()
	want := []string{"line-1", "line-2", "line-3"}
	if len(got) != len(want) {
		t.Fatalf("Snapshot() len = %d, want %d", len(got), len(want))
	}
	for i, e := range got {
		if e.Text != want[i] {
			t.Errorf("entry %d = %q, want %q", i, e.Text, want[i])
		}
	}
}
