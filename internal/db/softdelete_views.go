package db

import (
	"database/sql"
	"log"
	"strings"
)

// ignorableSoftDeleteErr 這些狀況在「確保」語意下不是錯誤：
//   - duplicate column：已確保過（重複執行）
//   - no such table：這個測試 schema 根本沒建該表，對應的視也就不需要
func ignorableSoftDeleteErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "no such table")
}

// EnsureWorkOrderItemsSoftDelete 確保 work_order_items 有 deleted_at 欄與
// active_work_order_items 視（工單明細軟刪除，SRS：重要營業資料與工單明細
// 不得物理刪除）。
//
// 由 migration 052 負責正式庫；這裡另外提供同一份確保邏輯，給兩類呼叫端用：
//   - 測試：自走 CREATE TABLE 建立 schema、不跑 migration，需要主動呼叫才會
//     有這組表/視，否則所有讀取路徑會因 view 不存在而失敗。
//   - 啟動流程：Migrate 之後再確保一次，讓「migration 之外新增的软刪除表」
//     有一處單一的定義來源。
func EnsureWorkOrderItemsSoftDelete(d *sql.DB) error {
	return ensureSoftDelete(d, "work_order_items", "active_work_order_items")
}

// EnsureEstimateItemsSoftDelete 確保 estimate_items 的軟刪除欄與
// active_estimate_items 視存在（估價項目與工單明細同一原則，見 migration 053）。
// 同上：重複執行時忽略「欄已存在」，其餘錯誤照常回報。
func EnsureEstimateItemsSoftDelete(d *sql.DB) error {
	return ensureSoftDelete(d, "estimate_items", "active_estimate_items")
}

// ensureSoftDelete 是兩張明細表共用的確保邏輯：加 deleted_at 欄 + 建立
// active_* 視。表不存在（測試 schema 沒建）就直接跳過，不算錯誤。
func ensureSoftDelete(d *sql.DB, table, view string) error {
	if d == nil {
		return nil
	}
	if _, err := d.Exec("ALTER TABLE " + table + " ADD COLUMN deleted_at TEXT"); err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "no such table") {
			return nil // 這個 schema 沒有該表，無從確保
		}
		if !strings.Contains(msg, "duplicate column") {
			log.Printf("確保 %s 軟刪除欄失敗: %v", table, err)
		}
	}
	if _, err := d.Exec("CREATE VIEW IF NOT EXISTS " + view + " AS SELECT * FROM " +
		table + " WHERE deleted_at IS NULL"); err != nil {
		return err
	}
	return nil
}
