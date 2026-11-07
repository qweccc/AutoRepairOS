package handler

import "testing"

func TestParseApprovalItems(t *testing.T) {
	_, err := parseApprovalItems("")
	if err == nil {
		t.Error("empty should error")
	}
	_, err = parseApprovalItems("invalid json")
	if err == nil {
		t.Error("invalid json should error")
	}
	items, err := parseApprovalItems(`[{"description":"test","quantity":1,"item_type":"labor"}]`)
	if err != nil {
		t.Errorf("valid json should not error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}
