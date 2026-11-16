package handler

import (
	"context"
	"net/url"
	"testing"
)

func TestPerPageSel(t *testing.T) {
	got := perPageSel(context.Background(), "/x", nil, 10)
	if got == "" {
		t.Error("should not be empty")
	}
	q := url.Values{}
	q.Set("page", "2")
	got2 := perPageSel(context.Background(), "/x", q, 10)
	if got2 == "" {
		t.Error("should not be empty")
	}
}
