package handler

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAppName_Default(t *testing.T) {
	os.Unsetenv("APP_NAME")
	if got := appName(); got != "AutoRepair OS" {
		t.Errorf("appName() = %q, want %q", got, "AutoRepair OS")
	}
}

func TestAppName_FromEnv(t *testing.T) {
	t.Setenv("APP_NAME", "XX車行維修管理系統")
	if got := appName(); got != "XX車行維修管理系統" {
		t.Errorf("appName() = %q, want %q", got, "XX車行維修管理系統")
	}
}

func TestRenderPage_ContainsAppName(t *testing.T) {
	t.Setenv("APP_NAME", "測試車行")
	rec := httptest.NewRecorder()
	renderPage(rec, "測試標題", "/test", "owner", "owner", "test body")
	body := rec.Body.String()
	if !strings.Contains(body, "測試車行") {
		t.Errorf("renderPage 未包含 APP_NAME")
	}
	if !strings.Contains(body, "測試標題 - 測試車行") {
		t.Errorf("renderPage title 未包含 APP_NAME")
	}
	if !strings.Contains(body, "🔧 測試車行") {
		t.Errorf("renderPage h1 未包含 APP_NAME")
	}
}
