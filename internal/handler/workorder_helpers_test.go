package handler

import (
	"testing"
)

func TestTrimQty(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{1.0, "1"},
		{2.5, "2.5"},
		{0.0, "0"},
		{100.0, "100"},
	}
	for _, tt := range tests {
		got := trimQty(tt.input)
		if got != tt.expected {
			t.Errorf("trimQty(%v) = %s, want %s", tt.input, got, tt.expected)
		}
	}
}

func TestBtnIf(t *testing.T) {
	if btnIf(true, "a", "b") != "a" {
		t.Error("btnIf(true) should return first arg")
	}
	if btnIf(false, "a", "b") != "b" {
		t.Error("btnIf(false) should return second arg")
	}
}

func TestYesNo(t *testing.T) {
	if yesNo(true) == "" {
		t.Error("yesNo(true) should not be empty")
	}
	if yesNo(false) == "" {
		t.Error("yesNo(false) should not be empty")
	}
}

func TestHiddenStatus(t *testing.T) {
	if hiddenStatus("") != "" {
		t.Errorf("hiddenStatus(empty) should be empty")
	}
	if hiddenStatus("paid") == "" {
		t.Errorf("hiddenStatus(paid) should not be empty")
	}
}

