package handler

import "testing"

func TestApprovalOptions(t *testing.T) {
	got := approvalOptions("pending")
	if got == "" {
		t.Error("should not be empty")
	}
}
