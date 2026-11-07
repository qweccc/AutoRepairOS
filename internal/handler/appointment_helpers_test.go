package handler

import "testing"

func TestAppointmentLabel(t *testing.T) {
	if got := appointmentLabel("booked"); got != "已預約" {
		t.Errorf("got %s", got)
	}
	if got := appointmentLabel("unknown"); got != "unknown" {
		t.Errorf("got %s", got)
	}
}

func TestAppointmentPill(t *testing.T) {
	if got := appointmentPill("completed"); got != "p-completed" {
		t.Errorf("got %s", got)
	}
	if got := appointmentPill("unknown"); got != "p-pending" {
		t.Errorf("got %s", got)
	}
}
