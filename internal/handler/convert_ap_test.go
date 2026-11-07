package handler

import "testing"

func TestConvertAPSuppliers(t *testing.T) {
	got := convertAPSuppliers(nil)
	if len(got) != 0 {
		t.Errorf("got %d", len(got))
	}
	got = convertAPSuppliers([]apSupplierOption{{ID: 1, Name: "test"}})
	if len(got) != 1 {
		t.Errorf("got %d", len(got))
	}
}
