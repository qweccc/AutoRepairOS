package handler

import (
	"context"
	"testing"
)

func TestStatusOptions(t *testing.T) {
	got := statusOptions(context.Background(), "pending")
	if got == "" {
		t.Error("should not be empty")
	}
}
