package handler

import "testing"

func TestParseItemsJSON(t *testing.T) {
	if parseItemsJSON("") != nil {
		t.Error("empty should return nil")
	}
	if parseItemsJSON("invalid") != nil {
		t.Error("invalid should return nil")
	}
	got := parseItemsJSON(`[{"description":"test","quantity":1}]`)
	if len(got) != 1 {
		t.Errorf("expected 1, got %d", len(got))
	}
}
