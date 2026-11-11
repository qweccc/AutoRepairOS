package handler

import (
	"context"
	"testing"
)

func TestApprovalOptions(t *testing.T) {
	got := approvalOptions(context.Background(), "pending")
	if got == "" {
		t.Error("should not be empty")
	}
}
