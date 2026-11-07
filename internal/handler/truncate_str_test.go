package handler

import "testing"

func TestTruncateStr(t *testing.T) {
	if truncateStr("hello", 10) != "hello" {
		t.Errorf("got %q", truncateStr("hello", 10))
	}
	if truncateStr("hello world", 5) != "hello..." {
		t.Errorf("got %q", truncateStr("hello world", 5))
	}
}
