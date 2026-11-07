package handler

import "testing"

func TestValidVisionImageBase64(t *testing.T) {
	if validVisionImageBase64("") {
		t.Error("empty should be invalid")
	}
	if validVisionImageBase64("not-base64!!") {
		t.Error("should be invalid")
	}
}
