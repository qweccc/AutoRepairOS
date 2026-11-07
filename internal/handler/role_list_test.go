package handler

import "testing"

func TestRoleList(t *testing.T) {
	got := roleList([]string{"owner", "counter"})
	if got == "" {
		t.Error("should not be empty")
	}
}
