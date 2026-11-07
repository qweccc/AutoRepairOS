package handler

import (
	"net/url"
	"testing"
)

func TestWoTabURL(t *testing.T) {
	qv := url.Values{"q": {"brake"}}
	got := woTabURL(qv, "done")
	if got != "/work-orders?q=brake&status=done" {
		t.Errorf("got %q", got)
	}
	if woTabURL(url.Values{}, "") != "/work-orders" {
		t.Errorf("got %q", woTabURL(url.Values{}, ""))
	}
}
