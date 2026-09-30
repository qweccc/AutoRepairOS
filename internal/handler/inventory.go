package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

const inventoryCols = `i.id, COALESCE(i.supplier_id,0), COALESCE(s.name,''), i.part_number,
	COALESCE(i.barcode,''), i.name, i.bin_location, COALESCE(i.cost_price,0),
	COALESCE(i.selling_price,0), COALESCE(i.stock_quantity,0), COALESCE(i.min_safety_stock,0)`

func scanInventoryItem(rows interface{ Scan(...interface{}) error }) (model.InventoryItem, error) {
	var it model.InventoryItem
	err := rows.Scan(&it.ID, &it.SupplierID, &it.SupplierName, &it.PartNumber, &it.Barcode,
		&it.Name, &it.BinLocation, &it.CostPrice, &it.SellingPrice,
		&it.StockQuantity, &it.MinSafetyStock)
	it.LowStock = it.StockQuantity < it.MinSafetyStock
	return it, err
}

func ListInventory(w http.ResponseWriter, r *http.Request, user model.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	lowOnly := r.URL.Query().Get("low") == "1"

	query := `SELECT ` + inventoryCols + ` FROM inventory_items i
		LEFT JOIN suppliers s ON s.id = i.supplier_id WHERE 1=1`
	var args []interface{}
	if q != "" {
		query += ` AND (i.name LIKE ? OR i.part_number LIKE ? OR COALESCE(i.barcode,'') LIKE ? OR i.bin_location LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	if lowOnly {
		query += ` AND COALESCE(i.stock_quantity,0) < COALESCE(i.min_safety_stock,0)`
	}
	query += ` ORDER BY i.name`

	rows, err := db.DB.Query(query, args...)
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

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>料號</th><th>名稱</th><th>庫位</th><th>供應商</th><th class="num">成本</th><th class="num">售價</th><th class="num">庫存</th><th class="num">安全值</th><th>操作</th></tr>`)
	for _, it := range list {
		stockCell := strconv.Itoa(it.StockQuantity)
		if it.LowStock {
			stockCell = `<b style="color:#f87171">` + stockCell + ` ⚠</b>`
		}
		tb.WriteString(`<tr><td><code>` + htmlEsc(it.PartNumber) + `</code></td><td>` + htmlEsc(it.Name) +
			`</td><td>` + htmlEsc(it.BinLocation) + `</td><td>` + htmlEsc(orDash(it.SupplierName)) +
			`</td><td class="num">` + money(it.CostPrice) + `</td><td class="num">` + money(it.SellingPrice) +
			`</td><td class="num">` + stockCell + `</td><td class="num">` + strconv.Itoa(it.MinSafetyStock) +
			`</td><td><a class="btn btn-s" href="/inventory/` + strconv.FormatInt(it.ID, 10) + `/edit">編輯</a> ` +
			`<a class="btn btn-o" href="/inventory/` + strconv.FormatInt(it.ID, 10) + `/stock">調庫</a> ` +
			`<a class="btn btn-d" href="/inventory/` + strconv.FormatInt(it.ID, 10) + `/delete" onclick="return confirm('確定刪除此零件？')">刪除</a></td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有零件資料</div>`
	}

	lowCount := countRows("SELECT COUNT(1) FROM inventory_items WHERE COALESCE(stock_quantity,0) < COALESCE(min_safety_stock,0)")
	lowNote := ""
	if lowOnly {
		lowNote = ` <a href="/inventory" style="font-size:13px">（顯示全部）</a>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>新增零件</h2>
      <form class="frm" method="POST" action="/inventory">
        <label>料號<input name="part_number" required></label>
        <label>名稱<input name="name" required></label>
        <label>庫位<input name="bin_location" required placeholder="例 A-01-3"></label>
        <label>條碼<input name="barcode"></label>
        <label>供應商<select name="supplier_id"><option value="0">無</option>` + supplierOptions() + `</select></label>
        <label>成本價<input name="cost_price" type="number" min="0" step="1" value="0"></label>
        <label>售價<input name="selling_price" type="number" min="0" step="1" value="0"></label>
        <label>庫存數量<input name="stock_quantity" type="number" value="0"></label>
        <label>安全庫存<input name="min_safety_stock" type="number" min="0" value="5"></label>
        <div class="full"><button class="btn btn-p" type="submit">新增零件</button></div>
      </form>
    </div>
    <div class="card">
      <h2>零件列表（` + strconv.Itoa(len(list)) + ` 筆，低庫存 ` + strconv.Itoa(lowCount) + ` 種）` + lowNote + `</h2>
      <form method="GET" action="/inventory" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋名稱 / 料號 / 條碼 / 庫位" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:300px">
        <label style="font-size:13px;margin-left:10px"><input type="checkbox" name="low" value="1"` + chkIf(lowOnly) + `> 只看低庫存</label>
        <button class="btn btn-p" type="submit">搜尋</button>
      </form>` + table + `
    </div>`

	renderPage(w, "庫存管理", "/inventory", user.Username, user.Role, body)
}

func CreateInventoryItem(w http.ResponseWriter, r *http.Request) {
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

	_, err := db.DB.Exec(`INSERT INTO inventory_items
		(supplier_id, part_number, barcode, name, bin_location, cost_price, selling_price,
		 stock_quantity, min_safety_stock)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		supArg, part, nullStr(r.FormValue("barcode")), name, bin,
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

	row := db.DB.QueryRow(`SELECT `+inventoryCols+` FROM inventory_items i
		LEFT JOIN suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id)
	it, err := scanInventoryItem(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := `
    <div class="card">
      <h2>編輯零件 ` + htmlEsc(it.Name) + `</h2>
      <form class="frm" method="POST" action="/inventory/` + strconv.FormatInt(it.ID, 10) + `">
        <label>料號<input name="part_number" value="` + htmlEsc(it.PartNumber) + `" required></label>
        <label>名稱<input name="name" value="` + htmlEsc(it.Name) + `" required></label>
        <label>庫位<input name="bin_location" value="` + htmlEsc(it.BinLocation) + `" required></label>
        <label>條碼<input name="barcode" value="` + htmlEsc(it.Barcode) + `"></label>
        <label>供應商<select name="supplier_id"><option value="0">無</option>` + supplierOptionsSel(it.SupplierID) + `</select></label>
        <label>成本價<input name="cost_price" type="number" step="1" value="` + money(it.CostPrice) + `"></label>
        <label>售價<input name="selling_price" type="number" step="1" value="` + money(it.SellingPrice) + `"></label>
        <label>安全庫存<input name="min_safety_stock" type="number" min="0" value="` + strconv.Itoa(it.MinSafetyStock) + `"></label>
        <div class="full"><button class="btn btn-p" type="submit">儲存</button> <a class="btn btn-s" href="/inventory">取消</a></div>
      </form>
      <p style="color:#64748b;font-size:12.5px;margin-top:10px">庫存數量請用「調庫」功能變更，以保留進出紀錄。</p>
    </div>`

	renderPage(w, "編輯零件", "/inventory", user.Username, user.Role, body)
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
		name=?, bin_location=?, cost_price=?, selling_price=?, min_safety_stock=? WHERE id=?`,
		supArg, part, nullStr(r.FormValue("barcode")), name, bin,
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

	row := db.DB.QueryRow(`SELECT `+inventoryCols+` FROM inventory_items i
		LEFT JOIN suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id)
	it, err := scanInventoryItem(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := `
    <div class="card">
      <h2>調庫 — ` + htmlEsc(it.Name) + `（` + htmlEsc(it.PartNumber) + `）</h2>
      <p style="color:#94a3b8;font-size:13.5px">目前庫存：<b style="color:#f8fafc;font-size:20px">` +
		strconv.Itoa(it.StockQuantity) + `</b>　安全庫存：` + strconv.Itoa(it.MinSafetyStock) + `</p>
      <form class="frm" method="POST" action="/inventory/` + strconv.FormatInt(it.ID, 10) + `/stock">
        <label>異動數量（正數進貨、負數領用）<input name="delta" type="number" required placeholder="例 10 或 -2"></label>
        <label>備註<input name="note" placeholder="例：採購進貨 / 工單領用"></label>
        <div class="full"><button class="btn btn-p" type="submit">確認調庫</button> <a class="btn btn-s" href="/inventory">取消</a></div>
      </form>
    </div>`

	renderPage(w, "調庫", "/inventory", user.Username, user.Role, body)
}

func ApplyStockAdjust(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	delta := atoiDefault(r.FormValue("delta"), 0)

	if delta == 0 {
		fail(w, "異動數量不可為 0")
		return
	}

	var current int
	if err := db.DB.QueryRow("SELECT COALESCE(stock_quantity,0) FROM inventory_items WHERE id = ?", id).Scan(&current); err != nil {
		http.NotFound(w, r)
		return
	}

	if current+delta < 0 {
		fail(w, "庫存不足：目前 "+strconv.Itoa(current)+"，無法扣 "+strconv.Itoa(-delta))
		return
	}

	if _, err := db.DB.Exec("UPDATE inventory_items SET stock_quantity = stock_quantity + ? WHERE id = ?", delta, id); err != nil {
		fail(w, "調庫失敗")
		return
	}

	redirectMsg(w, r, "/inventory", "庫存已調整 "+strconv.Itoa(delta)+"，目前 "+strconv.Itoa(current+delta))
}

func DeleteInventoryItem(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	used := countRows("SELECT COUNT(1) FROM work_order_items WHERE inventory_item_id = ?", id)
	if used > 0 {
		fail(w, "此零件已被工單引用，無法刪除")
		return
	}

	res, err := db.DB.Exec("DELETE FROM inventory_items WHERE id = ?", id)
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
	rows, err := db.DB.Query("SELECT id, name FROM suppliers ORDER BY name")
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
	rows, err := db.DB.Query("SELECT id, name FROM suppliers ORDER BY name")
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
		FROM inventory_items ORDER BY name`)
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

func chkIf(cond bool) string {
	if cond {
		return " checked"
	}
	return ""
}
