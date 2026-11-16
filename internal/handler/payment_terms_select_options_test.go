package handler

import (
	"context"
	"testing"
)

func TestPaymentTermsSelectOptions(t *testing.T) {
	opts := paymentTermsSelectOptions(context.Background(), "")
	if len(opts) == 0 {
		t.Error("should have options")
	}
}
