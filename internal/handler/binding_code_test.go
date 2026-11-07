package handler

import "testing"

func TestGenerateBindingCode(t *testing.T) {
	code, err := generateBindingCode()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("expected 6 digits, got %s", code)
	}
}
