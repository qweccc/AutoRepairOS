package handler

import (
	"autorepairos/internal/model"
	"testing"
)

func TestVehicleOptionsJSON(t *testing.T) {
	got := vehicleOptionsJSON([]model.Vehicle{{ID: 1, PlateNumber: "ABC-123"}})
	if got == "" {
		t.Error("should not be empty")
	}
}
