package handler

import "testing"

func TestChkIf(t *testing.T) {
	if got := chkIf(true); got != " checked" {
		t.Errorf("got %q", got)
	}
	if got := chkIf(false); got != "" {
		t.Errorf("got %q", got)
	}
}
