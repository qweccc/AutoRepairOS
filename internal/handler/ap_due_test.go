package handler

import "testing"

func TestApDueDate(t *testing.T) {
	got, err := apDueDate("monthly", "2026-01-15")
	if err != nil || got == "" {
		t.Errorf("got %s err %v", got, err)
	}
	got, err = apDueDate("check30", "2026-01-15")
	if err != nil || got == "" {
		t.Errorf("got %s err %v", got, err)
	}
	_, err = apDueDate("monthly", "invalid")
	if err == nil {
		t.Error("should error")
	}
}
