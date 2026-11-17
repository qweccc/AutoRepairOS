package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// TestWorkOrderItemSoftDeleteHidesFromReads 守 SRS「工單明細不得物理刪除」：
// 刪除單一項目之後，列還在（帶 deleted_at），但所有讀取路徑都看不到它——
// 明細列表、總額重算皆然。使用真實 migration schema，順便驗證 052 的
// active_work_order_items 視真的有被建立。
func TestWorkOrderItemSoftDeleteHidesFromReads(t *testing.T) {
	batchBSeedBase(t) // newMigratedTestDB → 跑完整個 migration 棧
	batchBSeedWO(t, 1, "in_progress", 0)
	batchBExec(t, `INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity, reserved_quantity, selling_price)
		VALUES (1,'P-1','濾芯','A1',10,2,200)`)
	batchBExec(t, `INSERT INTO work_order_items (id, work_order_id, item_type, inventory_item_id, description, quantity, unit_price, subtotal)
		VALUES (1,1,'part',1,'濾芯',2,200,400)`)

	// 052 的產物和 029 其他表一致：一個 active_* 視。
	var viewType string
	if err := db.DB.QueryRow(`SELECT type FROM sqlite_master WHERE name='active_work_order_items'`).Scan(&viewType); err != nil {
		t.Fatalf("找不到 active_work_order_items：%v（migration 052 沒跑到？）", err)
	}
	if viewType != "view" {
		t.Fatalf("active_work_order_items 應該是 view，實際 %q", viewType)
	}

	items, total := loadWorkOrderItems(1)
	if len(items) != 1 || total != 400 {
		t.Fatalf("刪除前應看到 1 項 / 400，得到 %d 項 / %v", len(items), total)
	}

	req := httptest.NewRequest("POST", "/work-orders/1/items/1/delete", nil)
	req.SetPathValue("id", "1")
	req.SetPathValue("item_id", "1")
	rec := httptest.NewRecorder()
	DeleteWorkOrderItem(rec, req, model.User{ID: 1, Role: "owner"})
	if rec.Code != http.StatusFound {
		t.Fatalf("delete: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 列還在、已標記（不是物理刪除）。
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE id=1 AND deleted_at IS NOT NULL`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("明細應保留並標記 deleted_at（n=%d, err=%v）", n, err)
	}
	// 讀取路徑全面看不到它：明細列表空了，刪除 handler 也已重算總額。
	items, total = loadWorkOrderItems(1)
	if len(items) != 0 || total != 0 {
		t.Fatalf("刪除後仍讀得到項目：%d 項 / %v", len(items), total)
	}
	var amount float64
	if err := db.DB.QueryRow(`SELECT total_amount FROM work_orders WHERE id=1`).Scan(&amount); err != nil {
		t.Fatal(err)
	}
}
