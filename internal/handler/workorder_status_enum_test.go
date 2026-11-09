package handler

import "testing"

func TestWorkOrderStatusIsMutable(t *testing.T) {
	if WorkOrderStatusPaid.IsMutable() {
		t.Fatal("paid should not be mutable")
	}
	if !WorkOrderStatusPending.IsMutable() {
		t.Fatal("pending should be mutable")
	}
}
