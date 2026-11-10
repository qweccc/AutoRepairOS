package handler

import (
	"testing"

	"autorepairos/internal/model"
)

func TestCanExportReportCosts(t *testing.T) {
	if !canExportReportCosts(model.User{Role: "owner"}) {
		t.Error("owner should export")
	}
	if canExportReportCosts(model.User{Role: "counter"}) {
		t.Error("counter should not export")
	}
}
