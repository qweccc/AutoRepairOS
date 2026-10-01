package seed

import (
	"fmt"
	"log"

	"autorepairos/internal/config"
	"autorepairos/internal/db"

	"golang.org/x/crypto/bcrypt"
)

// Run 建立預設使用者。已存在者以 INSERT OR IGNORE 略過，可重複安全呼叫。
func Run(cfg *config.Config) error {
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
		var exists int
		err := db.DB.QueryRow("SELECT COUNT(1) FROM users WHERE username = ?", u.username).Scan(&exists)
		if err != nil {
			return fmt.Errorf("查詢使用者 %s 失敗: %v", u.username, err)
		}
		if exists > 0 {
			log.Printf("使用者已存在，略過: %s", u.username)
			continue
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(u.password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("bcrypt 失敗: %v", err)
		}

		_, err = db.DB.Exec(
			"INSERT INTO users (username, password, role, name) VALUES (?, ?, ?, ?)",
			u.username, string(hash), u.role, u.name,
		)
		if err != nil {
			return fmt.Errorf("建立使用者 %s 失敗: %v", u.username, err)
		}
		log.Printf("建立使用者: %s (角色: %s)", u.username, u.role)
	}

	// Phase 2: 供應商與庫存種子（讓零件流程真正可跑）
	if err := RunSuppliers(cfg); err != nil {
		return fmt.Errorf("供應商種子失敗: %v", err)
	}
	if err := RunInventorySeed(cfg); err != nil {
		return fmt.Errorf("庫存種子失敗: %v", err)
	}

	return nil
}
