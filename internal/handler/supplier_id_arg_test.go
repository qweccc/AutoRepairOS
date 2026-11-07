package handler

import "testing"

func TestSupplierIDArg(t *testing.T) {
	if supplierIDArg("0") != nil {
		t.Error("zero should return nil")
	}
	if v, ok := supplierIDArg("5").(int64); !ok || v != 5 {
		t.Errorf("got %v", supplierIDArg("5"))
	}
}
