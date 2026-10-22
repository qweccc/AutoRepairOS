package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"autorepairos/internal/model"
)

// TestMoneySearchAPIsRejectTechnician 守門：收款/支出搜尋 API 必須依 canSeeMoney
// 擋掉技師。列表頁早有此檢查，但 HTMX 搜尋 API 一度只用 RequireLogin，技師可繞過
// 頁面層級直接拿到全部金額——此測試防止該落差回潮。
func TestMoneySearchAPIsRejectTechnician(t *testing.T) {
	tech := model.User{Role: "technician"}
	cases := []struct {
		name string
		h    func(http.ResponseWriter, *http.Request, model.User)
	}{
		{"receivables", SearchReceivablesAPI},
		{"expenses", SearchExpensesAPI},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/"+tc.name+"/search?q=x", nil)
			rec := httptest.NewRecorder()
			tc.h(rec, req, tech)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("technician %s search: got %d, want 403", tc.name, rec.Code)
			}
		})
	}
}
