package handler

import (
	"database/sql"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

const inventoryCols = `i.id, COALESCE(i.supplier_id,0), COALESCE(s.name,''), i.part_number,
	COALESCE(i.barcode,''), i.name, i.bin_location, COALESCE(i.cost_price,0),
	COALESCE(i.selling_price,0), COALESCE(i.stock_quantity,0), COALESCE(i.min_safety_stock,0),
	COALESCE(i.category,''), COALESCE(i.item_kind,'part')`

func scanInventoryItem(rows interface{ Scan(...interface{}) error }) (model.InventoryItem, error) {
	var it model.InventoryItem
	err := rows.Scan(&it.ID, &it.SupplierID, &it.SupplierName, &it.PartNumber, &it.Barcode,
		&it.Name, &it.BinLocation, &it.CostPrice, &it.SellingPrice,
		&it.StockQuantity, &it.MinSafetyStock, &it.Category, &it.ItemKind)
	it.LowStock = it.StockQuantity < it.MinSafetyStock
	return it, err
}

func ListInventory(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	lowOnly := qv.Get("low") == "1"
	category := strings.TrimSpace(qv.Get("category"))
	showMoney := canSeeMoney(user)
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (i.name LIKE ? OR i.part_number LIKE ? OR COALESCE(i.barcode,'') LIKE ? OR i.bin_location LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	if lowOnly {
		where += ` AND COALESCE(i.stock_quantity,0) < COALESCE(i.min_safety_stock,0)`
	}
	if category != "" {
		if category == "__none__" {
			where += ` AND COALESCE(i.category,'') = ''`
		} else {
			where += ` AND i.category = ?`
			args = append(args, category)
		}
	}

	total := countRows(`SELECT COUNT(1) FROM active_inventory_items i`+where, args...)

	query := `SELECT ` + inventoryCols + ` FROM active_inventory_items i
		LEFT JOIN active_suppliers s ON s.id = i.supplier_id` + where +
		` ORDER BY i.name LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢庫存失敗")
		return
	}
	defer rows.Close()

	var list []model.InventoryItem
	for rows.Next() {
		it, err := scanInventoryItem(rows)
		if err != nil {
			continue
		}
		list = append(list, it)
	}

	// 技師不看成本價（毛利屬店內機密），但仍需看到售價以便估價。
	var invRows []pages.InventoryRow
	for _, it := range list {
		invRows = append(invRows, pages.InventoryRow{
			ID:            it.ID,
			PartNumber:    it.PartNumber,
			Name:          it.Name,
			Category:      it.Category,
			BinLocation:   it.BinLocation,
			SupplierName:  it.SupplierName,
			CostPrice:     it.CostPrice,
			SellingPrice:  it.SellingPrice,
			StockQuantity: it.StockQuantity,
			MinSafety:     it.MinSafetyStock,
			LowStock:      it.LowStock,
			ItemKind:      it.ItemKind,
		})
	}

	// 類別篩選籤（依實際資料的類別列出，附筆數）
	var catTabs strings.Builder
	catTabs.WriteString(`<a class="btn ` + btnIf(category == "", "btn-p", "btn-s") + `" href="` + invTabURL(qv, "", lowOnly) + `">全部</a> `)
	catRows, cerr := db.DB.Query(`SELECT COALESCE(NULLIF(category,''),'__none__') c, COUNT(1) n
		FROM active_inventory_items GROUP BY 1 ORDER BY n DESC`)
	if cerr == nil {
		defer catRows.Close()
		for catRows.Next() {
			var c string
			var n int
			if catRows.Scan(&c, &n) != nil {
				continue
			}
			label := c
			if c == "__none__" {
				label = "未分類"
			}
			catTabs.WriteString(`<a class="btn ` + btnIf(category == c, "btn-p", "btn-s") + `" href="` +
				invTabURL(qv, c, lowOnly) + `">` + htmlEsc(label) + ` ` + strconv.Itoa(n) + `</a> `)
		}
	}

	lowCount := countRows("SELECT COUNT(1) FROM active_inventory_items WHERE COALESCE(stock_quantity,0) < COALESCE(min_safety_stock,0)")
	negCount := countRows("SELECT COUNT(1) FROM active_inventory_items WHERE COALESCE(stock_quantity,0) < 0")

	negBanner := ""
	if negCount > 0 {
		negBanner = `<div class="msg msg-err" style="display:flex;justify-content:space-between;align-items:center;gap:12px">
        <span>⚠ 有 <b>` + strconv.Itoa(negCount) + `</b> 個料號庫存為負數（舊系統進貨未登錄造成）。</span>
        <a class="btn btn-p" href="/inventory/negative">前往補正</a>
      </div>`
	}

	pages.ListInventoryPage(invRows, total, lowCount, negCount, page, size,
		q, lowOnly, showMoney, catTabs.String(), negBanner,
		lookupOptions("category", ""),
		user).Render(r.Context(), w)
}

// SearchInventoryAPI 庫存即時搜尋 API（HTMX 用）。
func SearchInventoryAPI(w http.ResponseWriter, r *http.Request, user model.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		pages.InventorySearchRows(nil, canSeeMoney(user)).Render(r.Context(), w)
		return
	}
	like := "%" + q + "%"
	costPrice := "0"
	showMoney := canSeeMoney(user)
	if showMoney {
		costPrice = "COALESCE(i.cost_price,0)"
	}
	rows, err := db.DB.Query(`SELECT i.id, i.name, i.part_number, COALESCE(i.category,''),
		COALESCE(i.bin_location,''), COALESCE(s.name,''),
		`+costPrice+`, COALESCE(i.selling_price,0),
		COALESCE(i.stock_quantity,0), COALESCE(i.min_safety_stock,0),
		COALESCE(i.stock_quantity,0) < COALESCE(i.min_safety_stock,0),
		COALESCE(i.item_kind,'')
		FROM active_inventory_items i
		LEFT JOIN active_suppliers s ON s.id = i.supplier_id
		WHERE i.name LIKE ? OR i.part_number LIKE ? OR COALESCE(i.barcode,'') LIKE ? OR i.bin_location LIKE ?
		ORDER BY i.name LIMIT 50`, like, like, like, like)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.InventoryRow
	for rows.Next() {
		var it pages.InventoryRow
		if err := rows.Scan(&it.ID, &it.Name, &it.PartNumber, &it.Category, &it.BinLocation,
			&it.SupplierName, &it.CostPrice, &it.SellingPrice, &it.StockQuantity, &it.MinSafety,
			&it.LowStock, &it.ItemKind); err != nil {
			continue
		}
		result = append(result, it)
	}
	pages.InventorySearchRows(result, showMoney).Render(r.Context(), w)
}

// invTabURL 產生庫存類別篩選連結，保留搜尋與低庫存條件。
func invTabURL(qv url.Values, category string, lowOnly bool) string {
	out := url.Values{}
	if q := qv.Get("q"); q != "" {
		out.Set("q", q)
	}
	if lowOnly {
		out.Set("low", "1")
	}
	if category != "" {
		out.Set("category", category)
	}
	if s := out.Encode(); s != "" {
		return "/inventory?" + s
	}
	return "/inventory"
}

func CreateInventoryItem(w http.ResponseWriter, r *http.Request) {
	part := strings.TrimSpace(r.FormValue("part_number"))
	name := strings.TrimSpace(r.FormValue("name"))
	bin := strings.TrimSpace(r.FormValue("bin_location"))
	category := strings.TrimSpace(r.FormValue("category"))

	if part == "" || name == "" || bin == "" {
		fail(w, "料號、名稱、庫位為必填")
		return
	}

	var supArg interface{}
	if s := mustInt(r.FormValue("supplier_id")); s > 0 {
		supArg = s
	}

	_, err := db.DB.Exec(`INSERT INTO inventory_items
		(supplier_id, part_number, barcode, name, bin_location, category, cost_price, selling_price,
		 stock_quantity, min_safety_stock)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		supArg, part, nullStr(r.FormValue("barcode")), name, bin, nullStr(category),
		atofDefault(r.FormValue("cost_price"), 0), atofDefault(r.FormValue("selling_price"), 0),
		atoiDefault(r.FormValue("stock_quantity"), 0), atoiDefault(r.FormValue("min_safety_stock"), 5))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "料號已存在")
			return
		}
		fail(w, "新增零件失敗")
		return
	}

	redirectMsg(w, r, "/inventory", "零件已新增")
}

func EditInventoryForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+inventoryCols+` FROM active_inventory_items i
		LEFT JOIN active_suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id)
	it, err := scanInventoryItem(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pages.EditInventoryPage(it, canSeeMoney(user), lookupOptions("category", it.Category), supplierOptionsSel(it.SupplierID), user).Render(r.Context(), w)
}

func UpdateInventoryItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	part := strings.TrimSpace(r.FormValue("part_number"))
	name := strings.TrimSpace(r.FormValue("name"))
	bin := strings.TrimSpace(r.FormValue("bin_location"))

	if part == "" || name == "" || bin == "" {
		fail(w, "料號、名稱、庫位為必填")
		return
	}

	var supArg interface{}
	if s := mustInt(r.FormValue("supplier_id")); s > 0 {
		supArg = s
	}

	res, err := db.DB.Exec(`UPDATE inventory_items SET supplier_id=?, part_number=?, barcode=?,
		name=?, category=?, bin_location=?, cost_price=?, selling_price=?, min_safety_stock=? WHERE id=?`,
		supArg, part, nullStr(r.FormValue("barcode")), name, nullStr(r.FormValue("category")), bin,
		atofDefault(r.FormValue("cost_price"), 0), atofDefault(r.FormValue("selling_price"), 0),
		atoiDefault(r.FormValue("min_safety_stock"), 5), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "料號已存在")
			return
		}
		fail(w, "更新零件失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/inventory", "零件已更新")
}

func StockAdjustForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+inventoryCols+` FROM active_inventory_items i
		LEFT JOIN active_suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id)
	it, err := scanInventoryItem(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pages.StockAdjustPage(it, user).Render(r.Context(), w)
}

func ApplyStockAdjust(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	delta := atoiDefault(r.FormValue("delta"), 0)

	if delta == 0 {
		fail(w, "異動數量不可為 0")
		return
	}
	if delta == -int(^uint(0)>>1)-1 {
		fail(w, "異動數量超出範圍")
		return
	}

	reason := strings.TrimSpace(r.FormValue("note"))
	if reason == "" {
		reason = "手動調庫"
	}
	actorID := int64(0)
	if u, uerr := currentUser(r); uerr == nil {
		actorID = u.ID
	}

	// 改庫存與寫異動紀錄必須同進同出，否則會出現「庫存變了但查不到原因」
	tx, err := db.BeginImmediate()
	if err != nil {
		fail(w, "調庫失敗")
		return
	}
	defer tx.Rollback()

	var after int
	var updateErr error
	if delta < 0 {
		needed := -delta
		updateErr = tx.QueryRow(`UPDATE inventory_items SET stock_quantity = stock_quantity - ?
			WHERE id = ? AND stock_quantity >= ? RETURNING stock_quantity`, needed, id, needed).Scan(&after)
	} else {
		updateErr = tx.QueryRow(`UPDATE inventory_items SET stock_quantity = stock_quantity + ?
			WHERE id = ? RETURNING stock_quantity`, delta, id).Scan(&after)
	}
	if updateErr == sql.ErrNoRows {
		var current int
		if err := tx.QueryRow("SELECT COALESCE(stock_quantity,0) FROM active_inventory_items WHERE id = ?", id).Scan(&current); err != nil {
			http.NotFound(w, r)
			return
		}
		if delta < 0 {
			fail(w, "庫存不足：目前 "+strconv.Itoa(current)+"，無法扣 "+strconv.Itoa(-delta)+
				"（若是舊系統留下的負庫存，請用「負庫存補正」）")
			return
		}
		fail(w, "調庫失敗")
		return
	}
	if updateErr != nil {
		fail(w, "調庫失敗")
		return
	}
	before := after - delta
	if err := recordMovement(tx, id, delta, before, after, reason, "manual", 0, actorID); err != nil {
		log.Printf("寫入庫存異動失敗: %v", err)
		fail(w, "寫入異動紀錄失敗")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, "調庫失敗")
		return
	}

	redirectMsg(w, r, "/inventory", "庫存已調整 "+strconv.Itoa(delta)+"，目前 "+strconv.Itoa(after))
}

func DeleteInventoryItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	used := countRows("SELECT COUNT(1) FROM work_order_items WHERE inventory_item_id = ?", id)
	if used > 0 {
		fail(w, "此零件已被工單引用，無法刪除")
		return
	}

	res, err := db.DB.Exec("UPDATE inventory_items SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		fail(w, "刪除零件失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/inventory", "零件已刪除")
}

func supplierOptions() string {
	rows, err := db.DB.Query("SELECT id, name FROM active_suppliers ORDER BY name LIMIT 300")
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(name) + `</option>`)
	}
	return b.String()
}

func supplierOptionsSel(selected int64) string {
	rows, err := db.DB.Query("SELECT id, name FROM active_suppliers ORDER BY name LIMIT 300")
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			continue
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `"` + selIf(id == selected) + `>` + htmlEsc(name) + `</option>`)
	}
	return b.String()
}

func inventoryOptions() string {
	rows, err := db.DB.Query(`SELECT id, name, part_number, COALESCE(selling_price,0), COALESCE(stock_quantity,0)
		FROM active_inventory_items ORDER BY name LIMIT 300`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name, part string
		var sell float64
		var stock int
		if err := rows.Scan(&id, &name, &part, &sell, &stock); err != nil {
			continue
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(name) + ` / ` + htmlEsc(part) +
			` / 售價 ` + money(sell) + ` / 庫存 ` + strconv.Itoa(stock) + `</option>`)
	}
	return b.String()
}

// SearchPartsAPI 零件即時搜尋 API，回傳 JSON。
// 給工單新增零件時的前端 autocomplete 使用。
func SearchPartsAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	like := "%" + q + "%"
	// T2.23: 加入庫位 bin_location，讓掃描條碼後可顯示庫位位置
	rows, err := db.DB.Query(`SELECT id, name, part_number, COALESCE(bin_location,''),
		COALESCE(selling_price,0), COALESCE(stock_quantity,0)
		FROM active_inventory_items
		WHERE name LIKE ? OR part_number LIKE ? OR COALESCE(barcode,'') LIKE ?
		ORDER BY name LIMIT 20`, like, like, like)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, []interface{}{})
		return
	}
	defer rows.Close()

	type partResult struct {
		ID          int64   `json:"id"`
		Name        string  `json:"name"`
		PartNumber  string  `json:"part_number"`
		BinLocation string  `json:"bin_location"`
		Price       float64 `json:"price"`
		Stock       int     `json:"stock"`
	}
	var results []partResult
	for rows.Next() {
		var p partResult
		if err := rows.Scan(&p.ID, &p.Name, &p.PartNumber, &p.BinLocation, &p.Price, &p.Stock); err != nil {
			continue
		}
		results = append(results, p)
	}
	writeJSON(w, http.StatusOK, results)
}

// costInput 產生成本價欄位；技師看不到此欄位。
func costInput(value string) string {
	return `<label>成本價<input name="cost_price" type="number" step="1" value="` + value + `"></label>`
}

func chkIf(cond bool) string {
	if cond {
		return " checked"
	}
	return ""
}
