package handler

import "testing"

func TestGrossStatValue(t *testing.T) {
	if got := grossStatValue(false, analyticsCosts{}, 50.0); got != "50.0%" {
		t.Errorf("got %s", got)
	}
	if got := grossStatValue(true, analyticsCosts{LaborRecorded: false}, 50.0); got != "未記錄" {
		t.Errorf("got %s", got)
	}
	if got := grossStatValue(true, analyticsCosts{LaborRecorded: true}, 50.0); got != "50.0%" {
		t.Errorf("got %s", got)
	}
}
