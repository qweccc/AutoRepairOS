package handler

import (
	"net/url"
	"testing"
)

func TestHeadStr(t *testing.T) {
	if got := headStr([]byte("hello"), 10); got != "hello" {
		t.Errorf("got %s", got)
	}
	if got := headStr([]byte("hello world"), 5); got != "hello..." {
		t.Errorf("got %s", got)
	}
}

func TestHiddenInputs(t *testing.T) {
	q := url.Values{}
	q.Set("a", "1")
	q.Set("b", "2")
	got := hiddenInputs(q, "b")
	if got == "" {
		t.Error("should not be empty")
	}
}
