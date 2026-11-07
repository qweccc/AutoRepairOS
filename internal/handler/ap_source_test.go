package handler

import "testing"

func TestIsAPPayableSource(t *testing.T) {
	if !isAPPayableSource("purchase_receive") {
		t.Error("purchase_receive should be true")
	}
	if !isAPPayableSource("subcontract") {
		t.Error("subcontract should be true")
	}
	if isAPPayableSource("other") {
		t.Error("other should be false")
	}
}

func TestIsSettledStatus(t *testing.T) {
	if !isSettledStatus("paid") {
		t.Error("paid should pass")
	}
	if !isSettledStatus("void") {
		t.Error("void should pass")
	}
	if isSettledStatus("pending") {
		t.Error("pending should fail")
	}
}
