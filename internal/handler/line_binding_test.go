package handler

import (
	"testing"

	"autorepairos/internal/model"
)

func TestLineBindingBlock(t *testing.T) {
	got := lineBindingBlock(model.Customer{ID: 1, Name: "test"})
	if got == "" {
		t.Error("should not be empty")
	}
	got = lineBindingBlock(model.Customer{ID: 1, Name: "test", LineUserID: "U123"})
	if got == "" {
		t.Error("should not be empty")
	}
}
