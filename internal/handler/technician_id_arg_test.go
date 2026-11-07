package handler

import "testing"

func TestTechnicianIDArg(t *testing.T) {
	if technicianIDArg("0") != nil {
		t.Error("zero should return nil")
	}
	if v, ok := technicianIDArg("5").(int64); !ok || v != 5 {
		t.Errorf("got %v", technicianIDArg("5"))
	}
}
