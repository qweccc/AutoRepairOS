package handler

import "testing"

func TestWelcomeMessage(t *testing.T) {
	if welcomeMessage() == "" {
		t.Error("should not be empty")
	}
}
