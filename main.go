package main

import (
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

	// 總覽
	mux.HandleFunc("GET /{$}", handler.RequireAuth(handler.IndexHandler))

	// 首頁靜態（index.html 字面檔，需登入才能取得）
	mux.HandleFunc("GET /index.html", handler.RequireLogin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix("/index.html", http.FileServer(http.Dir("web"))).ServeHTTP(w, r)
	})))

	// 客戶
	mux.HandleFunc("GET /customers", handler.RequireAuth(handler.ListCustomers))
	mux.HandleFunc("POST /customers", handler.RequireLogin(handler.CreateCustomer))
	mux.HandleFunc("GET /customers/{id}/edit", handler.RequireAuth(handler.EditCustomerForm))
	mux.HandleFunc("POST /customers/{id}", handler.RequireLogin(handler.UpdateCustomer))
	mux.HandleFunc("GET /customers/{id}/delete", handler.RequireLogin(handler.DeleteCustomer))

	// 車輛
	mux.HandleFunc("GET /vehicles", handler.RequireAuth(handler.ListVehicles))
	mux.HandleFunc("POST /vehicles", handler.RequireLogin(handler.CreateVehicle))
	mux.HandleFunc("GET /vehicles/{id}/edit", handler.RequireAuth(handler.EditVehicleForm))
	mux.HandleFunc("POST /vehicles/{id}", handler.RequireLogin(handler.UpdateVehicle))
	mux.HandleFunc("GET /vehicles/{id}/delete", handler.RequireLogin(handler.DeleteVehicle))

	// 工單
	mux.HandleFunc("GET /work-orders", handler.RequireAuth(handler.ListWorkOrders))
	mux.HandleFunc("POST /work-orders", handler.RequireLogin(handler.CreateWorkOrder))
	mux.HandleFunc("GET /work-orders/{id}", handler.RequireAuth(handler.ShowWorkOrder))
	mux.HandleFunc("POST /work-orders/{id}/status", handler.RequireLogin(handler.UpdateWorkOrderStatus))
	mux.HandleFunc("POST /work-orders/{id}/approval", handler.RequireLogin(handler.UpdateWorkOrderApproval))
	mux.HandleFunc("POST /work-orders/{id}/sop", handler.RequireLogin(handler.ToggleWorkOrderSOP))
	mux.HandleFunc("POST /work-orders/{id}/items", handler.RequireLogin(handler.AddWorkOrderItem))
	mux.HandleFunc("GET /work-orders/{id}/items/{item_id}/delete", handler.RequireLogin(handler.DeleteWorkOrderItem))

	// 庫存
	mux.HandleFunc("GET /inventory", handler.RequireAuth(handler.ListInventory))
	mux.HandleFunc("POST /inventory", handler.RequireLogin(handler.CreateInventoryItem))
	mux.HandleFunc("GET /inventory/{id}/edit", handler.RequireAuth(handler.EditInventoryForm))
	mux.HandleFunc("POST /inventory/{id}", handler.RequireLogin(handler.UpdateInventoryItem))
	mux.HandleFunc("GET /inventory/{id}/stock", handler.RequireAuth(handler.StockAdjustForm))
	mux.HandleFunc("POST /inventory/{id}/stock", handler.RequireLogin(handler.ApplyStockAdjust))
	mux.HandleFunc("GET /inventory/{id}/delete", handler.RequireLogin(handler.DeleteInventoryItem))

	// Phase 2: 技師工時/佣金
	mux.HandleFunc("GET /work-orders/{id}/worklogs", handler.RequireAuth(handler.ListTechnicianWorklogs))
	mux.HandleFunc("POST /work-orders/{id}/worklogs", handler.RequireLogin(handler.AddTechnicianWorklog))
	mux.HandleFunc("GET /work-orders/{id}/worklogs/{log_id}/delete", handler.RequireLogin(handler.DeleteTechnicianWorklog))
	mux.HandleFunc("GET /technicians/commissions", handler.RequireAuth(handler.ListTechnicianCommissions))

	// Phase 2: 收據列印
	mux.HandleFunc("GET /work-orders/{id}/receipt", handler.RequireAuth(handler.PrintWorkOrderReceipt))

	// Phase 2: 照片上傳
	mux.HandleFunc("GET /work-orders/{id}/photos", handler.RequireAuth(handler.ListWorkOrderPhotos))
	mux.HandleFunc("POST /work-orders/{id}/photos", handler.RequireLogin(handler.UploadWorkOrderPhoto))
	mux.HandleFunc("GET /work-orders/{id}/photos/{photo_id}/delete", handler.RequireLogin(handler.DeleteWorkOrderPhoto))
	mux.HandleFunc("GET /photos/{filename}", handler.RequireLogin(handler.ServePhoto))

	// Phase 3: 排程 + 報表
	mux.HandleFunc("GET /scheduled-tasks", handler.RequireAuth(handler.ListScheduledTasks))
	mux.HandleFunc("GET /scheduled-tasks/{key}/toggle", handler.RequireLogin(handler.ToggleScheduledTask))
	mux.HandleFunc("GET /scheduled-tasks/backups/run", handler.RequireLogin(handler.RunBackupNow))
	mux.HandleFunc("GET /scheduled-tasks/notifications/run", handler.RequireLogin(handler.RunNotificationsNow))
	mux.HandleFunc("GET /scheduled-tasks/maintenance-due/run", handler.RequireLogin(handler.RunMaintenanceScanNow))
	mux.HandleFunc("GET /scheduled-tasks/work-order-ready/run", handler.RequireLogin(handler.RunWorkOrderReadyNow))
	mux.HandleFunc("GET /notifications/{id}/cancel", handler.RequireLogin(handler.CancelNotification))
	mux.HandleFunc("GET /reports", handler.RequireAuth(handler.ReportsHome))

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

	log.Printf("AutoRepair OS 啟動中，監聽 port: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
