package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// AddPurchaseOrderItem 加入品項（HTMX 無刷新，回傳品項表格片段）。
func AddPurchaseOrderItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	// 已進貨的單據鎖定，不可再改（數量已入庫）。
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM active_purchase_orders WHERE id = ?`, id).Scan(&status); err != nil {
		http.NotFound(w, r)
		return
	}
	if status == "received" {
		w.Write([]byte(`<tr><td colspan="7" style="color:#f87171;text-align:center">此單已進貨，無法新增品項</td></tr>`))
		return
	}

	desc := strings.TrimSpace(r.FormValue("description"))
	qty := atofDefault(r.FormValue("quantity"), 0)
	unitCost := atofDefault(r.FormValue("unit_cost"), 0)
	category := strings.TrimSpace(r.FormValue("category"))
	invID := mustInt(r.FormValue("inventory_item_id"))
	partNo := strings.TrimSpace(r.FormValue("part_number"))

	if desc == "" && invID > 0 {
		// 選了庫存零件但沒填描述：用零件名稱帶入。
		db.DB.QueryRow(`SELECT name, COALESCE(part_number,'') FROM active_inventory_items WHERE id = ?`, invID).
			Scan(&desc, &partNo)
	}
	if desc == "" || qty <= 0 {
		w.Write([]byte(`<tr><td colspan="7" style="color:#f87171;text-align:center">品項名稱與數量為必填</td></tr>`))
		return
	}

	subtotal := qty * unitCost
	_, err := db.DB.Exec(`INSERT INTO purchase_order_items
		(order_id, inventory_item_id, description, part_number, category, quantity, unit_cost, subtotal)
		VALUES (?,?,?,?,?,?,?,?)`,
		id, invID, desc, partNo, category, qty, unitCost, subtotal)
	if err != nil {
		w.Write([]byte(`<tr><td colspan="7" style="color:#f87171;text-align:center">加入品項失敗</td></tr>`))
		return
	}

	recalcPurchaseOrderTotal(id)
	items, total := loadPurchaseOrderItems(id)
	locked := false
	pages.PurchaseOrderItemsTable(id, items, total, locked).Render(r.Context(), w)
}

// DeletePurchaseOrderItem 刪除品項（HTMX 無刷新）。
func DeletePurchaseOrderItem(w http.ResponseWriter, r *http.Request) {
	orderID := mustInt(r.PathValue("id"))
	itemID := mustInt(r.PathValue("itemId"))

	var status string
	if err := db.DB.QueryRow(`SELECT status FROM active_purchase_orders WHERE id = ?`, orderID).Scan(&status); err != nil {
		http.NotFound(w, r)
		return
	}
	if status == "received" {
		w.Write([]byte(`<tr><td colspan="7" style="color:#f87171;text-align:center">此單已進貨，無法刪除品項</td></tr>`))
		return
	}

	if _, err := db.DB.Exec(`UPDATE purchase_order_items SET deleted_at = datetime('now')
		WHERE id = ? AND order_id = ? AND deleted_at IS NULL`, itemID, orderID); err != nil {
		fail(w, "刪除品項失敗")
		return
	}
	recalcPurchaseOrderTotal(orderID)
	items, total := loadPurchaseOrderItems(orderID)
	pages.PurchaseOrderItemsTable(orderID, items, total, false).Render(r.Context(), w)
}

// ReceivePurchaseOrder 確認進貨：把品項數量加回庫存，單據鎖定。
//
// 進貨時若品項沒有連結庫存零件（inventory_item_id = 0，例如拍照辨識進來的新品），
// 會**自動建檔**成 inventory_items：
//   - name = 品項描述；part_number = 品項料號，沒有就用 PO-{單號}-{序號} 自動產生
//     （part_number 是 NOT NULL UNIQUE，不能留空；用單號前綴保證不撞號）
//   - bin_location = 「待歸位」（NOT NULL，櫃檯之後到零件頁補實際庫位）
//   - category = 拍照時猜到的類別（辨識結果已存於 purchase_order_items.category）
//   - cost_price = 進貨單價；stock_quantity = 本次進貨數量；supplier = 單據的供應商
//
// 建檔後同一交易內入庫，並寫 inventory_movements（source=purchase_receive）留痕。
func ReceivePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	tx, err := db.BeginImmediate()
	if err != nil {
		log.Printf("開始進貨交易失敗: %v", err)
		fail(w, "進貨失敗")
		return
	}
	defer tx.Rollback()

	// Claim the draft while holding SQLite's write reservation. Only the
	// request that changes one row may proceed to update inventory.
	res, err := tx.Exec(`UPDATE purchase_orders SET status='received', received_at=?
		WHERE id=? AND status='draft'`, nowText(), id)
	if err != nil {
		log.Printf("搶占進貨單失敗: %v", err)
		fail(w, "進貨失敗")
		return
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		log.Printf("讀取進貨單搶占結果失敗: %v", err)
		fail(w, "進貨失敗")
		return
	}
	if claimed == 0 {
		var status string
		if err := tx.QueryRow(`SELECT status FROM active_purchase_orders WHERE id = ?`, id).Scan(&status); err != nil {
			http.NotFound(w, r)
			return
		}
		fail(w, "此單已進貨")
		return
	}

	items := make([]model.PurchaseOrderItem, 0)
	rows, err := tx.Query(`SELECT id, order_id, COALESCE(inventory_item_id,0), description,
		COALESCE(part_number,''), COALESCE(category,''), quantity, unit_cost, subtotal
		FROM active_purchase_order_items WHERE order_id = ? ORDER BY id`, id)
	if err != nil {
		log.Printf("讀取進貨品項失敗: %v", err)
		fail(w, "進貨失敗")
		return
	}
	for rows.Next() {
		var it model.PurchaseOrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.InventoryItemID, &it.Description,
			&it.PartNumber, &it.Category, &it.Quantity, &it.UnitCost, &it.Subtotal); err != nil {
			rows.Close()
			log.Printf("讀取進貨品項失敗: %v", err)
			fail(w, "進貨失敗")
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		log.Printf("讀取進貨品項失敗: %v", err)
		fail(w, "進貨失敗")
		return
	}
	rows.Close()
	if len(items) == 0 {
		fail(w, "尚無品項，無法進貨")
		return
	}

	// 供應商帶到自動建檔的零件上（與單據同來源）。
	var supplierID int64
	if err := tx.QueryRow(`SELECT supplier_id FROM active_purchase_orders WHERE id = ?`, id).Scan(&supplierID); err != nil {
		log.Printf("讀取進貨單供應商失敗: %v", err)
		fail(w, "進貨失敗")
		return
	}
	var payableAmount float64
	for _, it := range items {
		payableAmount += it.Subtotal
	}
	if err := createAPPayable(tx, "purchase_receive", id, supplierID, payableAmount, today()); err != nil {
		log.Printf("建立進貨應付帳款失敗: %v", err)
		fail(w, "建立應付帳款失敗")
		return
	}

	autoCreated := 0
	for _, it := range items {
		invID := it.InventoryItemID
		if invID <= 0 {
			// 沒有連結零件 → 自動建檔（單一交易，編號衝突就換下一個後綴）。
			newID, perr := autoCreatePartFromItem(tx, id, supplierID, it)
			if perr != nil {
				fail(w, "自動建立零件失敗："+perr.Error())
				return
			}
			invID = newID
			autoCreated++
			// 品項回填連結，之後「調庫」與異動紀錄都指得到這顆零件。
			if _, err := tx.Exec(`UPDATE purchase_order_items SET inventory_item_id = ? WHERE id = ?`, invID, it.ID); err != nil {
				log.Printf("回填進貨零件失敗: %v", err)
				fail(w, "進貨失敗")
				return
			}
		}

		// 入庫後以 RETURNING 的結果記錄實際庫存數量。
		var after int64
		if err := tx.QueryRow(`UPDATE inventory_items SET stock_quantity = stock_quantity + ?
			WHERE id = ? RETURNING stock_quantity`, int64(it.Quantity), invID).Scan(&after); err != nil {
			log.Printf("進貨入庫失敗: %v", err)
			fail(w, "入庫失敗（找不到零件）")
			return
		}
		before := after - int64(it.Quantity)
		// P1-6: 進貨回寫成本（加權平均）
		var currentCost float64
		var currentStock int64
		tx.QueryRow(`SELECT COALESCE(cost_price,0), COALESCE(stock_quantity,0) - ? FROM inventory_items WHERE id = ?`,
			int64(it.Quantity), invID).Scan(&currentCost, &currentStock)
		// currentStock is stock before this receive
		var newCost float64
		if currentStock <= 0 {
			newCost = it.UnitCost
		} else {
			newCost = (float64(currentStock)*currentCost + float64(it.Quantity)*it.UnitCost) / float64(currentStock+int64(it.Quantity))
		}
		tx.Exec(`UPDATE inventory_items SET cost_price = ? WHERE id = ?`, newCost, invID)

		if _, err := tx.Exec(`INSERT INTO inventory_movements
			(inventory_item_id, delta, before_quantity, after_quantity, reason, source, purchase_order_item_id)
			VALUES (?,?,?,?,?,?,?)`,
			invID, int64(it.Quantity), before, after,
			"進貨單 #"+strconv.FormatInt(id, 10)+" 確認進貨", "purchase_receive", it.ID); err != nil {
			fail(w, "寫入異動紀錄失敗")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("提交進貨交易失敗: %v", err)
		fail(w, "進貨失敗")
		return
	}

	msg := "已進貨，庫存已更新"
	if autoCreated > 0 {
		msg += "（自動建立 " + strconv.Itoa(autoCreated) + " 個新零件，庫位待歸位、類別已帶入）"
	}
	redirectMsg(w, r, "/purchase-orders/"+strconv.FormatInt(id, 10), msg)
}

// autoCreatePartFromItem 依進貨品項自動建立庫存零件，回傳新零件 id。
//
// 料號規則：品項有料號就用它（已由 AddPurchaseOrderItem 確保不與既有零件衝突，
// 因為建立品項時不會填 inventory_item_id；這裡再防一次 UNIQUE 碰撞換後綴），
// 沒有就用「PO-{單號}-{序號}」。bin_location 一律「待歸位」——NOT NULL 不可留空，
// 實際庫位由櫃檯之後到零件編輯頁補。
func autoCreatePartFromItem(tx db.TxExecutor, orderID, supplierID int64, it model.PurchaseOrderItem) (int64, error) {
	name := strings.TrimSpace(it.Description)
	if name == "" {
		name = "進貨品項 #" + strconv.FormatInt(it.ID, 10)
	}
	bin := "待歸位"

	// 類別：拍照辨識時猜到的（存於 purchase_order_items.category），沒有就未分類。
	category := ""
	_ = tx.QueryRow(`SELECT COALESCE(category,'') FROM active_purchase_order_items WHERE id = ?`, it.ID).Scan(&category)

	// 料號：優先用品項料號；空或撞號則用 PO-單號-序號 後綴遞增。
	base := strings.TrimSpace(it.PartNumber)
	if base == "" {
		base = "PO-" + strconv.FormatInt(orderID, 10) + "-" + strconv.FormatInt(it.ID, 10)
	}
	part := base
	for attempt := 0; attempt < 50; attempt++ {
		var dup int64
		if err := tx.QueryRow(`SELECT COUNT(1) FROM active_inventory_items WHERE part_number = ?`, part).Scan(&dup); err != nil {
			return 0, err
		}
		if dup == 0 {
			break
		}
		part = base + "-" + strconv.Itoa(attempt+1)
	}

	res, err := tx.Exec(`INSERT INTO inventory_items
		(supplier_id, part_number, barcode, name, bin_location, category, item_kind,
		 cost_price, selling_price, stock_quantity, min_safety_stock)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		supplierID, part, nullStr(""), name, bin, nullStr(category), "part",
		it.UnitCost, 0, 0, 5)
	if err != nil {
		return 0, err
	}
	newID, _ := res.LastInsertId()
	return newID, nil
}

// DeletePurchaseOrder 刪除進貨單（僅草稿）。
func DeletePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM active_purchase_orders WHERE id = ?`, id).Scan(&status); err != nil {
		http.NotFound(w, r)
		return
	}
	if status == "received" {
		fail(w, "已進貨的單據不可刪除")
		return
	}
	if _, err := db.DB.Exec(`UPDATE purchase_order_items SET deleted_at = datetime('now')
		WHERE order_id = ? AND deleted_at IS NULL`, id); err != nil {
		fail(w, "刪除進貨單品項失敗")
		return
	}
	if _, err := db.DB.Exec(`UPDATE purchase_orders SET deleted_at = datetime('now')
		WHERE id = ? AND deleted_at IS NULL`, id); err != nil {
		fail(w, "刪除進貨單失敗")
		return
	}
	redirectMsg(w, r, "/purchase-orders", "進貨單已刪除")
}

// recalcPurchaseOrderTotal 重算進貨單總額（品項小計加總）。
func recalcPurchaseOrderTotal(orderID int64) {
	db.DB.Exec(`UPDATE purchase_orders SET total_amount =
		COALESCE((SELECT SUM(subtotal) FROM active_purchase_order_items WHERE order_id = ?), 0)
		WHERE id = ?`, orderID, orderID)
}

func nowText() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
