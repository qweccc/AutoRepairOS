package handler

import "testing"

func TestMustInt(t *testing.T) {
	if got := mustInt("42"); got != 42 {
		t.Errorf("got %d", got)
	}
	if got := mustInt("abc"); got != 0 {
		t.Errorf("got %d", got)
	}
}

func TestMetaPair(t *testing.T) {
	got := metaPair("a", "b")
	if got == "" {
		t.Error("should not be empty")
	}
}
