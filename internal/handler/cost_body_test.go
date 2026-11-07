package handler

import "testing"

func TestCostInput(t *testing.T) {
	got := costInput("100")
	if got == "" {
		t.Error("should not be empty")
	}
}

func TestBodyFingerprint(t *testing.T) {
	if got := bodyFingerprint(nil); got != "空" {
		t.Errorf("got %s", got)
	}
	if got := bodyFingerprint([]byte(`{"a":1}`)); got == "" {
		t.Error("should not be empty")
	}
}
