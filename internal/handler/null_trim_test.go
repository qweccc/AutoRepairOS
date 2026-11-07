package handler

import "testing"

func TestNullDate(t *testing.T) {
	if got := nullDate(""); got != "" {
		t.Errorf("got %s", got)
	}
	if got := nullDate("2026-01-01"); got != "2026-01-01" {
		t.Errorf("got %s", got)
	}
	if got := nullDate("invalid"); got != "" {
		t.Errorf("got %s", got)
	}
}

func TestNullStr(t *testing.T) {
	if got := nullStr("  hello  "); got != "hello" {
		t.Errorf("got %s", got)
	}
}

func TestMoney(t *testing.T) {
	if got := money(123.456); got != "123" {
		t.Errorf("got %s", got)
	}
}

func TestTrimNum(t *testing.T) {
	if got := trimNum(1.50); got != "1.5" {
		t.Errorf("got %s", got)
	}
	if got := trimNum(2.00); got != "2" {
		t.Errorf("got %s", got)
	}
	if got := trimNum(0.0); got != "0" {
		t.Errorf("got %s", got)
	}
}
