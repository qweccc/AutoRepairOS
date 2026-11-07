package handler

import "testing"

func TestCsrfExempt(t *testing.T) {
	if !csrfExempt("/login") {
		t.Error("/login should be exempt")
	}
	if !csrfExempt("/webhook/line") {
		t.Error("/webhook/line should be exempt")
	}
	if !csrfExempt("/approval/approve/abc") {
		t.Error("/approval/approve/ should be exempt")
	}
	if !csrfExempt("/approval/reject/abc") {
		t.Error("/approval/reject/ should be exempt")
	}
	if csrfExempt("/dashboard") {
		t.Error("/dashboard should not be exempt")
	}
}
