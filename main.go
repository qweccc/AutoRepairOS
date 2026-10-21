package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"autorepairos/internal/config"
	"autorepairos/internal/cron"
	"autorepairos/internal/db"
	"autorepairos/internal/handler"
	"autorepairos/internal/integration/vision"
	"autorepairos/internal/seed"
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

	mux := http.NewServeMux()

	// 未登入
	mux.HandleFunc("GET /login", handler.LoginHandler)
	mux.HandleFunc("POST /login", handler.LoginHandler)
	mux.HandleFunc("POST /logout", handler.LogoutHandler)
	mux.HandleFunc("GET /api/me", handler.MeHandler)
	mux.HandleFunc("GET /api/vision/status", handler.RequireLogin(handler.VisionStatus))
	mux.HandleFunc("POST /api/vision/recognize", handler.CounterUp(handler.RecognizePlate))
	mux.HandleFunc("POST /api/vision/invoice", handler.CounterUp(handler.RecognizeInvoice))
	mux.HandleFunc("GET /api/suppliers", handler.RequireLogin(handler.SupplierOptionsAPI))
	mux.HandleFunc("POST /api/suppliers/quick-create", handler.CounterUp(handler.QuickCreateSupplier))
	mux.HandleFunc("GET /api/parts", handler.RequireLogin(handler.SearchPartsAPI))
	mux.HandleFunc("GET /api/customers/search", handler.RequireLogin(handler.SearchCustomersAPI))
	mux.HandleFunc("GET /api/inventory/search", handler.AllStaff(handler.SearchInventoryAPI))
	mux.HandleFunc("GET /api/work-orders/search", handler.RequireLogin(handler.SearchWorkOrdersAPI))
	mux.HandleFunc("GET /api/vehicles/search", handler.RequireLogin(handler.SearchVehiclesAPI))
	mux.HandleFunc("GET /api/lookup/search", handler.RequireLogin(handler.SearchLookupAPI))
	mux.HandleFunc("GET /api/insurance/search", handler.RequireLogin(handler.SearchInsuranceAPI))
	mux.HandleFunc("GET /api/suppliers/search", handler.RequireLogin(handler.SearchSuppliersAPI))
	mux.HandleFunc("GET /api/appointments/search", handler.RequireLogin(handler.SearchAppointmentsAPI))
	mux.HandleFunc("GET /api/appointments/calendar", handler.AllStaff(handler.AppointmentCalendarAPI))
	mux.HandleFunc("GET /api/subcontracts/search", handler.RequireLogin(handler.SearchSubcontractsAPI))
	mux.HandleFunc("GET /api/equipment/search", handler.RequireLogin(handler.SearchEquipmentAPI))
	mux.HandleFunc("GET /api/staff/search", handler.RequireLogin(handler.SearchStaffAPI))
	mux.HandleFunc("GET /api/receivables/search", handler.RequireLogin(handler.SearchReceivablesAPI))
	mux.HandleFunc("GET /api/insurances/search", handler.RequireLogin(handler.SearchInsurancesAPI))
	mux.HandleFunc("GET /api/insurances/expiring", handler.RequireLogin(handler.ExpiringInsurancesAPI))
	mux.HandleFunc("GET /api/inspections/search", handler.RequireLogin(handler.SearchInspectionsAPI))
	mux.HandleFunc("GET /api/expenses/search", handler.RequireLogin(handler.SearchExpensesAPI))

	// LINE webhook：不用登入，靠 X-Line-Signature 驗簽。
	// 網址需經 Cloudflare Tunnel 對外公開，例如 https://<hostname>/webhook/line
	mux.HandleFunc("POST /webhook/line", handler.LineWebhook)

	// T2.11: 追加維修授權（公開頁面，客戶透過 LINE 連結存取）
	mux.HandleFunc("GET /approval/{token}", handler.ApprovalPageHandler)
	mux.HandleFunc("POST /approval/approve/{token}", handler.ApproveRequestHandler)
	mux.HandleFunc("POST /approval/reject/{token}", handler.RejectRequestHandler)

	// T2.10: LINE 數位維修履歷與保養收據（公開 API，客戶透過 LINE 連結存取）
	// 身份驗證靠 customers.line_user_id 綁定，不是登入 session。
	mux.HandleFunc("GET /api/passbook", handler.PassbookAPI)

	// 總覽
	mux.HandleFunc("GET /{$}", handler.AllStaff(handler.IndexHandler))

	// 首頁（index.html 用 template 注入 APP_NAME，需登入才能取得）
	mux.HandleFunc("GET /index.html", handler.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("web/index.html")
		if err != nil {
			http.Error(w, "template parse error", http.StatusInternalServerError)
			return
		}
		tmpl.Execute(w, map[string]string{"AppName": cfg.AppName})
	})))

	// 客戶
	mux.HandleFunc("GET /customers", handler.AllStaff(handler.ListCustomers))
	mux.HandleFunc("POST /customers", handler.CounterUp(handler.CreateCustomer))
	mux.HandleFunc("GET /customers/{id}/edit", handler.AllStaff(handler.EditCustomerForm))
	mux.HandleFunc("POST /customers/{id}", handler.CounterUp(handler.UpdateCustomer))
	mux.HandleFunc("POST /customers/{id}/delete", handler.OwnerOnly(handler.DeleteCustomer))
	// LINE / Telegram 綁定：獨立端點而不是編輯表單的欄位。
	// 表單直接 UPDATE 會繞過唯一性檢查，讓同一個 LINE 帳號綁到兩個客戶，
	// 進而讓對方在 LINE 查到不屬於他的維修紀錄（詳見 customer_binding.go）。
	mux.HandleFunc("POST /customers/{id}/line-bind", handler.CounterUp(handler.BindCustomerLine))
	mux.HandleFunc("POST /customers/{id}/line-unbind", handler.CounterUp(handler.UnbindCustomerLine))
	mux.HandleFunc("POST /customers/{id}/telegram-unbind", handler.CounterUp(handler.UnbindCustomerTelegram))

	// 車輛
	mux.HandleFunc("GET /vehicles", handler.AllStaff(handler.ListVehicles))
	mux.HandleFunc("POST /vehicles", handler.CounterUp(handler.CreateVehicle))
	mux.HandleFunc("GET /vehicles/{id}/edit", handler.AllStaff(handler.EditVehicleForm))
	mux.HandleFunc("POST /vehicles/{id}", handler.CounterUp(handler.UpdateVehicle))
	mux.HandleFunc("POST /vehicles/{id}/delete", handler.OwnerOnly(handler.DeleteVehicle))

	// 車輛履歷
	mux.HandleFunc("GET /vehicles/{id}", handler.AllStaff(handler.ShowVehicle))

	// 工單
	mux.HandleFunc("GET /work-orders", handler.AllStaff(handler.ListWorkOrders))
	mux.HandleFunc("GET /work-orders/new", handler.CounterUpPage(handler.NewWorkOrder))
	mux.HandleFunc("POST /work-orders", handler.CounterUp(handler.CreateWorkOrder))
	mux.HandleFunc("POST /api/quick-vehicle", handler.CounterUp(handler.QuickVehicle))
	mux.HandleFunc("GET /work-orders/{id}", handler.AllStaff(handler.ShowWorkOrder))
	mux.HandleFunc("POST /work-orders/{id}/status", handler.CounterUp(handler.UpdateWorkOrderStatus))
	mux.HandleFunc("POST /work-orders/{id}/approval", handler.CounterUp(handler.UpdateWorkOrderApproval))
	mux.HandleFunc("POST /work-orders/{id}/claim", handler.CounterUp(handler.UpdateWorkOrderClaim))
	mux.HandleFunc("POST /work-orders/{id}/sop", handler.CounterUp(handler.ToggleWorkOrderSOP))
	mux.HandleFunc("POST /work-orders/{id}/items", handler.CounterUp(handler.AddWorkOrderItem))
	mux.HandleFunc("POST /work-orders/{id}/items/{item_id}/delete", handler.CounterUp(handler.DeleteWorkOrderItem))

	// T2.9：獨立估價草稿。估價寫入與轉換限櫃檯、管理員。
	mux.HandleFunc("GET /estimates", handler.AllStaff(handler.ListEstimates))
	mux.HandleFunc("GET /estimates/new", handler.CounterUpPage(handler.NewEstimate))
	mux.HandleFunc("POST /estimates", handler.CounterUp(handler.CreateEstimate))
	mux.HandleFunc("GET /estimates/{id}", handler.AllStaff(handler.ShowEstimate))
	mux.HandleFunc("POST /estimates/{id}/items", handler.CounterUp(handler.AddEstimateItem))
	mux.HandleFunc("POST /estimates/{id}/items/{item_id}/delete", handler.CounterUp(handler.DeleteEstimateItem))
	mux.HandleFunc("POST /estimates/{id}/discount", handler.CounterUp(handler.SetEstimateDiscount))
	mux.HandleFunc("POST /estimates/{id}/convert", handler.CounterUp(handler.ConvertEstimate))
	mux.HandleFunc("GET /work-orders/{id}/estimate-source", handler.AllStaff(handler.ShowWorkOrderEstimateSource))

	// 庫存
	mux.HandleFunc("GET /inventory", handler.AllStaff(handler.ListInventory))
	mux.HandleFunc("POST /inventory", handler.CounterUp(handler.CreateInventoryItem))
	mux.HandleFunc("GET /inventory/{id}/edit", handler.AllStaff(handler.EditInventoryForm))
	mux.HandleFunc("POST /inventory/{id}", handler.CounterUp(handler.UpdateInventoryItem))
	mux.HandleFunc("GET /inventory/{id}/stock", handler.AllStaff(handler.StockAdjustForm))
	mux.HandleFunc("POST /inventory/{id}/stock", handler.CounterUp(handler.ApplyStockAdjust))
	mux.HandleFunc("POST /inventory/{id}/delete", handler.OwnerOnly(handler.DeleteInventoryItem))

	// 庫存異動紀錄與舊系統負庫存補正。
	//
	// 補正表單刻意走 query 參數（/inventory/negative?id=N）而非
	// /inventory/negative/{id}：後者與既有的 /inventory/{id}/edit 同為三段路徑，
	// Go 1.22 ServeMux 判定互不更明確，會在啟動時 panic。
	mux.HandleFunc("GET /inventory/movements", handler.AllStaff(handler.MovementLog))
	mux.HandleFunc("GET /inventory/negative", handler.AllStaff(handler.NegativeStockList))
	mux.HandleFunc("POST /inventory/negative/fix", handler.CounterUpPage(handler.ApplyNegativeStockFix))

	// 廠商（材料商 / 協力廠商）
	mux.HandleFunc("GET /suppliers", handler.AllStaff(handler.ListSuppliers))
	mux.HandleFunc("POST /suppliers", handler.CounterUp(handler.CreateSupplier))
	mux.HandleFunc("GET /suppliers/{id}/edit", handler.CounterUpPage(handler.EditSupplierForm))
	mux.HandleFunc("POST /suppliers/{id}", handler.CounterUp(handler.UpdateSupplier))
	mux.HandleFunc("POST /suppliers/{id}/deactivate", handler.OwnerOnly(handler.DeactivateSupplier))
	mux.HandleFunc("POST /suppliers/{id}/reactivate", handler.OwnerOnly(handler.ReactivateSupplier))

	// 員工與健保資料（含健保眷屬）
	mux.HandleFunc("GET /staff", handler.AllStaff(handler.ListStaff))
	mux.HandleFunc("POST /staff", handler.CounterUpPage(handler.CreateStaff))
	mux.HandleFunc("GET /staff/{id}/edit", handler.CounterUpPage(handler.EditStaffForm))
	mux.HandleFunc("POST /staff/{id}", handler.CounterUp(handler.UpdateStaff))
	mux.HandleFunc("POST /staff/{id}/deactivate", handler.OwnerOnly(handler.DeactivateStaff))
	mux.HandleFunc("POST /staff/{id}/reactivate", handler.OwnerOnly(handler.ReactivateStaff))

	// 技師片語庫（T2.7）
	mux.HandleFunc("GET /phrases", handler.AllStaff(handler.ListPhrases))
	mux.HandleFunc("POST /phrases/new", handler.AllStaff(handler.CreatePhrase))
	mux.HandleFunc("GET /phrases/{id}/edit", handler.AllStaff(handler.EditPhraseForm))
	mux.HandleFunc("POST /phrases/{id}/update", handler.AllStaff(handler.UpdatePhrase))
	mux.HandleFunc("POST /phrases/{id}/delete", handler.AllStaff(handler.DeletePhrase))
	mux.HandleFunc("GET /api/phrases/search", handler.RequireLogin(handler.SearchPhrasesAPI))

	// T2.8: 照片標記 + 舊傷預檢
	mux.HandleFunc("POST /api/photos/{photo_id}/annotations", handler.AllStaff(handler.AddPhotoAnnotation))
	mux.HandleFunc("GET /api/photos/{photo_id}/annotations", handler.AllStaff(handler.ListPhotoAnnotations))
	mux.HandleFunc("POST /api/annotations/{annotation_id}/delete", handler.AllStaff(handler.DeletePhotoAnnotation))
	mux.HandleFunc("POST /work-orders/{id}/walkaround-marks", handler.AllStaff(handler.AddWalkaroundMark))
	mux.HandleFunc("GET /work-orders/{id}/walkaround-marks", handler.AllStaff(handler.ListWalkaroundMarks))
	mux.HandleFunc("POST /work-orders/{id}/walkaround-marks/{mark_id}/delete", handler.AllStaff(handler.DeleteWalkaroundMark))

	// 舊系統補遷移：車輛投保紀錄 / 定檢排程 / 保養套餐 / 零用金
	mux.HandleFunc("GET /insurances", handler.AllStaff(handler.ListVehicleInsurances))
	mux.HandleFunc("GET /insurances/expiring", handler.AllStaff(handler.ListVehicleInsurances))
	mux.HandleFunc("GET /insurances/new", handler.CounterUpPage(handler.NewVehicleInsuranceForm))
	mux.HandleFunc("POST /insurances", handler.CounterUp(handler.CreateVehicleInsurance))
	mux.HandleFunc("GET /insurances/{id}/edit", handler.CounterUpPage(handler.EditVehicleInsuranceForm))
	mux.HandleFunc("POST /insurances/{id}", handler.CounterUp(handler.UpdateVehicleInsurance))
	mux.HandleFunc("POST /insurances/{id}/delete", handler.CounterUp(handler.DeleteVehicleInsurance))
	mux.HandleFunc("GET /inspections", handler.AllStaff(handler.ListVehicleInspections))
	mux.HandleFunc("GET /inspections/new", handler.CounterUpPage(handler.NewVehicleInspectionForm))
	mux.HandleFunc("POST /inspections", handler.CounterUp(handler.CreateVehicleInspection))
	mux.HandleFunc("GET /inspections/{id}/edit", handler.CounterUpPage(handler.EditVehicleInspectionForm))
	mux.HandleFunc("POST /inspections/{id}", handler.CounterUp(handler.UpdateVehicleInspection))
	mux.HandleFunc("POST /inspections/{id}/delete", handler.CounterUp(handler.DeleteVehicleInspection))
	mux.HandleFunc("GET /maintenance-templates", handler.AllStaff(handler.ListMaintenanceTemplates))
	// 保養套餐範本 CRUD（項目整批重寫）
	mux.HandleFunc("GET /maintenance-templates/new", handler.CounterUpPage(handler.NewMaintenanceTemplateForm))
	mux.HandleFunc("POST /maintenance-templates", handler.CounterUp(handler.CreateMaintenanceTemplate))
	mux.HandleFunc("GET /maintenance-templates/{id}/edit", handler.CounterUpPage(handler.EditMaintenanceTemplateForm))
	mux.HandleFunc("POST /maintenance-templates/{id}", handler.CounterUp(handler.UpdateMaintenanceTemplate))
	mux.HandleFunc("POST /maintenance-templates/{id}/items", handler.CounterUp(handler.ReplaceMaintenanceTemplateItems))
	mux.HandleFunc("POST /maintenance-templates/{id}/delete", handler.CounterUp(handler.DeleteMaintenanceTemplate))
	mux.HandleFunc("GET /expenses", handler.CounterUpPage(handler.ListExpenses))

	// 收款沖銷（CAR35 Customer Pay，90,762 筆）——
	// 財務頁面，用 CounterUpPage 讓技師看不到（handler 內另有一次 canSeeMoney 檢查）。
	mux.HandleFunc("GET /receivables", handler.CounterUpPage(handler.ListReceivables))
	mux.HandleFunc("GET /receivables/new", handler.CounterUpPage(handler.NewReceivableForm))
	mux.HandleFunc("POST /receivables", handler.CounterUp(handler.CreateReceivable))
	mux.HandleFunc("GET /receivables/{id}/edit", handler.CounterUpPage(handler.EditReceivableForm))
	mux.HandleFunc("POST /receivables/{id}", handler.CounterUp(handler.UpdateReceivable))
	mux.HandleFunc("POST /receivables/{id}/void", handler.OwnerOnly(handler.VoidReceivable))

	// 保險公司（CAR35 P0-1：Company 表，理賠案件歸屬對象）
	mux.HandleFunc("GET /insurance-companies", handler.AllStaff(handler.ListInsuranceCompanies))
	mux.HandleFunc("POST /insurance-companies", handler.CounterUp(handler.CreateInsuranceCompany))
	mux.HandleFunc("GET /insurance-companies/{id}/edit", handler.CounterUpPage(handler.EditInsuranceCompanyForm))
	mux.HandleFunc("POST /insurance-companies/{id}", handler.CounterUp(handler.UpdateInsuranceCompany))
	mux.HandleFunc("POST /insurance-companies/{id}/delete", handler.OwnerOnly(handler.DeactivateInsuranceCompany))

	// Phase 2: 技師工時/佣金
	mux.HandleFunc("GET /work-orders/{id}/worklogs", handler.AllStaff(handler.ListTechnicianWorklogs))
	mux.HandleFunc("POST /work-orders/{id}/worklogs", handler.WithActor(handler.AddTechnicianWorklog))
	mux.HandleFunc("GET /work-orders/{id}/worklogs/{log_id}/edit", handler.CounterUpPage(handler.EditTechnicianWorklogForm))
	mux.HandleFunc("POST /work-orders/{id}/worklogs/{log_id}", handler.CounterUp(handler.UpdateTechnicianWorklog))
	mux.HandleFunc("POST /work-orders/{id}/worklogs/{log_id}/delete", handler.CounterUp(handler.DeleteTechnicianWorklog))
	mux.HandleFunc("GET /technicians/commissions", handler.AllStaff(handler.ListTechnicianCommissions))

	// Phase 2: 收據列印
	mux.HandleFunc("GET /work-orders/{id}/receipt", handler.AllStaff(handler.PrintWorkOrderReceipt))
	// 估價單（FR-PRINT-01）：?paper=a4|thermal。與結帳收據刻意分開——
	// 估價單有有效期與客戶簽名欄，收據是收款憑證，兩者用途不同。
	mux.HandleFunc("GET /work-orders/{id}/quotation", handler.CounterUpPage(handler.PrintWorkOrderQuotation))
	// 完工證明（FR-PRINT-02）：交車時給客戶的「做了什麼」證明，不含金額。
	mux.HandleFunc("GET /work-orders/{id}/completion", handler.AllStaff(handler.PrintWorkOrderCompletion))

	// 收款明細含稅版（FR-PRINT-03）：財務用，支付方式與稅額分列。
	mux.HandleFunc("GET /receivables/{id}/print", handler.CounterUp(handler.PrintReceiptWithTax))
	// AR 催款單（FR-PRINT-04）
	mux.HandleFunc("GET /customers/{id}/ar-statement", handler.CounterUpPage(handler.PrintARStatement))
	// AP 月結對帳單（FR-PRINT-05）
	mux.HandleFunc("GET /suppliers/{id}/ap-statement", handler.CounterUpPage(handler.PrintAPStatement))
	// T2.12：供應商應付帳款、帳齡、付款沖銷與可列印／匯出的月結對帳單。
	mux.HandleFunc("GET /ap", handler.CounterUpPage(handler.APHome))
	mux.HandleFunc("GET /ap/statement", handler.CounterUpPage(handler.APStatement))
	mux.HandleFunc("GET /ap/statement.csv", handler.CounterUp(handler.APStatementCSV))
	mux.HandleFunc("POST /ap/{id}/payments", handler.CounterUp(handler.RecordAPPayment))
	mux.HandleFunc("POST /ap/{id}/void", handler.OwnerOnly(handler.VoidAPPayable))
	// 日結報表（FR-PRINT-06）
	mux.HandleFunc("GET /reports/daily", handler.CounterUpPage(handler.PrintDailyReport))

	// Phase 2: 照片上傳
	mux.HandleFunc("GET /work-orders/{id}/photos", handler.AllStaff(handler.ListWorkOrderPhotos))
	mux.HandleFunc("POST /work-orders/{id}/photos", handler.CounterUp(handler.UploadWorkOrderPhoto))
	mux.HandleFunc("POST /work-orders/{id}/photos/{photo_id}/delete", handler.CounterUp(handler.DeleteWorkOrderPhoto))
	mux.HandleFunc("GET /photos/{filename}", handler.RequireLogin(handler.ServePhoto))

	// Phase 3: 排程 + 報表
	mux.HandleFunc("GET /lookup", handler.CounterUpPage(handler.ListLookupItems))
	mux.HandleFunc("GET /lookup/new", handler.CounterUpPage(handler.NewLookupItemForm))
	mux.HandleFunc("POST /lookup", handler.CounterUp(handler.CreateLookupItem))
	mux.HandleFunc("GET /lookup/{id}/edit", handler.CounterUpPage(handler.EditLookupItemForm))
	mux.HandleFunc("POST /lookup/{id}", handler.CounterUp(handler.UpdateLookupItem))
	mux.HandleFunc("POST /lookup/{id}/deactivate", handler.OwnerOnly(handler.DeactivateLookupItem))
	mux.HandleFunc("POST /lookup/{id}/reactivate", handler.OwnerOnly(handler.ReactivateLookupItem))
	mux.HandleFunc("GET /scheduled-tasks", handler.AllStaff(handler.ListScheduledTasks))
	mux.HandleFunc("POST /scheduled-tasks/{key}/toggle", handler.OwnerOnly(handler.ToggleScheduledTask))
	mux.HandleFunc("POST /scheduled-tasks/backups/run", handler.OwnerOnly(handler.RunBackupNow))
	mux.HandleFunc("POST /scheduled-tasks/notifications/run", handler.OwnerOnly(handler.RunNotificationsNow))
	mux.HandleFunc("POST /scheduled-tasks/maintenance-due/run", handler.OwnerOnly(handler.RunMaintenanceScanNow))
	mux.HandleFunc("POST /scheduled-tasks/work-order-ready/run", handler.OwnerOnly(handler.RunWorkOrderReadyNow))
	mux.HandleFunc("POST /notifications/{id}/cancel", handler.OwnerOnly(handler.CancelNotification))
	mux.HandleFunc("GET /reports", handler.AllStaff(handler.ReportsHome))
	mux.HandleFunc("GET /reports/export", handler.AllStaff(handler.ReportsExport))

	// 經營分析（SRS 5.12）：KPI、損益分拆、留存、方案成效、死頭料、CSV 匯出
	mux.HandleFunc("GET /analytics", handler.CounterUpPage(handler.AnalyticsHome))
	mux.HandleFunc("GET /analytics/export", handler.CounterUp(handler.AnalyticsExport))

	// 預約排程（SRS 5.9）
	mux.HandleFunc("GET /appointments", handler.AllStaff(handler.ListAppointments))
	mux.HandleFunc("POST /appointments", handler.CounterUp(handler.CreateAppointment))
	mux.HandleFunc("GET /appointments/{id}/edit", handler.AllStaff(handler.EditAppointmentForm))
	mux.HandleFunc("POST /appointments/{id}", handler.CounterUp(handler.UpdateAppointment))
	mux.HandleFunc("POST /appointments/{id}/delete", handler.OwnerOnly(handler.DeleteAppointment))

	// 車輛里程履歷（FR-VH-03）：進廠工單自動帶入，也可手動登錄
	mux.HandleFunc("GET /vehicles/{id}/mileage/new", handler.CounterUpPage(handler.AddVehicleMileageForm))
	mux.HandleFunc("POST /vehicles/{id}/mileage", handler.CounterUp(handler.CreateVehicleMileage))

	// 外包加工（SRS 5.11）
	mux.HandleFunc("GET /subcontracts", handler.AllStaff(handler.ListSubcontracts))
	mux.HandleFunc("POST /subcontracts", handler.CounterUp(handler.CreateSubcontract))
	mux.HandleFunc("GET /subcontracts/{id}/edit", handler.AllStaff(handler.EditSubcontractForm))
	mux.HandleFunc("POST /subcontracts/{id}", handler.CounterUp(handler.UpdateSubcontract))
	mux.HandleFunc("POST /subcontracts/{id}/delete", handler.OwnerOnly(handler.DeleteSubcontract))

	// 設備管理（SRS 5.11）
	mux.HandleFunc("GET /equipment", handler.AllStaff(handler.ListEquipment))
	mux.HandleFunc("POST /equipment", handler.CounterUp(handler.CreateEquipment))
	mux.HandleFunc("GET /equipment/{id}/edit", handler.AllStaff(handler.EditEquipmentForm))
	mux.HandleFunc("POST /equipment/{id}", handler.CounterUp(handler.UpdateEquipment))
	mux.HandleFunc("POST /equipment/{id}/delete", handler.OwnerOnly(handler.DeleteEquipment))
	mux.HandleFunc("GET /equipment/{id}/maintenance", handler.AllStaff(handler.ListEquipmentMaintenance))
	mux.HandleFunc("POST /equipment/{id}/maintenance", handler.CounterUp(handler.CreateEquipmentMaintenance))
	mux.HandleFunc("POST /equipment/{id}/maintenance/{maint_id}/delete", handler.OwnerOnly(handler.DeleteEquipmentMaintenance))

	// 進貨單（採購進貨：品項 + 應付總額）
	mux.HandleFunc("GET /purchase-orders", handler.AllStaff(handler.ListPurchaseOrders))
	mux.HandleFunc("POST /purchase-orders", handler.CounterUp(handler.CreatePurchaseOrder))
	mux.HandleFunc("GET /purchase-orders/new", handler.CounterUpPage(handler.NewPurchaseOrderForm))
	mux.HandleFunc("GET /purchase-orders/{id}", handler.AllStaff(handler.ShowPurchaseOrder))
	mux.HandleFunc("POST /purchase-orders/{id}/items", handler.CounterUp(handler.AddPurchaseOrderItem))
	mux.HandleFunc("DELETE /purchase-orders/{id}/items/{itemId}", handler.CounterUp(handler.DeletePurchaseOrderItem))
	mux.HandleFunc("POST /purchase-orders/{id}/receive", handler.CounterUp(handler.ReceivePurchaseOrder))
	mux.HandleFunc("POST /purchase-orders/{id}/delete", handler.CounterUp(handler.DeletePurchaseOrder))

	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	handler.RegisterStatic(mux)

	// 啟動排程器（通知佇列、每日備份、保養到期掃描）
	scheduler, err := cron.New()
	if err != nil {
		log.Printf("排程器初始化失敗（功能停用）: %v", err)
	} else {
		scheduler.Start()
		defer scheduler.Stop()
	}

	// shutdown 是所有退出路徑共用的清理流程。
	// 為什麼不能用 defer：本程式所有現實退出路徑都經過 os.Exit——
	//   (1) signal handler 的 os.Exit(0)（Ctrl+C / SIGTERM / docker stop）
	//   (2) log.Fatal 的 os.Exit(1)（ListenAndServe 綁定失敗等致命錯誤）
	//   (3) 被工作管理員強殺（什麼都跑不到，靠 vision 的 Job Object 父死子亡兜底）
	// os.Exit 一律跳過 defer，所以 defer db.Close() 在實務上不會被執行到；
	// 清理必須顯式呼叫 shutdown()。db.Close() 內部會先 wal_checkpoint(TRUNCATE)
	// 把 WAL 寫回主庫，再關連線池（SQLite crash-safe，即使沒跑到資料也不會丟）。
	shutdown := func() {
		log.Printf("正在關閉服務，清理資源…")
		handler.StopVisionClient()
		if scheduler != nil {
			scheduler.Stop()
		}
		db.Close()
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
	if err := http.ListenAndServe(":"+port, handler.CSRFProtection(mux)); err != nil {
		// 用 log.Printf + shutdown + os.Exit 取代 log.Fatal：
		// log.Fatal 內部是 os.Exit(1)，同樣會跳過 defer，DB 不會被關閉。
		log.Printf("HTTP 服務結束: %v", err)
		shutdown()
		os.Exit(1)
	}
}
