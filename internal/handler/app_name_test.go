package handler

import "testing"

func TestAppName(t *testing.T) {
	got := appName()
	if got == "" {
		t.Error("should not be empty")
	}
}
