package handler

import "testing"

func TestOptNoteBlock(t *testing.T) {
	if optNoteBlock("  ") != "" {
		t.Error("empty should return empty")
	}
	if optNoteBlock("hello") == "" {
		t.Error("should return html")
	}
}
