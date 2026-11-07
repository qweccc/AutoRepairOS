package handler

import "testing"

func TestPaymentMismatch(t *testing.T) {
	if paymentMismatch(100, 100) {
		t.Error("equal should not mismatch")
	}
	if !paymentMismatch(100, 90) {
		t.Error("should mismatch")
	}
	if paymentMismatch(0, 100) {
		t.Error("zero sum should not mismatch")
	}
}
