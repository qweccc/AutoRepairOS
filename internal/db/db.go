package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	// 連線池設定：不能設 MaxOpenConns(1)——handler 中常有「查詢 rows 尚未 Close 又發另一個查詢」的
	// 巢狀呼叫（例如載入工單時同時算技師名稱），單一連線會直接死鎖。
	// 因此放寬上限，改以 busy_timeout 處理寫入衝突。
	DB.SetMaxOpenConns(8)
	DB.SetMaxIdleConns(4)
	DB.SetConnMaxLifetime(time.Hour)

	if err := DB.Ping(); err != nil {
		return fmt.Errorf("SQLite 連線失敗: %v", err)
	}

	// journal mode：必須用 PRAGMA 明確設定。
	//
	// DB_PATH 的 DSN 參數寫法是 `_journal_mode=WAL`，但 modernc.org/sqlite 會
	// 靜默忽略它——實測帶著該參數連線時 journal_mode 仍是預設的 delete。
	// 只有在這裡明確下 PRAGMA 才會生效（journal mode 一旦設定會存進 DB 檔案，
	// 所以過去的 WAL 資料庫即使新版不再設定也仍是 WAL，容易誤以為參數有效）。
	//
	// 若目標環境的檔案系統不支援 WAL（例如某些網路磁碟），設為 WAL 會失敗；
	// 這時退回 rollback journal 並記錄警告，不讓整個服務開不起來。
	if _, err := DB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		log.Printf("無法啟用 WAL（%v），改用預設 journal mode", err)
	} else if mode := journalMode(); mode != "wal" {
		log.Printf("要求 WAL 但實際為 %s，可能是檔案系統不支援", mode)
	}

	MustExec("PRAGMA busy_timeout=5000")

	log.Printf("資料庫初始化完成 (journal mode: %s)", journalMode())
	return nil
}

// journalMode 回報實際生效的 journal mode，用於啟動 log 與除錯。
func journalMode() string {
	var m string
	if err := DB.QueryRow("PRAGMA journal_mode").Scan(&m); err != nil {
		return "unknown"
	}
	return m
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
