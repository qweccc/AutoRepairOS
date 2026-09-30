package main

import (
	"log"
	"net/http"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
	"autorepairos/internal/handler"
)

func main() {
	cfg := config.Load()

	if err := db.Init(cfg); err != nil {
		log.Fatalf("資料庫初始化失敗: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/login", handler.LoginHandler)
	mux.HandleFunc("/logout", handler.LogoutHandler)
	mux.HandleFunc("/api/me", handler.MeHandler)
	mux.HandleFunc("/", handler.IndexHandler)

	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	log.Printf("AutoRepair OS 啟動中，監聽 port: %s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
