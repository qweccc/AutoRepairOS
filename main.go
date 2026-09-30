package main

import (
	"log"
	"net/http"

	"autorepairos/internal/config"
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

	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	log.Printf("AutoRepair OS 啟動中，監聽 port: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
