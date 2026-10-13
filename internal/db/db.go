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

	// journal mode：由 DB_ENABLE_WAL 明確決定，不依賴 DSN 參數。
	//
	// DSN 裡的 _journal_mode=WAL 對 modernc.org/sqlite 是無效參數，會被靜默
	// 忽略；真正的 WAL 必須靠這裡的 PRAGMA。預設關閉，因為 Windows bind-mount
	// 下 WAL 會讓 SQLite 連線直接失敗（disk I/O error 4618），服務會陷入重啟迴圈。
	// 要啟用請設 DB_ENABLE_WAL=true，並確保資料目錄不是 bind-mount（改用
	// Docker named volume），否則會踩到同一個錯誤。
	if cfg.EnableWAL {
		if _, err := DB.Exec("PRAGMA journal_mode=WAL"); err != nil {
			log.Printf("DB_ENABLE_WAL=true 但無法啟用 WAL（%v），沿用預設 journal mode", err)
		} else if mode := journalMode(); mode != "wal" {
			log.Printf("要求 WAL 但實際為 %s，可能是檔案系統不支援（例如 bind-mount）", mode)
		}
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

// Close 安全關閉資料庫：先嘗試把 WAL 寫回主庫並清空（TRUNCATE 模式），
// 再關閉連線池。用在服務關閉（Ctrl+C、SIGTERM、docker stop）時。
//
// 為什麼顯式 checkpoint：SQLite 在關閉「最後一個連線」時本來就會自動
// checkpoint，但那是「盡力而為」——若有其他讀取連線還開著（例如排程任務
// 剛好在跑查詢），關閉時的自動 checkpoint 會被略過，.db-wal 留著舊內容。
// 顯式執行一次是雙保險：失敗（例如 busy）只記 log 不中斷關閉流程，
// 資料本身不會丟（WAL 是 crash-safe 的，下次開啟會重放）。
//
// 注意：signal handler 裡 os.Exit() 會跳過 defer，所以那邊要顯式呼叫本函式。
func Close() {
	if DB == nil {
		return
	}
	if _, err := DB.Exec(`PRAGMA wal_checkpoint(TRUNCATE);`); err != nil {
		log.Printf("WAL checkpoint 失敗（不影響資料安全，下次開啟會重放 WAL）: %v", err)
	}
	if err := DB.Close(); err != nil {
		log.Printf("資料庫關閉時發生錯誤: %v", err)
	}
}
