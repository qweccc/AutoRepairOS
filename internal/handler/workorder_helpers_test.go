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

func TestPartDisplayLabel(t *testing.T) {
	if got := partDisplayLabel("P001", "煞車皮"); got != "煞車皮 (P001)" {
		t.Errorf("got %s", got)
	}
	if got := partDisplayLabel("P001", "P001"); got != "P001" {
		t.Errorf("got %s", got)
	}
	if got := partDisplayLabel("", "煞車皮"); got != "煞車皮" {
		t.Errorf("got %s", got)
	}
	if got := partDisplayLabel("P001", ""); got != "P001" {
		t.Errorf("got %s", got)
	}
}

func TestParseClaimAmount(t *testing.T) {
	v, err := parseClaimAmount("")
	if err != nil || v != 0 {
		t.Errorf("empty: %v %v", v, err)
	}
	v, err = parseClaimAmount("123.45")
	if err != nil || v != 123.45 {
		t.Errorf("valid: %v %v", v, err)
	}
	_, err = parseClaimAmount("abc")
	if err == nil {
		t.Error("invalid should error")
	}
}

func TestOrDash(t *testing.T) {
	if orDash("") != "-" {
		t.Error("empty should be dash")
	}
	if orDash("  ") != "-" {
		t.Error("whitespace should be dash")
	}
	if orDash("abc") != "abc" {
		t.Error("non-empty should pass through")
	}
}
