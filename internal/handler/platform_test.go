package handler

import "testing"

func TestIsValidPlatform(t *testing.T) {
	if !isValidPlatform("line") {
		t.Error("line should pass")
	}
	if !isValidPlatform("telegram") {
		t.Error("telegram should pass")
	}
	if isValidPlatform("discord") {
		t.Error("discord should fail")
	}
}
