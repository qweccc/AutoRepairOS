package main

import (
	"log"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
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

	if err := seed.Run(cfg); err != nil {
		log.Fatalf("Seed 執行失敗: %v", err)
	}

	log.Println("Seed 執行完成")
}
