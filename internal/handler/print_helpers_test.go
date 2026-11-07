package handler

import "testing"

func TestComputeTaxBreakdown(t *testing.T) {
	got := computeTaxBreakdown(0)
	if got.Total != 0 {
		t.Errorf("got %v", got)
	}
	got = computeTaxBreakdown(105)
	if got.Total != 105 {
		t.Errorf("got %v", got)
	}
	if got.Taxable <= 0 {
		t.Errorf("got %v", got)
	}
}
