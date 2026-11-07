package handler

import (
	"testing"

	"autorepairos/internal/model"
)

func TestMaintDueText(t *testing.T) {
	dateCell, mileCell := maintDueText(model.Vehicle{}, 0)
	if dateCell == "" {
		t.Error("dateCell should not be empty")
	}
	_ = mileCell
}
