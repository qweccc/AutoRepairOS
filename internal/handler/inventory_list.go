package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/components"
	"autorepairos/web/pages"
)

func ListInventory(w http.ResponseWriter, r *http.Request, user model.User) {
	renderInventoryList(w, r, user, pages.InventoryCreateForm{})
}

// renderInventoryList queries and renders the inventory list page. Shared by
// ListInventory and CreateInventoryItem's error path so a failed POST
// re-renders the same templ page with the error and input preserved.
func renderInventoryList(w http.ResponseWriter, r *http.Request, user model.User, form pages.InventoryCreateForm) {
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
		lookupOptions("category", ""), form,
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
	showMoney := user.Role == "owner"
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

// SearchPartsAPI 零件即時搜尋 API。HTMX 請求回傳結果 fragment，其餘
// client 維持原本的 JSON response。
func SearchPartsAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		if r.Header.Get("HX-Request") == "true" {
			// For HX requests, return a few recent items to help users
			rows, err := db.DB.Query(`SELECT id, name, part_number, COALESCE(bin_location,''),
				COALESCE(selling_price,0), COALESCE(stock_quantity,0)
				FROM active_inventory_items
				ORDER BY id DESC LIMIT 20`)
			if err != nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_ = components.PartLookupResults(nil).Render(r.Context(), w)
				return
			}
			defer rows.Close()
			type partResult struct {
				ID          int64
				Name        string
				PartNumber  string
				BinLocation string
				Price       float64
				Stock       int
			}
			var results []partResult
			for rows.Next() {
				var p partResult
				if err := rows.Scan(&p.ID, &p.Name, &p.PartNumber, &p.BinLocation, &p.Price, &p.Stock); err != nil {
					continue
				}
				results = append(results, p)
			}
			fragment := make([]components.PartLookupResult, 0, len(results))
			for _, p := range results {
				fragment = append(fragment, components.PartLookupResult{
					ID: p.ID, Name: p.Name, PartNumber: p.PartNumber,
					BinLocation: p.BinLocation, Price: money(p.Price), Stock: p.Stock,
				})
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if err := components.PartLookupResults(fragment).Render(r.Context(), w); err != nil {
				http.Error(w, "渲染零件結果失敗", http.StatusInternalServerError)
			}
			return
		}
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
		if r.Header.Get("HX-Request") == "true" {
			http.Error(w, "查詢零件失敗", http.StatusInternalServerError)
			return
		}
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
	if r.Header.Get("HX-Request") == "true" {
		fragment := make([]components.PartLookupResult, 0, len(results))
		for _, p := range results {
			fragment = append(fragment, components.PartLookupResult{
				ID: p.ID, Name: p.Name, PartNumber: p.PartNumber,
				BinLocation: p.BinLocation, Price: money(p.Price), Stock: p.Stock,
			})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := components.PartLookupResults(fragment).Render(r.Context(), w); err != nil {
			http.Error(w, "渲染零件結果失敗", http.StatusInternalServerError)
		}
		return
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
