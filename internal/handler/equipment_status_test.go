package handler

import "testing"

func TestEquipmentStatusArg(t *testing.T) {
	if got := equipmentStatusArg("active"); got != "active" {
		t.Errorf("got %s", got)
	}
	if got := equipmentStatusArg("maintenance"); got != "maintenance" {
		t.Errorf("got %s", got)
	}
	if got := equipmentStatusArg("retired"); got != "retired" {
		t.Errorf("got %s", got)
	}
	if got := equipmentStatusArg("invalid"); got != "active" {
		t.Errorf("got %s", got)
	}
}

func TestErrLine(t *testing.T) {
	if got := errLine(""); got != "" {
		t.Errorf("got %s", got)
	}
	if got := errLine("test"); got == "" {
		t.Error("should not be empty")
	}
}
