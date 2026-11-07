package handler

import "testing"

func TestPrintHead(t *testing.T) {
	got := printHead("title", []string{"r1"})
	if got == "" {
		t.Error("should not be empty")
	}
}
