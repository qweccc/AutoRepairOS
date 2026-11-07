package handler

import "testing"

func TestSubcontractStatusArg(t *testing.T) {
	if subcontractStatusArg("completed") != "completed" {
		t.Error("completed")
	}
	if subcontractStatusArg("invalid") != "pending" {
		t.Error("invalid should default to pending")
	}
}
