package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-chi/chi/v5"

	"autorepairos/internal/config"
	"autorepairos/internal/cron"
	"autorepairos/internal/db"
	"autorepairos/internal/handler"
	"autorepairos/internal/integration/vision"
	"autorepairos/internal/seed"
	"autorepairos/web/pages"
)

// 建置資訊，由 build-release.ps1 以 -ldflags -X 注入。
// 本地 go run / go build 沒有注入時保持空字串，頁面顯示「開發版」。
var (
	buildTime = ""
	gitCommit = ""
)

// versionText 回傳「版本 + 建置時間 + Git 修訂」的顯示字串。
// 用在啟動訊息與頁尾，讓現場回報問題時能直接說出跑的是哪一版。
func versionText() string {
	if buildTime == "" && gitCommit == "" {
		return "開發版"
	}
	s := ""
	if buildTime != "" {
		s = "建置 " + buildTime
	}
	if gitCommit != "" {
		if s != "" {
			s += " / "
		}
		s += "git " + gitCommit
	}
	return s
}

func main() {
	cfg := config.Load()
	if cfg.AppName != "" {
		pages.AppName = cfg.AppName
	}
	log.Printf("AutoRepairOS 啟動：%s", versionText())

	if err := db.Init(cfg); err != nil {
		log.Fatalf("資料庫初始化失敗: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Migration 執行失敗: %v", err)
	}

	// 檢查 lookup 分頁籤與資料是否配套（只 log 警告，不中斷啟動）。
	handler.CheckLookupCategories()

	// 視覺辨識（行照/車牌 OCR）：依 config 建立 client，未設定則為 nil（功能停用）。
	if vc := vision.NewClient(cfg.VisionMode, cfg.VisionAPIURL, cfg.VisionAPIKey,
		cfg.VisionModel, cfg.VisionLocalURL, cfg.VisionAutoStart, cfg.VisionIdleMin,
		cfg.VisionHFModel, cfg.VisionModelPath, cfg.VisionMmprojPath, cfg.VisionLocalProtocol); vc != nil {
		handler.SetVisionClient(vc)
		if vc.Mode == "local" && vc.AutoStart {
			log.Printf("視覺辨識已啟用：模式=local（自動管理 llama-server，閒置 %d 分鐘自動關閉）", vc.IdleMin)
		} else {
			log.Printf("視覺辨識已啟用：模式=%s", vc.Mode)
		}
	} else {
		log.Printf("視覺辨識未啟用（.env 未設定 VISION_API_URL/VISION_API_KEY 或 VISION_LOCAL_URL）")
	}

	if cfg.RunSeed {
		if err := seed.Run(cfg); err != nil {
			log.Fatalf("Seed 執行失敗: %v", err)
		}
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			rc := chi.RouteContext(req.Context())
			if rc != nil {
				for i := 0; i < len(rc.URLParams.Keys); i++ {
					if i < len(rc.URLParams.Values) {
						req.SetPathValue(rc.URLParams.Keys[i], rc.URLParams.Values[i])
					}
				}
			}
			next.ServeHTTP(w, req)
		})
	})

	// 未登入
	r.Get("/login", handler.LoginHandler)
	r.Post("/login", handler.LoginHandler)
	r.Post("/logout", handler.LogoutHandler)
	r.Get("/api/me", handler.MeHandler)
	r.Get("/api/vision/status", handler.RequireLogin(handler.VisionStatus))
	r.Post("/api/vision/recognize", handler.CounterUp(handler.RecognizePlate))
	r.Post("/api/vision/invoice", handler.CounterUp(handler.RecognizeInvoice))
	r.Get("/api/suppliers", handler.RequireLogin(handler.SupplierOptionsAPI))
	r.Post("/api/suppliers/quick-create", handler.CounterUp(handler.QuickCreateSupplier))
	r.Get("/api/parts", handler.RequireLogin(handler.SearchPartsAPI))
	r.Get("/api/customers/search", handler.RequireLogin(handler.SearchCustomersAPI))
	r.Get("/api/customers/picker", handler.RequireLogin(handler.CustomerPickerAPI))
	r.Get("/api/inventory/search", handler.AllStaff(handler.SearchInventoryAPI))
	r.Get("/api/work-orders/search", handler.RequireLogin(handler.SearchWorkOrdersAPI))
	r.Get("/api/vehicles/search", handler.RequireLogin(handler.SearchVehiclesAPI))
	r.Get("/api/work-orders/vehicles", handler.RequireLogin(handler.SearchWorkOrderVehiclesAPI))
	r.Get("/api/lookup/search", handler.RequireLogin(handler.SearchLookupAPI))
	r.Get("/api/insurance/search", handler.RequireLogin(handler.SearchInsuranceAPI))
	r.Get("/api/suppliers/search", handler.RequireLogin(handler.SearchSuppliersAPI))
	r.Get("/api/appointments/search", handler.RequireLogin(handler.SearchAppointmentsAPI))
	r.Get("/api/appointments/calendar", handler.AllStaff(handler.AppointmentCalendarAPI))
	r.Get("/api/subcontracts/search", handler.RequireLogin(handler.SearchSubcontractsAPI))
	r.Get("/api/equipment/search", handler.RequireLogin(handler.SearchEquipmentAPI))
	r.Get("/api/staff/search", handler.OwnerOnly(handler.SearchStaffAPI))
	r.Get("/api/receivables/search", handler.AllStaff(handler.SearchReceivablesAPI))
	r.Get("/api/insurances/search", handler.RequireLogin(handler.SearchInsurancesAPI))
	r.Get("/api/insurances/expiring", handler.RequireLogin(handler.ExpiringInsurancesAPI))
	r.Get("/api/inspections/search", handler.RequireLogin(handler.SearchInspectionsAPI))
	r.Get("/api/expenses/search", handler.AllStaff(handler.SearchExpensesAPI))

	// LINE webhook：不用登入，靠 X-Line-Signature 驗簽。
	r.Post("/webhook/line", handler.LineWebhook)

	// T2.11: 追加維修授權（公開頁面，客戶透過 LINE 連結存取）
	r.Get("/approval/{token}", handler.ApprovalPageHandler)
	r.Post("/approval/approve/{token}", handler.ApproveRequestHandler)
	r.Post("/approval/reject/{token}", handler.RejectRequestHandler)

	// T2.10: LINE 數位維修履歷與保養收據（公開 API，客戶透過 LINE 連結存取）
	r.Get("/api/passbook", handler.PassbookAPI)

	// 總覽
	r.Get("/", handler.AllStaff(handler.IndexHandler))

	// 首頁（index.html 用 template 注入 APP_NAME，需登入才能取得）
	r.Get("/index.html", handler.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		tmpl, err := template.ParseFiles("web/index.html")
		if err != nil {
			http.Error(w, "template parse error", http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, map[string]string{"AppName": cfg.AppName})
	})))

	// 客戶
	r.Get("/customers", handler.AllStaff(handler.ListCustomers))
	r.Post("/customers", handler.CounterUp(handler.CreateCustomer))
	r.Get("/customers/{id}/edit", handler.AllStaff(handler.EditCustomerForm))
	r.Post("/customers/{id}", handler.CounterUp(handler.UpdateCustomer))
	r.Post("/customers/{id}/delete", handler.OwnerOnly(handler.DeleteCustomer))
	// LINE / Telegram 綁定
	r.Post("/customers/{id}/line-bind", handler.CounterUp(handler.BindCustomerLine))
	r.Post("/customers/{id}/line-unbind", handler.CounterUp(handler.UnbindCustomerLine))
	r.Post("/customers/{id}/telegram-bind", handler.CounterUp(handler.BindCustomerTelegram))
	r.Post("/customers/{id}/telegram-unbind", handler.CounterUp(handler.UnbindCustomerTelegram))
	r.Post("/customers/{id}/generate-binding-code", handler.CounterUp(handler.GenerateBindingCode))

	// 車輛
	r.Get("/vehicles", handler.AllStaff(handler.ListVehicles))
	r.Get("/vehicles/new", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/vehicles", http.StatusFound)
	})
	r.Post("/vehicles", handler.CounterUp(handler.CreateVehicle))
	r.Get("/vehicles/{id}/edit", handler.AllStaff(handler.EditVehicleForm))
	r.Post("/vehicles/{id}", handler.CounterUp(handler.UpdateVehicle))
	r.Post("/vehicles/{id}/delete", handler.OwnerOnly(handler.DeleteVehicle))

	// 車輛履歷
	r.Get("/vehicles/{id}", handler.AllStaff(handler.ShowVehicle))

	// 工單
	r.Get("/work-orders", handler.AllStaff(handler.ListWorkOrders))
	r.Get("/work-orders/new", handler.CounterUpPage(handler.NewWorkOrder))
	r.Post("/work-orders", handler.CounterUp(handler.CreateWorkOrder))
	r.Post("/api/quick-vehicle", handler.CounterUp(handler.QuickVehicle))
	r.Get("/work-orders/{id}", handler.AllStaff(handler.ShowWorkOrder))
	r.Post("/work-orders/{id}/status", handler.CounterUp(handler.UpdateWorkOrderStatus))
	r.Post("/work-orders/{id}/approval", handler.CounterUp(handler.UpdateWorkOrderApproval))
	// 追加維修授權：櫃檯建立（客戶端的三個 /approval/... 路由在更上方）)

	r.Post("/work-orders/{id}/approval-request", handler.CounterUp(handler.CreateApprovalFromWorkOrder))
	// 櫃檯端的解套路徑：客戶電話中同意、或取消這筆授權。)

	// 沒有這兩條的話，未決授權會擋住完工（見 CheckApprovalForStatus）
	// 而櫃檯只能等 24 小時過期。
	r.Post("/work-orders/{id}/approval-requests/{req_id}/approve-phone", handler.CounterUp(handler.ApproveApprovalByPhone))
	r.Post("/work-orders/{id}/approval-requests/{req_id}/cancel", handler.CounterUp(handler.CancelApprovalRequest))
	r.Post("/work-orders/{id}/claim", handler.CounterUp(handler.UpdateWorkOrderClaim))
	r.Post("/work-orders/{id}/sop", handler.CounterUp(handler.ToggleWorkOrderSOP))
	r.Post("/work-orders/{id}/sop/attach", handler.CounterUp(handler.AttachSOPTemplate))
	r.Post("/work-orders/{id}/sop/{template_id}/detach", handler.CounterUp(handler.DetachSOPTemplate))
	r.Post("/work-orders/{id}/sop/records/{record_id}/toggle", handler.CounterUpPage(handler.ToggleSOPRecord))
	r.Post("/work-orders/{id}/items", handler.CounterUpPage(handler.AddWorkOrderItem))
	r.Post("/work-orders/{id}/items/{item_id}/delete", handler.CounterUpPage(handler.DeleteWorkOrderItem))
	// T2.9：獨立估價草稿。估價寫入與轉換限櫃檯、管理員。
	r.Get("/estimates", handler.AllStaff(handler.ListEstimates))
	r.Get("/estimates/new", handler.CounterUpPage(handler.NewEstimate))
	r.Post("/estimates", handler.CounterUp(handler.CreateEstimate))
	r.Get("/estimates/{id}", handler.AllStaff(handler.ShowEstimate))
	r.Post("/estimates/{id}/items", handler.CounterUp(handler.AddEstimateItem))
	r.Post("/estimates/{id}/items/{item_id}/delete", handler.CounterUp(handler.DeleteEstimateItem))
	r.Post("/estimates/{id}/discount", handler.CounterUp(handler.SetEstimateDiscount))
	r.Post("/estimates/{id}/convert", handler.CounterUp(handler.ConvertEstimate))
	r.Get("/work-orders/{id}/estimate-source", handler.AllStaff(handler.ShowWorkOrderEstimateSource))
	// 庫存
	r.Get("/inventory", handler.AllStaff(handler.ListInventory))
	r.Post("/inventory", handler.CounterUp(handler.CreateInventoryItem))
	r.Get("/inventory/{id}/edit", handler.AllStaff(handler.EditInventoryForm))
	r.Post("/inventory/{id}", handler.CounterUp(handler.UpdateInventoryItem))
	r.Get("/inventory/{id}/stock", handler.AllStaff(handler.StockAdjustForm))
	r.Post("/inventory/{id}/stock", handler.CounterUp(handler.ApplyStockAdjust))
	r.Post("/inventory/{id}/delete", handler.OwnerOnly(handler.DeleteInventoryItem))
	// 庫存異動紀錄與舊系統負庫存補正。
	//
	// 補正表單刻意走 query 參數（/inventory/negative?id=N）而非
	// /inventory/negative/{id}：後者與既有的 /inventory/{id}/edit 同為三段路徑，
	// Go 1.22 ServeMux 判定互不更明確，會在啟動時 panic。
	r.Get("/inventory/movements", handler.AllStaff(handler.MovementLog))
	r.Get("/inventory/negative", handler.AllStaff(handler.NegativeStockList))
	r.Post("/inventory/negative/fix", handler.CounterUpPage(handler.ApplyNegativeStockFix))
	// 廠商（材料商 / 協力廠商）
	r.Get("/suppliers", handler.AllStaff(handler.ListSuppliers))
	r.Post("/suppliers", handler.CounterUp(handler.CreateSupplier))
	r.Get("/suppliers/{id}/edit", handler.CounterUpPage(handler.EditSupplierForm))
	r.Post("/suppliers/{id}", handler.CounterUp(handler.UpdateSupplier))
	r.Post("/suppliers/{id}/deactivate", handler.OwnerOnly(handler.DeactivateSupplier))
	r.Post("/suppliers/{id}/reactivate", handler.OwnerOnly(handler.ReactivateSupplier))
	// 員工與健保資料（含健保眷屬）
	r.Get("/staff", handler.AllStaff(handler.ListStaff))
	r.Post("/staff", handler.CounterUpPage(handler.CreateStaff))
	r.Get("/staff/{id}/edit", handler.CounterUpPage(handler.EditStaffForm))
	r.Post("/staff/{id}", handler.CounterUp(handler.UpdateStaff))
	r.Post("/staff/{id}/deactivate", handler.OwnerOnly(handler.DeactivateStaff))
	r.Post("/staff/{id}/reactivate", handler.OwnerOnly(handler.ReactivateStaff))
	// 技師片語庫（T2.7）
	r.Get("/phrases", handler.AllStaff(handler.ListPhrases))
	r.Post("/phrases/new", handler.AllStaff(handler.CreatePhrase))
	r.Get("/phrases/{id}/edit", handler.AllStaff(handler.EditPhraseForm))
	r.Post("/phrases/{id}/update", handler.AllStaff(handler.UpdatePhrase))
	r.Post("/phrases/{id}/delete", handler.AllStaff(handler.DeletePhrase))
	r.Get("/api/phrases/search", handler.RequireLogin(handler.SearchPhrasesAPI))
	// T2.8: 照片標記 + 舊傷預檢
	r.Post("/api/photos/{photo_id}/annotations", handler.AllStaff(handler.AddPhotoAnnotation))
	r.Get("/api/photos/{photo_id}/annotations", handler.AllStaff(handler.ListPhotoAnnotations))
	r.Post("/api/annotations/{annotation_id}/delete", handler.AllStaff(handler.DeletePhotoAnnotation))
	r.Post("/work-orders/{id}/walkaround-marks", handler.AllStaff(handler.AddWalkaroundMark))
	r.Get("/work-orders/{id}/walkaround-marks", handler.AllStaff(handler.ListWalkaroundMarks))
	r.Post("/work-orders/{id}/walkaround-marks/{mark_id}/delete", handler.AllStaff(handler.DeleteWalkaroundMark))
	// 舊系統補遷移：車輛投保紀錄 / 定檢排程 / 保養套餐 / 零用金
	r.Get("/insurances", handler.AllStaff(handler.ListVehicleInsurances))
	r.Get("/insurances/expiring", handler.AllStaff(handler.ListVehicleInsurances))
	r.Get("/insurances/new", handler.CounterUpPage(handler.NewVehicleInsuranceForm))
	r.Post("/insurances", handler.CounterUp(handler.CreateVehicleInsurance))
	r.Get("/insurances/{id}/edit", handler.CounterUpPage(handler.EditVehicleInsuranceForm))
	r.Post("/insurances/{id}", handler.CounterUp(handler.UpdateVehicleInsurance))
	r.Post("/insurances/{id}/delete", handler.CounterUp(handler.DeleteVehicleInsurance))
	r.Get("/inspections", handler.AllStaff(handler.ListVehicleInspections))
	r.Get("/inspections/new", handler.CounterUpPage(handler.NewVehicleInspectionForm))
	r.Post("/inspections", handler.CounterUp(handler.CreateVehicleInspection))
	r.Get("/inspections/{id}/edit", handler.CounterUpPage(handler.EditVehicleInspectionForm))
	r.Post("/inspections/{id}", handler.CounterUp(handler.UpdateVehicleInspection))
	r.Post("/inspections/{id}/delete", handler.CounterUp(handler.DeleteVehicleInspection))
	r.Get("/maintenance-templates", handler.AllStaff(handler.ListMaintenanceTemplates))
	// 保養套餐範本 CRUD（項目整批重寫）)

	r.Get("/maintenance-templates/new", handler.CounterUpPage(handler.NewMaintenanceTemplateForm))
	r.Post("/maintenance-templates", handler.CounterUp(handler.CreateMaintenanceTemplate))
	r.Get("/maintenance-templates/{id}/edit", handler.CounterUpPage(handler.EditMaintenanceTemplateForm))
	r.Post("/maintenance-templates/{id}", handler.CounterUp(handler.UpdateMaintenanceTemplate))
	r.Post("/maintenance-templates/{id}/items", handler.CounterUp(handler.ReplaceMaintenanceTemplateItems))
	r.Post("/maintenance-templates/{id}/delete", handler.CounterUp(handler.DeleteMaintenanceTemplate))
	// SOP 模板管理（SRS 5.x：大修檢查表）
	r.Get("/sop-templates", handler.AllStaff(handler.ListSOPTemplates))
	r.Get("/sop-templates/new", handler.CounterUpPage(handler.NewSOPTemplateForm))
	r.Post("/sop-templates", handler.CounterUp(handler.CreateSOPTemplate))
	r.Get("/sop-templates/{id}/edit", handler.CounterUpPage(handler.EditSOPTemplateForm))
	r.Post("/sop-templates/{id}", handler.CounterUp(handler.UpdateSOPTemplate))
	r.Post("/sop-templates/{id}/delete", handler.CounterUp(handler.DeleteSOPTemplate))
	r.Post("/sop-templates/{id}/items", handler.CounterUp(handler.ReplaceSOPItems))
	r.Get("/expenses", handler.CounterUpPage(handler.ListExpenses))
	// 收款沖銷（CAR35 Customer Pay，90,762 筆）——
	// 財務頁面，用 CounterUpPage 讓技師看不到（handler 內另有一次 canSeeMoney 檢查）。
	r.Get("/receivables", handler.CounterUpPage(handler.ListReceivables))
	r.Get("/receivables/new", handler.CounterUpPage(handler.NewReceivableForm))
	r.Post("/receivables", handler.CounterUp(handler.CreateReceivable))
	r.Get("/receivables/{id}/edit", handler.CounterUpPage(handler.EditReceivableForm))
	r.Post("/receivables/{id}", handler.CounterUp(handler.UpdateReceivable))
	r.Post("/receivables/{id}/void", handler.OwnerOnly(handler.VoidReceivable))
	// 保險公司（CAR35 P0-1：Company 表，理賠案件歸屬對象）
	r.Get("/insurance-companies", handler.AllStaff(handler.ListInsuranceCompanies))
	r.Post("/insurance-companies", handler.CounterUp(handler.CreateInsuranceCompany))
	r.Get("/insurance-companies/{id}/edit", handler.CounterUpPage(handler.EditInsuranceCompanyForm))
	r.Post("/insurance-companies/{id}", handler.CounterUp(handler.UpdateInsuranceCompany))
	r.Post("/insurance-companies/{id}/delete", handler.OwnerOnly(handler.DeactivateInsuranceCompany))
	// Phase 2: 技師工時/佣金
	r.Get("/work-orders/{id}/worklogs", handler.AllStaff(handler.ListTechnicianWorklogs))
	r.Post("/work-orders/{id}/worklogs", handler.WithActor(handler.AddTechnicianWorklog))
	r.Get("/work-orders/{id}/worklogs/{log_id}/edit", handler.CounterUpPage(handler.EditTechnicianWorklogForm))
	r.Post("/work-orders/{id}/worklogs/{log_id}", handler.CounterUp(handler.UpdateTechnicianWorklog))
	r.Post("/work-orders/{id}/worklogs/{log_id}/delete", handler.CounterUp(handler.DeleteTechnicianWorklog))
	r.Get("/technicians/commissions", handler.AllStaff(handler.ListTechnicianCommissions))
	// Phase 2: 收據列印
	r.Get("/work-orders/{id}/receipt", handler.AllStaff(handler.PrintWorkOrderReceipt))
	// 估價單（FR-PRINT-01）：?paper=a4|thermal。與結帳收據刻意分開——)

	// 估價單有有效期與客戶簽名欄，收據是收款憑證，兩者用途不同。
	r.Get("/work-orders/{id}/quotation", handler.CounterUpPage(handler.PrintWorkOrderQuotation))
	// 完工證明（FR-PRINT-02）：交車時給客戶的「做了什麼」證明，不含金額。)

	r.Get("/work-orders/{id}/completion", handler.AllStaff(handler.PrintWorkOrderCompletion))
	// 收款明細含稅版（FR-PRINT-03）：財務用，支付方式與稅額分列。
	r.Get("/receivables/{id}/print", handler.CounterUp(handler.PrintReceiptWithTax))
	// AR 催款單（FR-PRINT-04）)

	r.Get("/customers/{id}/ar-statement", handler.CounterUpPage(handler.PrintARStatement))
	// AP 月結對帳單（FR-PRINT-05）)

	r.Get("/suppliers/{id}/ap-statement", handler.CounterUpPage(handler.PrintAPStatement))
	// T2.12：供應商應付帳款、帳齡、付款沖銷與可列印／匯出的月結對帳單。)

	r.Get("/ap", handler.CounterUpPage(handler.APHome))
	r.Get("/ap/statement", handler.CounterUpPage(handler.APStatement))
	r.Get("/ap/statement.csv", handler.CounterUp(handler.APStatementCSV))
	r.Post("/ap/{id}/payments", handler.CounterUp(handler.RecordAPPayment))
	r.Post("/ap/{id}/void", handler.OwnerOnly(handler.VoidAPPayable))
	// 日結報表（FR-PRINT-06）)

	r.Get("/reports/daily", handler.CounterUpPage(handler.PrintDailyReport))
	// Phase 2: 照片上傳
	r.Get("/work-orders/{id}/photos", handler.AllStaff(handler.ListWorkOrderPhotos))
	r.Post("/work-orders/{id}/photos", handler.CounterUp(handler.UploadWorkOrderPhoto))
	r.Post("/work-orders/{id}/photos/{photo_id}/delete", handler.CounterUp(handler.DeleteWorkOrderPhoto))
	r.Get("/work-orders/{id}/photos/{n}", handler.RequireLogin(handler.ServePhoto))
	// Phase 3: 排程 + 報表
	r.Get("/lookup", handler.CounterUpPage(handler.ListLookupItems))
	r.Get("/lookup/new", handler.CounterUpPage(handler.NewLookupItemForm))
	r.Post("/lookup", handler.CounterUp(handler.CreateLookupItem))
	r.Get("/lookup/{id}/edit", handler.CounterUpPage(handler.EditLookupItemForm))
	r.Post("/lookup/{id}", handler.CounterUp(handler.UpdateLookupItem))
	r.Post("/lookup/{id}/deactivate", handler.OwnerOnly(handler.DeactivateLookupItem))
	r.Post("/lookup/{id}/reactivate", handler.OwnerOnly(handler.ReactivateLookupItem))
	r.Get("/scheduled-tasks", handler.AllStaff(handler.ListScheduledTasks))
	r.Post("/scheduled-tasks/{key}/toggle", handler.OwnerOnly(handler.ToggleScheduledTask))
	r.Post("/scheduled-tasks/backups/run", handler.OwnerOnly(handler.RunBackupNow))
	r.Post("/scheduled-tasks/notifications/run", handler.OwnerOnly(handler.RunNotificationsNow))
	r.Post("/scheduled-tasks/maintenance-due/run", handler.OwnerOnly(handler.RunMaintenanceScanNow))
	r.Post("/scheduled-tasks/work-order-ready/run", handler.OwnerOnly(handler.RunWorkOrderReadyNow))
	r.Post("/notifications/{id}/cancel", handler.OwnerOnly(handler.CancelNotification))
	r.Get("/reports", handler.AllStaff(handler.ReportsHome))
	r.Get("/reports/export", handler.AllStaff(handler.ReportsExport))
	// 經營分析（SRS 5.12）：KPI、損益分拆、留存、方案成效、死頭料、CSV 匯出
	r.Get("/analytics", handler.CounterUpPage(handler.AnalyticsHome))
	r.Get("/analytics/export", handler.CounterUp(handler.AnalyticsExport))
	// 預約排程（SRS 5.9）
	r.Get("/appointments", handler.AllStaff(handler.ListAppointments))
	r.Post("/appointments", handler.CounterUp(handler.CreateAppointment))
	r.Get("/appointments/{id}/edit", handler.AllStaff(handler.EditAppointmentForm))
	r.Post("/appointments/{id}", handler.CounterUp(handler.UpdateAppointment))
	r.Post("/appointments/{id}/delete", handler.OwnerOnly(handler.DeleteAppointment))
	// 車輛里程履歷（FR-VH-03）：進廠工單自動帶入，也可手動登錄
	r.Get("/vehicles/{id}/mileage/new", handler.CounterUpPage(handler.AddVehicleMileageForm))
	r.Post("/vehicles/{id}/mileage", handler.CounterUp(handler.CreateVehicleMileage))
	// 外包加工（SRS 5.11）
	r.Get("/subcontracts", handler.AllStaff(handler.ListSubcontracts))
	r.Post("/subcontracts", handler.CounterUp(handler.CreateSubcontract))
	r.Get("/subcontracts/{id}/edit", handler.AllStaff(handler.EditSubcontractForm))
	r.Post("/subcontracts/{id}", handler.CounterUp(handler.UpdateSubcontract))
	r.Post("/subcontracts/{id}/delete", handler.OwnerOnly(handler.DeleteSubcontract))
	// 設備管理（SRS 5.11）
	r.Get("/equipment", handler.AllStaff(handler.ListEquipment))
	r.Post("/equipment", handler.CounterUp(handler.CreateEquipment))
	r.Get("/equipment/{id}/edit", handler.AllStaff(handler.EditEquipmentForm))
	r.Post("/equipment/{id}", handler.CounterUp(handler.UpdateEquipment))
	r.Post("/equipment/{id}/delete", handler.OwnerOnly(handler.DeleteEquipment))
	r.Get("/equipment/{id}/maintenance", handler.AllStaff(handler.ListEquipmentMaintenance))
	r.Post("/equipment/{id}/maintenance", handler.CounterUp(handler.CreateEquipmentMaintenance))
	r.Post("/equipment/{id}/maintenance/{maint_id}/delete", handler.OwnerOnly(handler.DeleteEquipmentMaintenance))
	// 進貨單（採購進貨：品項 + 應付總額）
	r.Get("/purchase-orders", handler.AllStaff(handler.ListPurchaseOrders))
	r.Post("/purchase-orders", handler.CounterUp(handler.CreatePurchaseOrder))
	r.Get("/purchase-orders/new", handler.CounterUpPage(handler.NewPurchaseOrderForm))
	r.Get("/purchase-orders/{id}", handler.AllStaff(handler.ShowPurchaseOrder))
	r.Post("/purchase-orders/{id}/items", handler.CounterUp(handler.AddPurchaseOrderItem))
	r.Delete("/purchase-orders/{id}/items/{itemId}", handler.CounterUp(handler.DeletePurchaseOrderItem))
	r.Post("/purchase-orders/{id}/receive", handler.CounterUp(handler.ReceivePurchaseOrder))
	r.Post("/purchase-orders/{id}/delete", handler.CounterUp(handler.DeletePurchaseOrder))
	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	handler.RegisterStatic(r)

	// 啟動排程器（通知佇列、每日備份、保養到期掃描）
	scheduler, err := cron.New()
	if err != nil {
		log.Printf("排程器初始化失敗（功能停用）: %v", err)
	} else {
		scheduler.Start()
		defer scheduler.Stop()
	}

	telegramCtx, telegramCancel := context.WithCancel(context.Background())
	var telegramWG sync.WaitGroup
	telegramWG.Add(1)
	go handler.StartTelegramBinding(telegramCtx, &telegramWG)

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			log.Printf("正在關閉服務，清理資源…")
			telegramCancel()
			telegramWG.Wait()
			handler.StopVisionClient()
			if scheduler != nil {
				scheduler.Stop()
			}
			db.Close()
		})
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("收到關閉訊號（Ctrl+C / SIGTERM）…")
		shutdown()
		os.Exit(0)
	}()

	log.Printf("%s 啟動中，監聽 port: %s", cfg.AppName, port)
	if err := http.ListenAndServe(":"+port, handler.CSRFProtection(r)); err != nil {
		log.Printf("HTTP 服務結束: %v", err)
		shutdown()
		os.Exit(1)
	}
}
