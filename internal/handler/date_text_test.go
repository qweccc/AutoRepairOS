package handler

import "testing"

func TestDateText(t *testing.T) {
	if got := dateText(""); got != "-" {
		t.Errorf("got %s", got)
	}
	if got := dateText("2026-01-01"); got != "2026-01-01" {
		t.Errorf("got %s", got)
	}
}

func TestInsuranceExpiryDate(t *testing.T) {
	got, err := insuranceExpiryDate("")
	if err != nil || got != "" {
		t.Errorf("got %s err %v", got, err)
	}
	got, err = insuranceExpiryDate("2026-01-01")
	if err != nil || got != "2026-01-01" {
		t.Errorf("got %s err %v", got, err)
	}
	_, err = insuranceExpiryDate("invalid")
	if err == nil {
		t.Error("should error")
	}
}
