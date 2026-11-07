package handler

import (
	"testing"
	"time"
)

func TestWoCreatedText(t *testing.T) {
	if woCreatedText(time.Time{}) != "-" {
		t.Errorf("got %q", woCreatedText(time.Time{}))
	}
	tm := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	if woCreatedText(tm) != "2026-01-15" {
		t.Errorf("got %q", woCreatedText(tm))
	}
}
