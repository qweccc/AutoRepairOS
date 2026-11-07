package handler

import (
	"testing"
	"time"
)

func TestParseFlexTime(t *testing.T) {
	if !parseFlexTime("").IsZero() {
		t.Error("empty should be zero")
	}
	got := parseFlexTime("2026-01-02")
	if got.Year() != 2026 {
		t.Errorf("got %v", got)
	}
	_ = time.Now()
}
