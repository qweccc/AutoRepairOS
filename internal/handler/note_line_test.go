package handler

import "testing"

func TestNoteLine(t *testing.T) {
	if noteLine("  ") != "" {
		t.Error("empty should return empty")
	}
	if noteLine("<script>") == "" {
		t.Error("should return html")
	}
}
