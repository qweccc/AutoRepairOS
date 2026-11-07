package handler

import "testing"

func TestFormatReportAmount(t *testing.T) {
	if got := formatReportAmount(123.456); got != "123.46" {
		t.Errorf("got %s", got)
	}
	if got := formatReportAmount(0); got != "0.00" {
		t.Errorf("got %s", got)
	}
}
