package handler

import "testing"

func TestNormalizeCalendarDate(t *testing.T) {
	if got := normalizeCalendarDate("2026-01-01"); got != "2026-01-01" {
		t.Errorf("got %s", got)
	}
	if got := normalizeCalendarDate("invalid"); got != "" {
		t.Errorf("got %s", got)
	}
	if got := normalizeCalendarDate("  2026-01-01  "); got != "2026-01-01" {
		t.Errorf("got %s", got)
	}
}

func TestNormalizeCalendarView(t *testing.T) {
	if got := normalizeCalendarView("week"); got != "week" {
		t.Errorf("got %s", got)
	}
	if got := normalizeCalendarView("day"); got != "day" {
		t.Errorf("got %s", got)
	}
	if got := normalizeCalendarView("foo"); got != "day" {
		t.Errorf("got %s", got)
	}
}

func TestNormTags(t *testing.T) {
	got := normTags("a, b, a")
	if got == "" {
		t.Error("should not be empty")
	}
	got = normTags("")
	if got != "" {
		t.Errorf("empty should be empty, got %s", got)
	}
}
