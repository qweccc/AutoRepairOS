package handler

import "testing"

func TestItemTypeLabelShort(t *testing.T) {
	if got := itemTypeLabelShort("labor"); got != "工資" {
		t.Errorf("got %s", got)
	}
	if got := itemTypeLabelShort("part"); got != "零件" {
		t.Errorf("got %s", got)
	}
	if got := itemTypeLabelShort("subcontract"); got != "外包" {
		t.Errorf("got %s", got)
	}
	if got := itemTypeLabelShort("unknown"); got != "unknown" {
		t.Errorf("got %s", got)
	}
}

func TestPaymentTermsLabel(t *testing.T) {
	// Just ensure no panic
	_ = paymentTermsLabel("")
	_ = paymentTermsLabel("nonexistent")
}
