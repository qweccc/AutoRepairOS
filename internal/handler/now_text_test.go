package handler

import (
	"testing"
	"time"
)

func TestNowText(t *testing.T) {
	got := nowText()
	if len(got) != 19 {
		t.Errorf("nowText() length = %d, want 19", len(got))
	}
	// verify parseable
	if _, err := time.Parse("2006-01-02 15:04:05", got); err != nil {
		t.Errorf("nowText() not parseable: %v", err)
	}
}
