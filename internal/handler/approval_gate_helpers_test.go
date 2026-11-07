package handler

import "testing"

func TestIsCompletionOrPayment(t *testing.T) {
	if !isCompletionOrPayment("completed") {
		t.Error("completed should pass")
	}
	if !isCompletionOrPayment("paid") {
		t.Error("paid should pass")
	}
	if isCompletionOrPayment("pending") {
		t.Error("pending should fail")
	}
}
