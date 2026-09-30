package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"autorepairos/internal/config"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init(cfg *config.Config) error {
	var err error

	dataDir := "./data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("建立 data 目錄失敗: %v", err)
	}

	dbPath := cfg.DBPath
	if !strings.HasPrefix(dbPath, "file:") && !filepath.IsAbs(dbPath) {
		dbPath = "file:" + filepath.Join(dataDir, "autorepair.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	} else if !strings.HasPrefix(dbPath, "file:") {
		dbPath = "file:" + filepath.Join(dataDir, "autorepair.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	}

	DB, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("開啟 SQLite 失敗: %v", err)
	}

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("SQLite 連線失敗: %v", err)
	}

	MustExec("PRAGMA journal_mode=WAL")
	MustExec("PRAGMA busy_timeout=5000")

	log.Println("資料庫初始化完成 (WAL 模式)")
	return nil
}

func MustExec(query string, args ...interface{}) {
	_, err := DB.Exec(query, args...)
	if err != nil {
		log.Printf("SQL 執行失敗: %v (查詢: %s)", err, query)
	}
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}
