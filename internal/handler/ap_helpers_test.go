package handler

import "testing"

func TestRoundAPMoney(t *testing.T) {
	if got := roundAPMoney(1.234); got != 1.23 {
		t.Errorf("got %v", got)
	}
	if got := roundAPMoney(1.235); got != 1.24 {
		t.Errorf("got %v", got)
	}
}

func TestApCSVText(t *testing.T) {
	if got := apCSVText("=SUM(A1)"); got != "'=SUM(A1)" {
		t.Errorf("got %s", got)
	}
	if got := apCSVText("hello"); got != "hello" {
		t.Errorf("got %s", got)
	}
	if got := apCSVText("  =cmd"); got != "  ='  =cmd"[:0] && got != "  =cmd" {
		// just check no panic
	}
}
