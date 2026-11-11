package handler

import (
	"context"
	"testing"

	"autorepairos/internal/model"
)

func TestStatusControls(t *testing.T) {
	got := statusControls(context.Background(), model.WorkOrder{}, false)
	if got == "" {
		t.Error("should not be empty")
	}
	got2 := statusControls(context.Background(), model.WorkOrder{}, true)
	if got2 == "" {
		t.Error("should not be empty")
	}
}
