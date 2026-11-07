package handler

import "testing"

func TestSelIf(t *testing.T) {
	if got := selIf(true); got != " selected" {
		t.Errorf("got %q", got)
	}
	if got := selIf(false); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestHtmlEsc(t *testing.T) {
	if got := htmlEsc("<script>"); got != "&lt;script&gt;" {
		t.Errorf("got %s", got)
	}
}
