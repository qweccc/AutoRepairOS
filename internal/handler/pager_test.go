package handler

import (
	"net/url"
	"testing"
)

func TestPager(t *testing.T) {
	if pager("/x", nil, 5, 1, 10) != "" {
		t.Error("should be empty")
	}
	q := url.Values{}
	got := pager("/x", q, 25, 1, 10)
	if got == "" {
		t.Error("should have pager")
	}
}
