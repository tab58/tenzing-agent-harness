package rlm

import (
	"strings"
	"testing"
)

func TestTruncateFits(t *testing.T) {
	result := Truncate("hello world", 100)
	if result != "hello world" {
		t.Fatalf("got %q, want %q", result, "hello world")
	}
}

func TestTruncateExactFit(t *testing.T) {
	text := "abcde"
	result := Truncate(text, 5)
	if result != text {
		t.Fatalf("got %q, want %q", result, text)
	}
}

func TestTruncateClips(t *testing.T) {
	text := strings.Repeat("x", 100)
	result := Truncate(text, 20)

	if len(result) >= len(text) {
		t.Fatal("result should be shorter than original")
	}
	if !strings.Contains(result, "[truncated, 100 total chars]") {
		t.Fatalf("missing truncation annotation, got: %s", result)
	}
	if !strings.HasPrefix(result, "xxxxxxxxxx") {
		t.Fatal("should start with first half")
	}
	if !strings.HasSuffix(result, "xxxxxxxxxx") {
		t.Fatal("should end with last half")
	}
}

func TestTruncateUnicode(t *testing.T) {
	text := strings.Repeat("日本語", 20) // 60 runes
	result := Truncate(text, 10)

	if !strings.Contains(result, "[truncated, 60 total chars]") {
		t.Fatalf("missing truncation annotation, got: %s", result)
	}
	for _, r := range result {
		if r == 0xFFFD {
			t.Fatal("truncation split a multi-byte character")
		}
	}
}

func TestTruncateEmpty(t *testing.T) {
	result := Truncate("", 100)
	if result != "" {
		t.Fatalf("got %q, want empty", result)
	}
}
