package core

import "testing"

func TestToolResultZeroValue(t *testing.T) {
	var r ToolResult
	if r.IsError {
		t.Fatal("zero ToolResult must not be an error")
	}
}
