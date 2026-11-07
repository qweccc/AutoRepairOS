package handler

import "testing"

func TestWarrantyCostBlock(t *testing.T) {
	if warrantyCostBlock(false, 0, 0, false) != "" {
		t.Error("should be empty when !showCost")
	}
	got := warrantyCostBlock(true, 100, 50, true)
	if got == "" {
		t.Error("should not be empty")
	}
}
