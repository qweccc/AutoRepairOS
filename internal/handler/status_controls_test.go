package handler

import (
	"autorepairos/internal/model"
	"testing"
)

func TestStatusControls(t *testing.T) {
	got := statusControls(model.WorkOrder{}, false)
	if got == "" {
		t.Error("should not be empty")
	}
	got2 := statusControls(model.WorkOrder{}, true)
	if got2 == "" {
		t.Error("should not be empty")
	}
}
