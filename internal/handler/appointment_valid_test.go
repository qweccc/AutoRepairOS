package handler

import "testing"

func TestValidAppointmentTime(t *testing.T) {
	if !validAppointmentTime("") {
		t.Error("empty should be valid")
	}
	if !validAppointmentTime("14:30") {
		t.Error("14:30 should be valid")
	}
	if validAppointmentTime("25:00") {
		t.Error("25:00 should be invalid")
	}
	if validAppointmentTime("abc") {
		t.Error("abc should be invalid")
	}
}

func TestValidAppointmentDate(t *testing.T) {
	if !validAppointmentDate("2026-01-01") {
		t.Error("valid date should pass")
	}
	if validAppointmentDate("invalid") {
		t.Error("invalid should fail")
	}
	if validAppointmentDate("2026-13-01") {
		t.Error("invalid month should fail")
	}
}
