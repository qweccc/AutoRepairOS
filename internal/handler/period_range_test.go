package handler

import (
	"autorepairos/internal/model"
	"testing"
)

func TestPeriodRange(t *testing.T) {
	f, t2 := periodRange("day", "", "")
	if f == "" || t2 == "" {
		t.Error("should not be empty")
	}
}

func TestOwnerOnlyStat(t *testing.T) {
	got := ownerOnlyStat(model.User{Role: "owner"}, "100", "label")
	if got == "" {
		t.Error("should not be empty for counter")
	}
}
