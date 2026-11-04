// Package routes 組裝 HTTP 路由（含 middleware）。
// Phase 0：先提供 New() 以利測試直接用 httptest.NewServer(routes.New())，
// 後續逐步把 main.go 的路由遷移過來。
package routes

import (
	"github.com/go-chi/chi/v5"

	"autorepairos/internal/handler"
)

// Deps 預留給日後注入設定、服務等相依。
type Deps struct{}

// New 建立應用路由，外層需加上 middleware（如 CSRFProtection）。
func New(d Deps) *chi.Mux {
	r := chi.NewRouter()
	// Phase 1：唯讀搜尋 API
	r.Get("/api/parts", handler.RequireLogin(handler.SearchPartsAPI))
	r.Get("/api/customers/search", handler.RequireLogin(handler.SearchCustomersAPI))
	r.Get("/api/inventory/search", handler.AllStaff(handler.SearchInventoryAPI))
	r.Get("/api/work-orders/search", handler.RequireLogin(handler.SearchWorkOrdersAPI))
	r.Get("/api/vehicles/search", handler.RequireLogin(handler.SearchVehiclesAPI))
	_ = handler.CSRFProtection(r)
	return r
}
