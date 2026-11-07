package handler

import "testing"

func TestPhoneMatchSQL(t *testing.T) {
	got := phoneMatchSQL("phone")
	if got == "" {
		t.Error("should not be empty")
	}
}
