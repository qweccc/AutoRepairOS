package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// 進貨單（採購進貨）：主檔 + 品項明細。
// 核心用途：記錄向供應商進了什麼、各多少錢、總共要給廠商多少錢。
// 狀態 draft=草稿可改；received=已進貨（數量已加回庫存，鎖定不可改）。

const purchaseOrderCols = `po.id, po.supplier_id, COALESCE(s.name,''), COALESCE(po.work_order_id,0), COALESCE(po.order_date,''),
	po.status, COALESCE(po.total_amount,0), COALESCE(po.notes,''), po.created_at, COALESCE(po.received_at,'')`

func scanPurchaseOrder(rows interface{ Scan(...interface{}) error }) (model.PurchaseOrder, error) {
	var po model.PurchaseOrder
	err := rows.Scan(&po.ID, &po.SupplierID, &po.SupplierName, &po.WorkOrderID, &po.OrderDate,
		&po.Status, &po.TotalAmount, &po.Notes, &po.CreatedAt, &po.ReceivedAt)
	return po, err
}

// ListPurchaseOrders 進貨單列表（HTMX 即時搜尋）。
func ListPurchaseOrders(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	status := strings.TrimSpace(qv.Get("status"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (s.name LIKE ? OR COALESCE(po.notes,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if status != "" {
		where += ` AND po.status = ?`
		args = append(args, status)
	}

	total := countRows(`SELECT COUNT(1) FROM purchase_orders po
		JOIN suppliers s ON s.id = po.supplier_id`+where, args...)

	query := `SELECT ` + purchaseOrderCols + ` FROM purchase_orders po
		JOIN suppliers s ON s.id = po.supplier_id` + where +
		` ORDER BY po.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢進貨單失敗")
		return
	}
	defer rows.Close()

	var poRows []pages.PurchaseOrderRow
	for rows.Next() {
		po, err := scanPurchaseOrder(rows)
		if err != nil {
			continue
		}
		var itemCount int
		db.DB.QueryRow(`SELECT COUNT(1) FROM purchase_order_items WHERE order_id = ?`, po.ID).Scan(&itemCount)
		poRows = append(poRows, pages.PurchaseOrderRow{
			ID:           po.ID,
			SupplierID:   po.SupplierID,
			SupplierName: po.SupplierName,
			OrderDate:    po.OrderDate,
			Status:       po.Status,
			TotalAmount:  po.TotalAmount,
			Notes:        po.Notes,
			ItemCount:    itemCount,
		})
	}

	pages.ListPurchaseOrdersPage(poRows, total, page, size, q, status).Render(r.Context(), w)
}

// SearchPurchaseOrdersAPI 進貨單即時搜尋（HTMX 用）。
func SearchPurchaseOrdersAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (s.name LIKE ? OR COALESCE(po.notes,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if status != "" {
		where += ` AND po.status = ?`
		args = append(args, status)
	}
	rows, err := db.DB.Query(`SELECT `+purchaseOrderCols+` FROM purchase_orders po
		JOIN suppliers s ON s.id = po.supplier_id`+where+
		` ORDER BY po.id DESC LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.PurchaseOrderRow
	for rows.Next() {
		po, err := scanPurchaseOrder(rows)
		if err != nil {
			continue
		}
		var itemCount int
		db.DB.QueryRow(`SELECT COUNT(1) FROM purchase_order_items WHERE order_id = ?`, po.ID).Scan(&itemCount)
		result = append(result, pages.PurchaseOrderRow{
			ID:           po.ID,
			SupplierID:   po.SupplierID,
			SupplierName: po.SupplierName,
			OrderDate:    po.OrderDate,
			Status:       po.Status,
			TotalAmount:  po.TotalAmount,
			Notes:        po.Notes,
			ItemCount:    itemCount,
		})
	}
	pages.PurchaseOrderSearchRows(result).Render(r.Context(), w)
}

// NewPurchaseOrderForm 開立進貨單。
func NewPurchaseOrderForm(w http.ResponseWriter, r *http.Request, user model.User) {
	pages.NewPurchaseOrderPage(today(), supplierOptionsSel(0), "").Render(r.Context(), w)
}

// CreatePurchaseOrder 建立進貨單主檔，並可一次帶入品項。
//
// 為什麼一次送完整單據：「先拍單據再開單」流程（拍照辨識供應商+品項後）
// 不希望櫃檯先建一張空單、再一筆一筆加品項。表單可帶 hidden 欄位 items_json
// （JSON 陣列 [{description,quantity,unit_price,part_number}]），建單後同一
// 交易寫入；沒有 items_json 就維持舊行為（建空單，之後於詳情頁加品項）。
func CreatePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	supplierID := mustInt(r.FormValue("supplier_id"))
	orderDate := strings.TrimSpace(r.FormValue("order_date"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	if supplierID <= 0 {
		fail(w, "請選擇供應商")
		return
	}
	if orderDate == "" {
		orderDate = today()
	}

	var actorID int64
	if u, err := currentUser(r); err == nil {
		actorID = u.ID
	}
	workOrderID := mustInt(r.FormValue("work_order_id"))

	items := parseItemsJSON(r.FormValue("items_json"))

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "建立進貨單失敗")
		return
	}
	res, err := tx.Exec(`INSERT INTO purchase_orders
		(supplier_id, work_order_id, order_date, status, total_amount, notes, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		supplierID, workOrderID, orderDate, "draft", 0, nullStr(notes), actorID, nowText())
	if err != nil {
		tx.Rollback()
		fail(w, "建立進貨單失敗")
		return
	}
	id, _ := res.LastInsertId()

	var total float64
	for _, it := range items {
		if it.Description == "" || it.Quantity <= 0 {
			continue // 沒品名或數量不合理的列跳過（其餘照寫）
		}
		subtotal := it.Quantity * it.UnitPrice
		if _, err := tx.Exec(`INSERT INTO purchase_order_items
			(order_id, inventory_item_id, description, part_number, category, quantity, unit_cost, subtotal)
			VALUES (?,?,?,?,?,?,?,?)`,
			id, 0, it.Description, it.PartNumber, it.Category, it.Quantity, it.UnitCost(), subtotal); err != nil {
			tx.Rollback()
			fail(w, "建立進貨單失敗（品項寫入錯誤）")
			return
		}
		total += subtotal
	}
	if _, err := tx.Exec(`UPDATE purchase_orders SET total_amount = ? WHERE id = ?`, total, id); err != nil {
		tx.Rollback()
		fail(w, "建立進貨單失敗")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, "建立進貨單失敗")
		return
	}

	msg := "進貨單已建立，請加入品項"
	if len(items) > 0 {
		msg = "進貨單已建立（含 " + strconv.Itoa(len(items)) + " 項品項）"
	}
	redirectMsg(w, r, "/purchase-orders/"+strconv.FormatInt(id, 10), msg)
}

// orderItemDraft 建立進貨單時帶入的暫存品項（來自 items_json）。
// Category 是拍照辨識時猜到的零件類別（lookup code），確認進貨時帶到新建零件。
type orderItemDraft struct {
	Description string
	Quantity    float64
	UnitPrice   float64
	PartNumber  string
	Category    string
}

// UnitCost 品項的 unit_cost 就是 unit_price（進貨單價），命名對齊 DB 欄位。
func (d orderItemDraft) UnitCost() float64 { return d.UnitPrice }

// parseItemsJSON 解析表單帶入的 items_json；壞 JSON 或空值回空切片（不報錯，
// 維持「可手動建空單」的舊行為）。
func parseItemsJSON(raw string) []orderItemDraft {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var arr []struct {
		Description string  `json:"description"`
		Quantity    float64 `json:"quantity"`
		UnitPrice   float64 `json:"unit_price"`
		PartNumber  string  `json:"part_number"`
		Category    string  `json:"category"`
	}
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil
	}
	var out []orderItemDraft
	for _, it := range arr {
		out = append(out, orderItemDraft{
			Description: strings.TrimSpace(it.Description),
			Quantity:    it.Quantity,
			UnitPrice:   it.UnitPrice,
			PartNumber:  strings.TrimSpace(it.PartNumber),
			Category:    strings.TrimSpace(it.Category),
		})
	}
	return out
}

// ShowPurchaseOrder 進貨單詳情：主檔 + 品項 + 總額（要給廠商多少錢）。
func ShowPurchaseOrder(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	row := db.DB.QueryRow(`SELECT `+purchaseOrderCols+` FROM purchase_orders po
		JOIN suppliers s ON s.id = po.supplier_id WHERE po.id = ?`, id)
	po, err := scanPurchaseOrder(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	items, total := loadPurchaseOrderItems(id)
	locked := po.Status == "received"

	pages.ShowPurchaseOrderPage(po, items, total, locked, supplierOptionsSel(po.SupplierID),
		lookupOptions("category", ""), r.URL.Query().Get("msg")).Render(r.Context(), w)
}

// loadPurchaseOrderItems 取進貨單品項與總額。
func loadPurchaseOrderItems(orderID int64) ([]model.PurchaseOrderItem, float64) {
	rows, err := db.DB.Query(`SELECT id, order_id, COALESCE(inventory_item_id,0), description,
		COALESCE(part_number,''), COALESCE(category,''), quantity, unit_cost, subtotal
		FROM purchase_order_items WHERE order_id = ? ORDER BY id`, orderID)
	if err != nil {
		return nil, 0
	}
	defer rows.Close()
	var items []model.PurchaseOrderItem
	var total float64
	for rows.Next() {
		var it model.PurchaseOrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.InventoryItemID, &it.Description,
			&it.PartNumber, &it.Category, &it.Quantity, &it.UnitCost, &it.Subtotal); err != nil {
			continue
		}
		items = append(items, it)
		total += it.Subtotal
	}
	return items, total
}

// AddPurchaseOrderItem 加入品項（HTMX 無刷新，回傳品項表格片段）。
func AddPurchaseOrderItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	// 已進貨的單據鎖定，不可再改（數量已入庫）。
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM purchase_orders WHERE id = ?`, id).Scan(&status); err != nil {
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
		db.DB.QueryRow(`SELECT name, COALESCE(part_number,'') FROM inventory_items WHERE id = ?`, invID).
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
	if err := db.DB.QueryRow(`SELECT status FROM purchase_orders WHERE id = ?`, orderID).Scan(&status); err != nil {
		http.NotFound(w, r)
		return
	}
	if status == "received" {
		w.Write([]byte(`<tr><td colspan="7" style="color:#f87171;text-align:center">此單已進貨，無法刪除品項</td></tr>`))
		return
	}

	db.DB.Exec(`DELETE FROM purchase_order_items WHERE id = ? AND order_id = ?`, itemID, orderID)
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
		fail(w, "進貨失敗")
		return
	}
	defer tx.Rollback()

	// Claim the draft while holding SQLite's write reservation. Only the
	// request that changes one row may proceed to update inventory.
	res, err := tx.Exec(`UPDATE purchase_orders SET status='received', received_at=?
		WHERE id=? AND status='draft'`, nowText(), id)
	if err != nil {
		fail(w, "進貨失敗")
		return
	}
	claimed, err := res.RowsAffected()
	if err != nil {
		fail(w, "進貨失敗")
		return
	}
	if claimed == 0 {
		var status string
		if err := tx.QueryRow(`SELECT status FROM purchase_orders WHERE id = ?`, id).Scan(&status); err != nil {
			http.NotFound(w, r)
			return
		}
		fail(w, "此單已進貨")
		return
	}

	items := make([]model.PurchaseOrderItem, 0)
	rows, err := tx.Query(`SELECT id, order_id, COALESCE(inventory_item_id,0), description,
		COALESCE(part_number,''), COALESCE(category,''), quantity, unit_cost, subtotal
		FROM purchase_order_items WHERE order_id = ? ORDER BY id`, id)
	if err != nil {
		fail(w, "進貨失敗")
		return
	}
	for rows.Next() {
		var it model.PurchaseOrderItem
		if err := rows.Scan(&it.ID, &it.OrderID, &it.InventoryItemID, &it.Description,
			&it.PartNumber, &it.Category, &it.Quantity, &it.UnitCost, &it.Subtotal); err != nil {
			rows.Close()
			fail(w, "進貨失敗")
			return
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
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
	if err := tx.QueryRow(`SELECT supplier_id FROM purchase_orders WHERE id = ?`, id).Scan(&supplierID); err != nil {
		fail(w, "進貨失敗")
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
				fail(w, "進貨失敗")
				return
			}
		}

		// 入庫後以 RETURNING 的結果記錄實際庫存數量。
		var after int64
		if err := tx.QueryRow(`UPDATE inventory_items SET stock_quantity = stock_quantity + ?
			WHERE id = ? RETURNING stock_quantity`, int64(it.Quantity), invID).Scan(&after); err != nil {
			fail(w, "入庫失敗（找不到零件）")
			return
		}
		before := after - int64(it.Quantity)
		if _, err := tx.Exec(`INSERT INTO inventory_movements
			(inventory_item_id, delta, before_quantity, after_quantity, reason, source)
			VALUES (?,?,?,?,?,?)`,
			invID, int64(it.Quantity), before, after,
			"進貨單 #"+strconv.FormatInt(id, 10)+" 確認進貨", "purchase_receive"); err != nil {
			fail(w, "寫入異動紀錄失敗")
			return
		}
	}
	if err := tx.Commit(); err != nil {
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
	_ = tx.QueryRow(`SELECT COALESCE(category,'') FROM purchase_order_items WHERE id = ?`, it.ID).Scan(&category)

	// 料號：優先用品項料號；空或撞號則用 PO-單號-序號 後綴遞增。
	base := strings.TrimSpace(it.PartNumber)
	if base == "" {
		base = "PO-" + strconv.FormatInt(orderID, 10) + "-" + strconv.FormatInt(it.ID, 10)
	}
	part := base
	for attempt := 0; attempt < 50; attempt++ {
		var dup int64
		if err := tx.QueryRow(`SELECT COUNT(1) FROM inventory_items WHERE part_number = ?`, part).Scan(&dup); err != nil {
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
	if err := db.DB.QueryRow(`SELECT status FROM purchase_orders WHERE id = ?`, id).Scan(&status); err != nil {
		http.NotFound(w, r)
		return
	}
	if status == "received" {
		fail(w, "已進貨的單據不可刪除")
		return
	}
	db.DB.Exec(`DELETE FROM purchase_order_items WHERE order_id = ?`, id)
	db.DB.Exec(`DELETE FROM purchase_orders WHERE id = ?`, id)
	redirectMsg(w, r, "/purchase-orders", "進貨單已刪除")
}

// recalcPurchaseOrderTotal 重算進貨單總額（品項小計加總）。
func recalcPurchaseOrderTotal(orderID int64) {
	db.DB.Exec(`UPDATE purchase_orders SET total_amount =
		COALESCE((SELECT SUM(subtotal) FROM purchase_order_items WHERE order_id = ?), 0)
		WHERE id = ?`, orderID, orderID)
}

func nowText() string {
	return time.Now().Format("2006-01-02 15:04:05")
}
