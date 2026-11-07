package handler

import "testing"

func TestSignaturePrefix(t *testing.T) {
	if signaturePrefix("signature=abc") != "abc" {
		t.Errorf("got %q", signaturePrefix("signature=abc"))
	}
	if signaturePrefix("signature=123456789012345") != "123456789012..." {
		t.Errorf("got %q", signaturePrefix("signature=123456789012345"))
	}
}
