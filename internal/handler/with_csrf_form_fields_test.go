package handler

import "testing"

func TestWithCSRFFormFields(t *testing.T) {
	got := withCSRFFormFields("<form></form>", "token123")
	if got == "" {
		t.Error("should not be empty")
	}
	if withCSRFFormFields("<form></form>", "") != "<form></form>" {
		t.Error("empty token should return body unchanged")
	}
}
