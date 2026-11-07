package handler

import "testing"

func TestIntOrBlank(t *testing.T) {
	if got := intOrBlank(0); got != "" {
		t.Errorf("got %s", got)
	}
	if got := intOrBlank(5); got != "5" {
		t.Errorf("got %s", got)
	}
}

func TestIntOrEmpty(t *testing.T) {
	if got := intOrEmpty(0); got != "" {
		t.Errorf("got %s", got)
	}
	if got := intOrEmpty(5); got != "5" {
		t.Errorf("got %s", got)
	}
}
