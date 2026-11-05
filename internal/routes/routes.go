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
	// Phase 2：客戶/車輛 CRUD（唯讀/建立/更新）
	r.Get("/customers", handler.AllStaff(handler.ListCustomers))
	r.Post("/customers", handler.CounterUp(handler.CreateCustomer))
	r.Get("/customers/{id}/edit", handler.AllStaff(handler.EditCustomerForm))
	r.Post("/customers/{id}", handler.CounterUp(handler.UpdateCustomer))
	r.Get("/vehicles", handler.AllStaff(handler.ListVehicles))
	r.Post("/vehicles", handler.CounterUp(handler.CreateVehicle))
	r.Get("/vehicles/{id}/edit", handler.AllStaff(handler.EditVehicleForm))
	r.Post("/vehicles/{id}", handler.CounterUp(handler.UpdateVehicle))
	r.Get("/vehicles/{id}", handler.AllStaff(handler.ShowVehicle))
	r.Get("/work-orders", handler.AllStaff(handler.ListWorkOrders))
	r.Get("/work-orders/new", handler.CounterUpPage(handler.NewWorkOrder))
	r.Post("/work-orders", handler.CounterUp(handler.CreateWorkOrder))
	r.Get("/work-orders/{id}", handler.AllStaff(handler.ShowWorkOrder))
	r.Post("/work-orders/{id}/status", handler.CounterUp(handler.UpdateWorkOrderStatus))
	r.Get("/inventory", handler.AllStaff(handler.ListInventory))
	r.Post("/inventory", handler.CounterUp(handler.CreateInventoryItem))
	r.Get("/suppliers", handler.AllStaff(handler.ListSuppliers))
	r.Get("/staff", handler.AllStaff(handler.ListStaff))
	r.Post("/staff", handler.CounterUpPage(handler.CreateStaff))
	r.Get("/phrases", handler.AllStaff(handler.ListPhrases))
	r.Post("/phrases/new", handler.AllStaff(handler.CreatePhrase))
	r.Get("/insurances", handler.AllStaff(handler.ListVehicleInsurances))
	r.Get("/inspections", handler.AllStaff(handler.ListVehicleInspections))
	r.Get("/expenses", handler.CounterUpPage(handler.ListExpenses))
	r.Get("/receivables", handler.CounterUpPage(handler.ListReceivables))
	r.Get("/api/parts", handler.RequireLogin(handler.SearchPartsAPI))
	r.Get("/api/customers/search", handler.RequireLogin(handler.SearchCustomersAPI))
	r.Get("/api/inventory/search", handler.AllStaff(handler.SearchInventoryAPI))
	r.Get("/api/work-orders/search", handler.RequireLogin(handler.SearchWorkOrdersAPI))
	r.Get("/api/vehicles/search", handler.RequireLogin(handler.SearchVehiclesAPI))
	_ = handler.CSRFProtection(r)
	return r
}
