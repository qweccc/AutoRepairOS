package handler

import (
	"context"
	"testing"
)

func TestOptNoteBlock(t *testing.T) {
	if optNoteBlock(context.Background(), "  ") != "" {
		t.Error("empty should return empty")
	}
	if optNoteBlock(context.Background(), "hello") == "" {
		t.Error("should return html")
	}
}
