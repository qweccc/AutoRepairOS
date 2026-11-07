package handler

import "testing"

func TestIsValidStaffKind(t *testing.T) {
	if !isValidStaffKind("employee") {
		t.Error("employee should pass")
	}
	if !isValidStaffKind("dependent") {
		t.Error("dependent should pass")
	}
	if isValidStaffKind("other") {
		t.Error("other should fail")
	}
}
