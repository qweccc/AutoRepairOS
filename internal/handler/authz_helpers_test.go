package handler

import (
	"testing"

	"autorepairos/internal/model"
)

func TestCanSeeMoney(t *testing.T) {
	if !canSeeMoney(model.User{Role: "owner"}) {
		t.Error("owner should see money")
	}
	if !canSeeMoney(model.User{Role: "counter"}) {
		t.Error("counter should see money")
	}
	if canSeeMoney(model.User{Role: "technician"}) {
		t.Error("technician should not see money")
	}
}
