package handler

import "testing"

func TestWithCSRFHTMXActions(t *testing.T) {
	got := withCSRFHTMXActions(`<a href="/foo">link</a>`)
	if got == "" {
		t.Error("should not be empty")
	}
}
