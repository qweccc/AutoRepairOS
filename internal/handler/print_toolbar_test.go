package handler

import "testing"

func TestPrintToolbar(t *testing.T) {
	got := printToolbar("<a>link</a>")
	if got == "" {
		t.Error("should not be empty")
	}
}
