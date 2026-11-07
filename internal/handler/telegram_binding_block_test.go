package handler

import (
	"autorepairos/internal/model"
	"testing"
)

func TestTelegramBindingBlock(t *testing.T) {
	got := telegramBindingBlock(model.Customer{})
	if got == "" {
		t.Error("should not be empty")
	}
}
