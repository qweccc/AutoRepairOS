package handler

import (
	"context"
	"testing"
)

func TestGrossStatValue(t *testing.T) {
	if got := grossStatValue(context.Background(), false, analyticsCosts{}, 50.0); got != "50.0%" {
		t.Errorf("got %s", got)
	}
	if got := grossStatValue(context.Background(), true, analyticsCosts{LaborRecorded: false}, 50.0); got != "未記錄" {
		t.Errorf("got %s", got)
	}
	if got := grossStatValue(context.Background(), true, analyticsCosts{LaborRecorded: true}, 50.0); got != "50.0%" {
		t.Errorf("got %s", got)
	}
}
