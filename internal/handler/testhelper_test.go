package handler

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"autorepairos/internal/db"
)

// openTestDB 開一個獨立記憶體 SQLite 給 handler 測試用，用完自動關閉。
// handler 直接吃全域 db.DB，所以測試要換掉它再還原。
// 不用 cache=shared：共用快取會讓不同測試的連線互相干擾（曾造成死鎖卡住）。
func openTestDB(t *testing.T) (*sql.DB, error) {
	t.Helper()
	orig := db.DB
	t.Cleanup(func() { db.DB = orig })
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	// 單一連線：SQLite 記憶體 DB 每個連線是獨立 DB，限制 1 條連線確保
	// 同一測試內所有 Query/Exec 都落在同一個 DB 實例上。
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = d.Close() })
	return d, nil
}

// ensureWOISoftDelete 讓「自走 CREATE TABLE」的測試也有 work_order_items 的
// 軟刪除欄與 active_work_order_items 視（正式庫由 migration 052 建立）。
// 沒有它，所有改讀 active 視的 handler 會因 view 不存在而失敗。
func ensureWOISoftDelete(t *testing.T, d *sql.DB) error {
	t.Helper()
	if err := db.EnsureWorkOrderItemsSoftDelete(d); err != nil {
		return err
	}
	return db.EnsureEstimateItemsSoftDelete(d)
}
