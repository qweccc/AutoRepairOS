package db

import (
	"database/sql"
	"log"
	"strings"
)

// EnsureWorkOrderItemsSoftDelete 確保 work_order_items 有 deleted_at 欄與
// active_work_order_items 視（工單明細軟刪除，SRS：重要營業資料與工單明細
// 不得物理刪除）。
//
// 由 migration 052 負責正式庫；這裡另外提供同一份確保邏輯，給兩類呼叫端用：
//   - 測試：自走 CREATE TABLE 建立 schema、不跑 migration，需要主動呼叫才會
//     有這組表/視，否則所有讀取路徑會因 view 不存在而失敗。
//   - 啟動流程：Migrate 之後再確保一次，讓「migration 之外新增的软刪除表」
//     有一處單一的定義來源。
//
// 全部的錯誤只在「ALTER 已存在」（重复執行）時忽略，其餘照常回報。
func EnsureWorkOrderItemsSoftDelete(d *sql.DB) error {
	if d == nil {
		return nil
	}
	if _, err := d.Exec(`ALTER TABLE work_order_items ADD COLUMN deleted_at TEXT`); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			log.Printf("確保 work_order_items 軟刪除欄失敗: %v", err)
		}
	}
	if _, err := d.Exec(`CREATE VIEW IF NOT EXISTS active_work_order_items AS
		SELECT * FROM work_order_items WHERE deleted_at IS NULL`); err != nil {
		return err
	}
	return nil
}
