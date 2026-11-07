package handler

import "testing"

func TestWasPrompted(t *testing.T) {
	markPrompted(12345)
	if !wasPrompted(12345) {
		t.Error("should be prompted")
	}
	if wasPrompted(99999) {
		t.Error("should not be prompted")
	}
}
