package handler

import "testing"

func TestSafeSpreadsheetText(t *testing.T) {
	if safeSpreadsheetText("=SUM(A1)") != "'=SUM(A1)" {
		t.Errorf("got %q", safeSpreadsheetText("=SUM(A1)"))
	}
	if safeSpreadsheetText("hello") != "hello" {
		t.Errorf("got %q", safeSpreadsheetText("hello"))
	}
}
