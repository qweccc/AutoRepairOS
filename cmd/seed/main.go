package main

import (
	"fmt"
	"log"

	"autorepairos/internal/config"
	"autorepairos/internal/db"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	cfg := config.Load()

	if err := runSeed(cfg); err != nil {
		log.Fatalf("Seed 執行失敗: %v", err)
	}

	log.Println("Seed 執行完成")
}

func runSeed(cfg *config.Config) error {
	if err := db.Init(cfg); err != nil {
		return fmt.Errorf("資料庫初始化失敗: %v", err)
	}
	defer db.Close()

	defaults := []struct {
		username string
		password string
		role     string
		name     string
	}{
		{cfg.DefaultOwner, cfg.DefaultOwnerPwd, "owner", "系統管理員"},
		{cfg.DefaultCounter, cfg.DefaultCounterPwd, "counter", "櫃檯人員"},
		{cfg.DefaultTech, cfg.DefaultTechPwd, "technician", "技師"},
	}

	for _, u := range defaults {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("bcrypt 失敗: %v", err)
		}

		_, err = db.DB.Exec(
			"INSERT OR IGNORE INTO users (username, password, role, name) VALUES (?, ?, ?, ?)",
			u.username, string(hash), u.role, u.name,
		)
		if err != nil {
			return fmt.Errorf("建立使用者 %s 失敗: %v", u.username, err)
		}
		log.Printf("建立使用者: %s (角色: %s)", u.username, u.role)
	}

	return nil
}
