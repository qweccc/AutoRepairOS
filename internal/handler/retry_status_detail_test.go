package handler

import (
	"autorepairos/internal/cron"
	"testing"
)

func TestRetryStatusDetail(t *testing.T) {
	n := cron.Notification{}
	if retryStatusDetail(n) != "" {
		t.Error("empty should return empty")
	}
	n2 := cron.Notification{RetryCount: 2}
	if retryStatusDetail(n2) == "" {
		t.Error("should return html")
	}
}
