package handler

import "testing"

func TestOptionalPrintValue(t *testing.T) {
	if optionalPrintValue("  ") != "" {
		t.Error("empty should return empty")
	}
	if optionalPrintValue("-") != "" {
		t.Error("dash should return empty")
	}
	if optionalPrintValue("  hello  ") != "hello" {
		t.Errorf("got %q", optionalPrintValue("  hello  "))
	}
}
