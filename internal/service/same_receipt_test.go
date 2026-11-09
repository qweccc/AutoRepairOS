package service

import "testing"

func TestSameReceipt(t *testing.T) {
	a := ReceiptInput{VoucherNo: "V1", CustomerID: 1, PayTotal: 100}
	if !sameReceipt(a, a) {
		t.Fatal("identical receipts should be equal")
	}
	b := a
	b.PayTotal = 200
	if sameReceipt(a, b) {
		t.Fatal("different receipts should not be equal")
	}
}