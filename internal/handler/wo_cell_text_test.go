package handler

import "testing"

func TestWoCellText(t *testing.T) {
	if woCellText(0) != "-" {
		t.Errorf("got %q", woCellText(0))
	}
	if woCellText(42) != "#42" {
		t.Errorf("got %q", woCellText(42))
	}
}
