package handler

import "testing"

func TestTagPills(t *testing.T) {
	got := tagPills("")
	if got == "" {
		t.Error("should not be empty")
	}
	got = tagPills("a,b")
	if got == "" {
		t.Error("should not be empty")
	}
}
