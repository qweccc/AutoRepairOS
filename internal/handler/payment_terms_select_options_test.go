package handler

import "testing"

func TestPaymentTermsSelectOptions(t *testing.T) {
	opts := paymentTermsSelectOptions("")
	if len(opts) == 0 {
		t.Error("should have options")
	}
}
