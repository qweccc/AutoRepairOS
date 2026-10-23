package db

import (
	"context"
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

// dbFilePath 記錄解析後的 DB 檔案絕對／相對路徑（不含 DSN 參數），
// 供需要「與 DB 同目錄」的附屬檔案（如 Telegram 綁定 offset）定位用。
var dbFilePath string

// Path 回傳解析後的 DB 檔案路徑。Init 尚未呼叫時回傳空字串。
func Path() string { return dbFilePath }

// TxExecutor is the subset of transaction operations used by inventory writes.
type TxExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
}

// ImmediateTx holds a connection for a SQLite BEGIN IMMEDIATE transaction.
type ImmediateTx struct {
	conn   *sql.Conn
	closed bool
}

func (tx *ImmediateTx) Exec(query string, args ...interface{}) (sql.Result, error) {
	return tx.conn.ExecContext(context.Background(), query, args...)
}

func (tx *ImmediateTx) Query(query string, args ...interface{}) (*sql.Rows, error) {
	return tx.conn.QueryContext(context.Background(), query, args...)
}

func (tx *ImmediateTx) QueryRow(query string, args ...interface{}) *sql.Row {
	return tx.conn.QueryRowContext(context.Background(), query, args...)
}

func (tx *ImmediateTx) Commit() error {
	if tx.closed {
		return sql.ErrTxDone
	}
	_, err := tx.conn.ExecContext(context.Background(), "COMMIT")
	if err != nil {
		_, _ = tx.conn.ExecContext(context.Background(), "ROLLBACK")
	}
	tx.closed = true
	if closeErr := tx.conn.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (tx *ImmediateTx) Rollback() error {
	if tx.closed {
		return sql.ErrTxDone
	}
	_, err := tx.conn.ExecContext(context.Background(), "ROLLBACK")
	tx.closed = true
	if closeErr := tx.conn.Close(); err == nil {
		err = closeErr
	}
	return err
}

// BeginImmediate starts a SQLite write transaction, acquiring the write
// reservation before reading state that must be changed exactly once.
func BeginImmediate() (*ImmediateTx, error) {
	conn, err := DB.Conn(context.Background())
	if err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(context.Background(), "PRAGMA busy_timeout=5000"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &ImmediateTx{conn: conn}, nil
}

func Init(cfg *config.Config) error {
	var err error

	dataDir := "./data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("建立 data 目錄失敗: %v", err)
	}

	dbPath := cfg.DBPath
	switch {
	case strings.HasPrefix(dbPath, "file:"):
		// 已是完整 DSN（docker-compose、.env 範本都寫這種），直接用。
	case filepath.IsAbs(dbPath):
		// 絕對路徑：補 file: 前綴後照原路徑開檔。
		// 曾經這裡誤寫成跟相對路徑一樣 join dataDir，導致帶絕對 DB_PATH 的
		// 執行個體（隔離測試 DB、暫存 DB）全部靜默連到 ./data/autorepair.db，
		// 隔離測試因此打到正式資料庫上，極難察覺。
		dbPath = "file:" + dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	default:
		// 相對路徑或未設定：沿用預設資料庫位置 ./data/autorepair.db。
		dbPath = "file:" + filepath.Join(dataDir, "autorepair.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	}

	// 存下純檔案路徑（去掉 file: 前綴與 ? 後的 DSN 參數），供附屬檔案定位。
	dbFilePath = strings.TrimPrefix(dbPath, "file:")
	if i := strings.IndexByte(dbFilePath, '?'); i >= 0 {
		dbFilePath = dbFilePath[:i]
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
