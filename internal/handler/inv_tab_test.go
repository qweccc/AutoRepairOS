package handler

import (
	"net/url"
	"testing"
)

func TestInvTabURL(t *testing.T) {
	qv := url.Values{}
	qv.Set("q", "test")
	got := invTabURL(qv, "cat", true)
	if got == "" {
		t.Error("should not be empty")
	}
	got = invTabURL(url.Values{}, "", false)
	if got != "/inventory" {
		t.Errorf("got %s", got)
	}
}
