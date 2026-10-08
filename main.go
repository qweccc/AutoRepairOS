package main

import (
	"html/template"
	"log"
	"net/http"

	"autorepairos/internal/config"
	"autorepairos/internal/cron"
	"autorepairos/internal/db"
	"autorepairos/internal/handler"
	"autorepairos/internal/seed"
)

func main() {
	cfg := config.Load()

	if err := db.Init(cfg); err != nil {
		log.Fatalf("資料庫初始化失敗: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Migration 執行失敗: %v", err)
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
	mux.HandleFunc("GET /logout", handler.LogoutHandler)
	mux.HandleFunc("GET /api/me", handler.MeHandler)

	// LINE webhook：不用登入，靠 X-Line-Signature 驗簽。
	// 網址需經 Cloudflare Tunnel 對外公開，例如 https://<hostname>/webhook/line
	mux.HandleFunc("POST /webhook/line", handler.LineWebhook)

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
	mux.HandleFunc("GET /customers/{id}/delete", handler.OwnerOnly(handler.DeleteCustomer))

	// 車輛
	mux.HandleFunc("GET /vehicles", handler.AllStaff(handler.ListVehicles))
	mux.HandleFunc("POST /vehicles", handler.CounterUp(handler.CreateVehicle))
	mux.HandleFunc("GET /vehicles/{id}/edit", handler.AllStaff(handler.EditVehicleForm))
	mux.HandleFunc("POST /vehicles/{id}", handler.CounterUp(handler.UpdateVehicle))
	mux.HandleFunc("GET /vehicles/{id}/delete", handler.OwnerOnly(handler.DeleteVehicle))

	// 車輛履歷
	mux.HandleFunc("GET /vehicles/{id}", handler.AllStaff(handler.ShowVehicle))

	// 工單
	mux.HandleFunc("GET /work-orders", handler.AllStaff(handler.ListWorkOrders))
	mux.HandleFunc("GET /work-orders/new", handler.CounterUpPage(handler.NewWorkOrder))
	mux.HandleFunc("POST /work-orders", handler.CounterUp(handler.CreateWorkOrder))
	mux.HandleFunc("GET /work-orders/{id}", handler.AllStaff(handler.ShowWorkOrder))
	mux.HandleFunc("POST /work-orders/{id}/status", handler.CounterUp(handler.UpdateWorkOrderStatus))
	mux.HandleFunc("POST /work-orders/{id}/approval", handler.CounterUp(handler.UpdateWorkOrderApproval))
	mux.HandleFunc("POST /work-orders/{id}/sop", handler.CounterUp(handler.ToggleWorkOrderSOP))
	mux.HandleFunc("POST /work-orders/{id}/items", handler.CounterUp(handler.AddWorkOrderItem))
	mux.HandleFunc("GET /work-orders/{id}/items/{item_id}/delete", handler.CounterUp(handler.DeleteWorkOrderItem))

	// 庫存
	mux.HandleFunc("GET /inventory", handler.AllStaff(handler.ListInventory))
	mux.HandleFunc("POST /inventory", handler.CounterUp(handler.CreateInventoryItem))
	mux.HandleFunc("GET /inventory/{id}/edit", handler.AllStaff(handler.EditInventoryForm))
	mux.HandleFunc("POST /inventory/{id}", handler.CounterUp(handler.UpdateInventoryItem))
	mux.HandleFunc("GET /inventory/{id}/stock", handler.AllStaff(handler.StockAdjustForm))
	mux.HandleFunc("POST /inventory/{id}/stock", handler.CounterUp(handler.ApplyStockAdjust))
	mux.HandleFunc("GET /inventory/{id}/delete", handler.OwnerOnly(handler.DeleteInventoryItem))

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

	// 員工與健保資料（含健保眷屬）
	mux.HandleFunc("GET /staff", handler.AllStaff(handler.ListStaff))

	// 舊系統補遷移：車輛投保紀錄 / 定檢排程 / 保養套餐 / 零用金
	mux.HandleFunc("GET /insurances", handler.AllStaff(handler.ListVehicleInsurances))
	mux.HandleFunc("GET /inspections", handler.AllStaff(handler.ListVehicleInspections))
	mux.HandleFunc("GET /maintenance-templates", handler.AllStaff(handler.ListMaintenanceTemplates))
	mux.HandleFunc("GET /expenses", handler.CounterUpPage(handler.ListExpenses))

	// 收款沖銷（CAR35 Customer Pay，90,762 筆）——
	// 財務頁面，用 CounterUpPage 讓技師看不到（handler 內另有一次 canSeeMoney 檢查）。
	mux.HandleFunc("GET /receivables", handler.CounterUpPage(handler.ListReceivables))
	mux.HandleFunc("GET /receivables/new", handler.CounterUpPage(handler.NewReceivableForm))
	mux.HandleFunc("POST /receivables", handler.CounterUp(handler.CreateReceivable))
	mux.HandleFunc("GET /receivables/{id}/void", handler.OwnerOnly(handler.VoidReceivable))

	// 保險公司（CAR35 P0-1：Company 表，理賠案件歸屬對象）
	mux.HandleFunc("GET /insurance-companies", handler.AllStaff(handler.ListInsuranceCompanies))
	mux.HandleFunc("POST /insurance-companies", handler.CounterUp(handler.CreateInsuranceCompany))
	mux.HandleFunc("GET /insurance-companies/{id}/edit", handler.CounterUpPage(handler.EditInsuranceCompanyForm))
	mux.HandleFunc("POST /insurance-companies/{id}", handler.CounterUp(handler.UpdateInsuranceCompany))
	mux.HandleFunc("GET /insurance-companies/{id}/delete", handler.OwnerOnly(handler.DeactivateInsuranceCompany))

	// Phase 2: 技師工時/佣金
	mux.HandleFunc("GET /work-orders/{id}/worklogs", handler.AllStaff(handler.ListTechnicianWorklogs))
	mux.HandleFunc("POST /work-orders/{id}/worklogs", handler.WithActor(handler.AddTechnicianWorklog))
	mux.HandleFunc("GET /work-orders/{id}/worklogs/{log_id}/delete", handler.CounterUp(handler.DeleteTechnicianWorklog))
	mux.HandleFunc("GET /technicians/commissions", handler.AllStaff(handler.ListTechnicianCommissions))

	// Phase 2: 收據列印
	mux.HandleFunc("GET /work-orders/{id}/receipt", handler.AllStaff(handler.PrintWorkOrderReceipt))

	// Phase 2: 照片上傳
	mux.HandleFunc("GET /work-orders/{id}/photos", handler.AllStaff(handler.ListWorkOrderPhotos))
	mux.HandleFunc("POST /work-orders/{id}/photos", handler.CounterUp(handler.UploadWorkOrderPhoto))
	mux.HandleFunc("GET /work-orders/{id}/photos/{photo_id}/delete", handler.CounterUp(handler.DeleteWorkOrderPhoto))
	mux.HandleFunc("GET /photos/{filename}", handler.RequireLogin(handler.ServePhoto))

	// Phase 3: 排程 + 報表
	mux.HandleFunc("GET /scheduled-tasks", handler.AllStaff(handler.ListScheduledTasks))
	mux.HandleFunc("GET /scheduled-tasks/{key}/toggle", handler.OwnerOnly(handler.ToggleScheduledTask))
	mux.HandleFunc("GET /scheduled-tasks/backups/run", handler.OwnerOnly(handler.RunBackupNow))
	mux.HandleFunc("GET /scheduled-tasks/notifications/run", handler.OwnerOnly(handler.RunNotificationsNow))
	mux.HandleFunc("GET /scheduled-tasks/maintenance-due/run", handler.OwnerOnly(handler.RunMaintenanceScanNow))
	mux.HandleFunc("GET /scheduled-tasks/work-order-ready/run", handler.OwnerOnly(handler.RunWorkOrderReadyNow))
	mux.HandleFunc("GET /notifications/{id}/cancel", handler.OwnerOnly(handler.CancelNotification))
	mux.HandleFunc("GET /reports", handler.AllStaff(handler.ReportsHome))

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

	log.Printf("%s 啟動中，監聽 port: %s", cfg.AppName, port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
