package handler

import "testing"

func TestStatusOptions(t *testing.T) {
	got := statusOptions("pending")
	if got == "" {
		t.Error("should not be empty")
	}
}
