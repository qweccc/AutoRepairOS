package handler

import "testing"

func TestSizeKB(t *testing.T) {
	if sizeKB(500) != "500 B" {
		t.Errorf("got %q", sizeKB(500))
	}
	if sizeKB(2048) != "2 KB" {
		t.Errorf("got %q", sizeKB(2048))
	}
}
