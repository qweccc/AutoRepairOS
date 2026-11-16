package handler

import (
	"context"
	"testing"
)

func TestPrintToolbar(t *testing.T) {
	got := printToolbar(context.Background(), "<a>link</a>")
	if got == "" {
		t.Error("should not be empty")
	}
}
