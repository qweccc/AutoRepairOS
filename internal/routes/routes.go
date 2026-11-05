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
	r.Get("/equipment", handler.AllStaff(handler.ListEquipment))
	r.Post("/equipment", handler.CounterUp(handler.CreateEquipment))
	r.Get("/appointments", handler.AllStaff(handler.ListAppointments))
	r.Post("/appointments", handler.CounterUp(handler.CreateAppointment))
	r.Get("/subcontracts", handler.AllStaff(handler.ListSubcontracts))
	r.Post("/subcontracts", handler.CounterUp(handler.CreateSubcontract))
	r.Get("/purchase-orders", handler.AllStaff(handler.ListPurchaseOrders))
	r.Post("/purchase-orders", handler.CounterUp(handler.CreatePurchaseOrder))
	r.Get("/lookup", handler.CounterUpPage(handler.ListLookupItems))
	r.Get("/scheduled-tasks", handler.AllStaff(handler.ListScheduledTasks))
	r.Get("/reports", handler.AllStaff(handler.ReportsHome))
	r.Get("/analytics", handler.CounterUpPage(handler.AnalyticsHome))
	r.Get("/maintenance-templates", handler.AllStaff(handler.ListMaintenanceTemplates))
	r.Get("/sop-templates", handler.AllStaff(handler.ListSOPTemplates))
	r.Get("/insurance-companies", handler.AllStaff(handler.ListInsuranceCompanies))
	r.Get("/work-orders/{id}/receipt", handler.AllStaff(handler.PrintWorkOrderReceipt))
	r.Get("/approval/{token}", handler.ApprovalPageHandler)
	r.Post("/approval/approve/{token}", handler.ApproveRequestHandler)
	r.Post("/approval/reject/{token}", handler.RejectRequestHandler)
	r.Get("/api/passbook", handler.PassbookAPI)
	r.Get("/maintenance-templates/new", handler.CounterUpPage(handler.NewMaintenanceTemplateForm))
	r.Get("/sop-templates/new", handler.CounterUpPage(handler.NewSOPTemplateForm))
	r.Get("/insurance-companies", handler.AllStaff(handler.ListInsuranceCompanies))
	r.Get("/work-orders/{id}/receipt", handler.AllStaff(handler.PrintWorkOrderReceipt))
	r.Get("/work-orders/{id}/quotation", handler.CounterUpPage(handler.PrintWorkOrderQuotation))
	r.Get("/work-orders/{id}/completion", handler.AllStaff(handler.PrintWorkOrderCompletion))
	r.Get("/receivables/{id}/print", handler.CounterUp(handler.PrintReceiptWithTax))
	r.Get("/customers/{id}/ar-statement", handler.CounterUpPage(handler.PrintARStatement))
	r.Get("/suppliers/{id}/ap-statement", handler.CounterUpPage(handler.PrintAPStatement))
	r.Get("/reports/daily", handler.CounterUpPage(handler.PrintDailyReport))
	r.Post("/work-orders/{id}/items", handler.CounterUpPage(handler.AddWorkOrderItem))
	r.Post("/work-orders/{id}/items/{item_id}/delete", handler.CounterUpPage(handler.DeleteWorkOrderItem))
	r.Get("/estimates", handler.AllStaff(handler.ListEstimates))
	r.Get("/estimates/new", handler.CounterUpPage(handler.NewEstimate))
	r.Post("/estimates", handler.CounterUp(handler.CreateEstimate))
	r.Get("/estimates/{id}", handler.AllStaff(handler.ShowEstimate))
	r.Get("/work-orders/{id}/estimate-source", handler.AllStaff(handler.ShowWorkOrderEstimateSource))
	r.Get("/api/parts", handler.RequireLogin(handler.SearchPartsAPI))
	r.Get("/api/customers/search", handler.RequireLogin(handler.SearchCustomersAPI))
	r.Get("/api/inventory/search", handler.AllStaff(handler.SearchInventoryAPI))
	r.Get("/api/work-orders/search", handler.RequireLogin(handler.SearchWorkOrdersAPI))
	r.Get("/api/vehicles/search", handler.RequireLogin(handler.SearchVehiclesAPI))
	_ = handler.CSRFProtection(r)
	return r
}
