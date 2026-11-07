package handler

import "testing"

func TestPctText(t *testing.T) {
	if pctText(10, 0) != "—" {
		t.Error("zero total")
	}
	if pctText(50, 100) != "50.0%" {
		t.Errorf("got %q", pctText(50, 100))
	}
}
