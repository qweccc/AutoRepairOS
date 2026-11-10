package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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

	total := countRows(`SELECT COUNT(1) FROM active_purchase_orders po
		JOIN active_suppliers s ON s.id = po.supplier_id`+where, args...)

	query := `SELECT ` + purchaseOrderCols + ` FROM active_purchase_orders po
		JOIN active_suppliers s ON s.id = po.supplier_id` + where +
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
		db.DB.QueryRow(`SELECT COUNT(1) FROM active_purchase_order_items WHERE order_id = ?`, po.ID).Scan(&itemCount)
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

	pages.ListPurchaseOrdersPage(poRows, total, page, size, q, status, user).Render(r.Context(), w)
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
	rows, err := db.DB.Query(`SELECT `+purchaseOrderCols+` FROM active_purchase_orders po
		JOIN active_suppliers s ON s.id = po.supplier_id`+where+
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
		db.DB.QueryRow(`SELECT COUNT(1) FROM active_purchase_order_items WHERE order_id = ?`, po.ID).Scan(&itemCount)
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
	pages.NewPurchaseOrderPage(today(), supplierOptionsSel(0), "", user).Render(r.Context(), w)
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
		user, _ := currentUser(r)
		w.WriteHeader(http.StatusBadRequest)
		pages.NewPurchaseOrderPage(orderDate, supplierOptionsSel(0), "請選擇供應商", user).Render(r.Context(), w)
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
	row := db.DB.QueryRow(`SELECT `+purchaseOrderCols+` FROM active_purchase_orders po
		JOIN active_suppliers s ON s.id = po.supplier_id WHERE po.id = ?`, id)
	po, err := scanPurchaseOrder(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	items, total := loadPurchaseOrderItems(id)
	locked := po.Status == "received"

	pages.ShowPurchaseOrderPage(po, items, total, locked, supplierOptionsSel(po.SupplierID),
		lookupOptions("category", ""), r.URL.Query().Get("msg"),
		user).Render(r.Context(), w)
}

// loadPurchaseOrderItems 取進貨單品項與總額。
func loadPurchaseOrderItems(orderID int64) ([]model.PurchaseOrderItem, float64) {
	rows, err := db.DB.Query(`SELECT id, order_id, COALESCE(inventory_item_id,0), description,
		COALESCE(part_number,''), COALESCE(category,''), quantity, unit_cost, subtotal
		FROM active_purchase_order_items WHERE order_id = ? ORDER BY id`, orderID)
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
