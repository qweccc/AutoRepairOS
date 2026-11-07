package handler

import "testing"

func TestIsSupplierType(t *testing.T) {
	if !isSupplierType("material") {
		t.Error("material should pass")
	}
	if !isSupplierType("vendor") {
		t.Error("vendor should pass")
	}
	if isSupplierType("other") {
		t.Error("other should fail")
	}
}
