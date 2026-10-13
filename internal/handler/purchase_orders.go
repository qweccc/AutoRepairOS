package handler

import (
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

// CreatePurchaseOrder 建立進貨單主檔。
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
	res, err := db.DB.Exec(`INSERT INTO purchase_orders
		(supplier_id, work_order_id, order_date, status, total_amount, notes, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		supplierID, workOrderID, orderDate, "draft", 0, nullStr(notes), actorID, nowText())
	if err != nil {
		fail(w, "建立進貨單失敗")
		return
	}
	id, _ := res.LastInsertId()
	redirectMsg(w, r, "/purchase-orders/"+strconv.FormatInt(id, 10), "進貨單已建立，請加入品項")
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

	pages.ShowPurchaseOrderPage(po, items, total, locked, supplierOptionsSel(po.SupplierID)).Render(r.Context(), w)
}

// loadPurchaseOrderItems 取進貨單品項與總額。
func loadPurchaseOrderItems(orderID int64) ([]model.PurchaseOrderItem, float64) {
	rows, err := db.DB.Query(`SELECT id, order_id, COALESCE(inventory_item_id,0), description,
		COALESCE(part_number,''), quantity, unit_cost, subtotal
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
			&it.PartNumber, &it.Quantity, &it.UnitCost, &it.Subtotal); err != nil {
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
		w.Write([]byte(`<tr><td colspan="6" style="color:#f87171;text-align:center">此單已進貨，無法新增品項</td></tr>`))
		return
	}

	desc := strings.TrimSpace(r.FormValue("description"))
	qty := atofDefault(r.FormValue("quantity"), 0)
	unitCost := atofDefault(r.FormValue("unit_cost"), 0)
	invID := mustInt(r.FormValue("inventory_item_id"))
	partNo := strings.TrimSpace(r.FormValue("part_number"))

	if desc == "" && invID > 0 {
		// 選了庫存零件但沒填描述：用零件名稱帶入。
		db.DB.QueryRow(`SELECT name, COALESCE(part_number,'') FROM inventory_items WHERE id = ?`, invID).
			Scan(&desc, &partNo)
	}
	if desc == "" || qty <= 0 {
		w.Write([]byte(`<tr><td colspan="6" style="color:#f87171;text-align:center">品項名稱與數量為必填</td></tr>`))
		return
	}

	subtotal := qty * unitCost
	_, err := db.DB.Exec(`INSERT INTO purchase_order_items
		(order_id, inventory_item_id, description, part_number, quantity, unit_cost, subtotal)
		VALUES (?,?,?,?,?,?,?)`,
		id, invID, desc, partNo, qty, unitCost, subtotal)
	if err != nil {
		w.Write([]byte(`<tr><td colspan="6" style="color:#f87171;text-align:center">加入品項失敗</td></tr>`))
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
		w.Write([]byte(`<tr><td colspan="6" style="color:#f87171;text-align:center">此單已進貨，無法刪除品項</td></tr>`))
		return
	}

	db.DB.Exec(`DELETE FROM purchase_order_items WHERE id = ? AND order_id = ?`, itemID, orderID)
	recalcPurchaseOrderTotal(orderID)
	items, total := loadPurchaseOrderItems(orderID)
	pages.PurchaseOrderItemsTable(orderID, items, total, false).Render(r.Context(), w)
}

// ReceivePurchaseOrder 確認進貨：把品項數量加回庫存，單據鎖定。
func ReceivePurchaseOrder(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	var status string
	if err := db.DB.QueryRow(`SELECT status FROM purchase_orders WHERE id = ?`, id).Scan(&status); err != nil {
		http.NotFound(w, r)
		return
	}
	if status == "received" {
		fail(w, "此單已進貨")
		return
	}

	items, _ := loadPurchaseOrderItems(id)
	if len(items) == 0 {
		fail(w, "尚無品項，無法進貨")
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "進貨失敗")
		return
	}
	// 品項指向庫存零件的，數量加回庫存（零件進貨入庫）。
	for _, it := range items {
		if it.InventoryItemID > 0 {
			if _, err := tx.Exec(`UPDATE inventory_items SET stock_quantity = stock_quantity + ? WHERE id = ?`,
				it.Quantity, it.InventoryItemID); err != nil {
				tx.Rollback()
				fail(w, "入庫失敗")
				return
			}
		}
	}
	if _, err := tx.Exec(`UPDATE purchase_orders SET status='received', received_at=? WHERE id=?`,
		nowText(), id); err != nil {
		tx.Rollback()
		fail(w, "進貨失敗")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, "進貨失敗")
		return
	}

	redirectMsg(w, r, "/purchase-orders/"+strconv.FormatInt(id, 10), "已進貨，庫存已更新")
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

