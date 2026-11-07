package handler

import "testing"

func TestGenerateSecureToken(t *testing.T) {
	token, err := generateSecureToken()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(token))
	}
}
