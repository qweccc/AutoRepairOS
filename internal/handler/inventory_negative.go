package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// 舊系統負庫存補正工具。
//
// 背景：CAR35 時代進貨常常沒登錄就直接出料，導致匯入後庫存有 38.7% 是負數
// （例如「電源線 -117」）。負庫存會讓扣料邏輯壞掉，也讓庫存報表失去意義。
//
// 設計原則：
//   1. 不猜。只提供「設為 0」「設為安全庫存」兩種明確選項，
//      要填實際數量就一筆一筆來（或走批次）。
//   2. 留痕。每次補正都寫一筆 inventory_movements（source=legacy_fix），
//      並記下補正前的原值，必要時可回復。
//   3. 看得到全貌。列表要能一眼看出「負多少、涉及多少料號、
//      是不是有在賣的熱門件」——補正順序應該從熱門件先做。

// negativeStockStats 負庫存概況。
type negativeStockStats struct {
	Total        int // 負庫存料號數
	TotalAll     int // 全部料號數
	WorstQty     int // 最負的數量
	SumDeficit   int // 缺口總和（取絕對值）
	SellableOnly int // 其中有售價（代表有在賣）的數量
	WithSupplier int // 其中有指定供應商的數量
}

func loadNegativeStockStats() negativeStockStats {
	var s negativeStockStats
	db.DB.QueryRow(`SELECT COUNT(1) FROM inventory_items`).Scan(&s.TotalAll)
	db.DB.QueryRow(`SELECT COUNT(1) FROM inventory_items
		WHERE COALESCE(stock_quantity,0) < 0`).Scan(&s.Total)
	db.DB.QueryRow(`SELECT COALESCE(MIN(stock_quantity),0) FROM inventory_items
		WHERE COALESCE(stock_quantity,0) < 0`).Scan(&s.WorstQty)
	db.DB.QueryRow(`SELECT COALESCE(SUM(-stock_quantity),0) FROM inventory_items
		WHERE COALESCE(stock_quantity,0) < 0`).Scan(&s.SumDeficit)
	db.DB.QueryRow(`SELECT COUNT(1) FROM inventory_items
		WHERE COALESCE(stock_quantity,0) < 0 AND COALESCE(selling_price,0) > 0`).Scan(&s.SellableOnly)
	db.DB.QueryRow(`SELECT COUNT(1) FROM inventory_items
		WHERE COALESCE(stock_quantity,0) < 0 AND supplier_id IS NOT NULL`).Scan(&s.WithSupplier)
	return s
}

// NegativeStockList 負庫存補正頁。
//
// 帶 ?id=N 時顯示該料號的補正表單（見 NegativeStockFixForm）。
func NegativeStockList(w http.ResponseWriter, r *http.Request, user model.User) {
	if id := mustInt(r.URL.Query().Get("id")); id > 0 {
		NegativeStockFixForm(w, r, user)
		return
	}

	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	sortBy := qv.Get("sort")
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE COALESCE(i.stock_quantity,0) < 0`
	var args []interface{}
	if q != "" {
		where += ` AND (i.name LIKE ? OR i.part_number LIKE ? OR COALESCE(i.bin_location,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if sortBy == "sellable" {
		where += ` AND COALESCE(i.selling_price,0) > 0`
	}

	order := ` ORDER BY i.stock_quantity ASC, i.name`
	switch sortBy {
	case "name":
		order = ` ORDER BY i.name`
	case "sellable":
		order = ` ORDER BY i.selling_price DESC, i.stock_quantity ASC`
	case "deficit":
		order = ` ORDER BY i.stock_quantity ASC, i.name`
	}

	stats := loadNegativeStockStats()
	total := countRows(`SELECT COUNT(1) FROM inventory_items i`+where, args...)

	query := `SELECT ` + inventoryCols + ` FROM inventory_items i
		LEFT JOIN suppliers s ON s.id = i.supplier_id` + where + order +
		` LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢負庫存失敗")
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
	tb.WriteString(`<table><tr><th>料號</th><th>名稱</th><th>庫位</th><th>供應商</th>` +
		`<th class="num">售價</th><th class="num">目前庫存</th><th class="num">建議補正</th><th>操作</th></tr>`)
	for _, it := range list {
		suggest := -it.StockQuantity
		if it.MinSafetyStock > 0 {
			suggest = it.MinSafetyStock - it.StockQuantity
		}
		tb.WriteString(`<tr><td><code>` + htmlEsc(it.PartNumber) + `</code></td><td>` + htmlEsc(it.Name) +
			`</td><td>` + orDash(htmlEsc(it.BinLocation)) + `</td><td>` + orDash(htmlEsc(it.SupplierName)) +
			`</td><td class="num">` + money(it.SellingPrice) +
			`</td><td class="num"><b style="color:#f87171">` + strconv.Itoa(it.StockQuantity) +
			`</b></td><td class="num" style="color:#86efac">+` + strconv.Itoa(suggest) +
			`</td><td>` +
			`<a class="btn btn-p" href="/inventory/negative?id=` + strconv.FormatInt(it.ID, 10) + `">補正</a></td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有符合條件的負庫存料號</div>`
	}

	// 篩選籤
	tab := func(label, key string) string {
		cls := "btn-s"
		if sortBy == key {
			cls = "btn-p"
		}
		u := url.Values{}
		if q != "" {
			u.Set("q", q)
		}
		if key != "" {
			u.Set("sort", key)
		}
		href := "/inventory/negative"
		if s := u.Encode(); s != "" {
			href += "?" + s
		}
		return `<a class="btn ` + cls + `" href="` + href + `">` + label + `</a>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>負庫存補正</h2>
      <p style="color:#94a3b8;font-size:13.5px;margin:0 0 14px">
        舊系統（CAR35）進貨常未登錄就直接出料，匯入後留下負庫存。
        負庫存會讓扣料與庫存報表失準，建議從「有在賣的熱門件」開始補正。<br>
        <b>補正會留下紀錄</b>（誰、何時、從多少改成多少），日後可追溯。
      </p>
      <div style="margin-bottom:14px">
        <div class="stat"><b>` + strconv.Itoa(stats.Total) + `</b><span>負庫存料號</span></div>
        <div class="stat"><b>` + strconv.Itoa(stats.TotalAll) + `</b><span>全部料號</span></div>
        <div class="stat"><b style="color:#f87171">` + strconv.Itoa(stats.WorstQty) + `</b><span>最負數量</span></div>
        <div class="stat"><b>` + strconv.Itoa(stats.SumDeficit) + `</b><span>缺口總和</span></div>
        <div class="stat"><b>` + strconv.Itoa(stats.SellableOnly) + `</b><span>其中有售價</span></div>
        <div class="stat"><b>` + strconv.Itoa(stats.WithSupplier) + `</b><span>其中有供應商</span></div>
      </div>
      <div style="margin-bottom:12px">` +
		tab("缺口最大優先", "") + ` ` + tab("有售價優先", "sellable") + ` ` + tab("依名稱", "name") +
		`　<a class="btn btn-s" href="/inventory">回庫存列表</a>
      </div>
      <form method="GET" action="/inventory/negative" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋料號 / 名稱 / 庫位" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:280px">
        ` + hiddenInputs(qv, "q", "page", "per") + `
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/inventory/negative">清除</a>
      </form>
      <h2 style="font-size:15px">符合條件 ` + strconv.Itoa(total) + ` 筆</h2>` + table +
		pager("/inventory/negative", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/inventory/negative", qv, size) + `</div>
    </div>`

	renderPage(w, "負庫存補正", "/inventory", user.Username, user.Role, body)
}

// NegativeStockFixForm 單筆補正表單。
func NegativeStockFixForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.URL.Query().Get("id"))

	row := db.DB.QueryRow(`SELECT `+inventoryCols+` FROM inventory_items i
		LEFT JOIN suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id)
	it, err := scanInventoryItem(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 近期異動紀錄，讓使用者判斷該補多少
	movs := recentMovements(id, 10)

	body := `
    <div class="card">
      <h2>負庫存補正 — ` + htmlEsc(it.Name) + `（` + htmlEsc(it.PartNumber) + `）</h2>
      <p style="color:#94a3b8;font-size:13.5px">
        目前庫存：<b style="color:#f87171;font-size:20px">` + strconv.Itoa(it.StockQuantity) + `</b>　
        安全庫存：` + strconv.Itoa(it.MinSafetyStock) + `　
        售價：` + money(it.SellingPrice) + `
      </p>
      <form class="frm" method="POST" action="/inventory/negative/fix">
        <input type="hidden" name="item_id" value="` + strconv.FormatInt(it.ID, 10) + `">
        <label>補正方式
          <select name="mode" id="fixMode" onchange="document.getElementById('customRow').style.display=(this.value=='custom')?'block':'none'">
            <option value="zero">設為 0（歸零，不補貨）</option>
            <option value="safety">設為安全庫存（` + strconv.Itoa(it.MinSafetyStock) + `）</option>
            <option value="custom">設為指定數量</option>
          </select>
        </label>
        <label id="customRow" style="display:none">指定數量<input name="target" type="number" min="0" value="0"></label>
        <label class="full">補正原因<input name="reason" value="舊系統負庫存補正" placeholder="例：開店盤點實際庫存"></label>
        <div class="full">
          <button class="btn btn-p" type="submit">確認補正</button>
          <a class="btn btn-s" href="/inventory/negative">取消</a>
        </div>
      </form>
      <p style="color:#64748b;font-size:12.5px;margin-top:12px">
        補正會寫入庫存異動紀錄（source=legacy_fix），並保留補正前的原值以便回復。
      </p>
    </div>` + movs

	renderPage(w, "負庫存補正", "/inventory", user.Username, user.Role, body)
}

// ApplyNegativeStockFix 執行補正。
func ApplyNegativeStockFix(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.FormValue("item_id"))

	mode := r.FormValue("mode")
	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" {
		reason = "舊系統負庫存補正"
	}

	var current int
	if err := db.DB.QueryRow("SELECT COALESCE(stock_quantity,0) FROM inventory_items WHERE id = ?", id).
		Scan(&current); err != nil {
		http.NotFound(w, r)
		return
	}

	var minSafe int
	db.DB.QueryRow("SELECT COALESCE(min_safety_stock,0) FROM inventory_items WHERE id = ?", id).Scan(&minSafe)

	var target int
	switch mode {
	case "zero":
		target = 0
	case "safety":
		target = minSafe
	case "custom":
		target = atoiDefault(r.FormValue("target"), -1)
		if target < 0 {
			fail(w, "指定數量不可為負數")
			return
		}
	default:
		fail(w, "未知的補正方式")
		return
	}

	if target == current {
		redirectMsg(w, r, "/inventory/negative", "庫存已為 "+strconv.Itoa(target)+"，未變更")
		return
	}

	delta := target - current
	now := nowStamp()

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "補正失敗")
		return
	}
	defer tx.Rollback()

	// 保留補正前的原值（只在第一次補正時記錄，避免覆蓋掉最初的負值）
	if _, err := tx.Exec(`UPDATE inventory_items SET
		stock_quantity = ?,
		stock_corrected_at = ?,
		stock_corrected_from = COALESCE(stock_corrected_from, ?)
		WHERE id = ?`, target, now, current, id); err != nil {
		fail(w, "補正失敗")
		return
	}

	if _, err := tx.Exec(`INSERT INTO inventory_movements
		(inventory_item_id, delta, before_quantity, after_quantity, reason, source, created_by)
		VALUES (?,?,?,?,?,?,?)`,
		id, delta, current, target, reason, "legacy_fix", user.ID); err != nil {
		fail(w, "寫入異動紀錄失敗")
		return
	}

	if err := tx.Commit(); err != nil {
		fail(w, "補正失敗")
		return
	}

	redirectMsg(w, r, "/inventory/negative",
		"已補正："+strconv.Itoa(current)+" → "+strconv.Itoa(target))
}

// MovementLog 庫存異動紀錄頁。
func MovementLog(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	source := strings.TrimSpace(qv.Get("source"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (i.name LIKE ? OR i.part_number LIKE ? OR COALESCE(m.reason,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if source != "" {
		where += ` AND m.source = ?`
		args = append(args, source)
	}

	from := ` FROM inventory_movements m
		JOIN inventory_items i ON i.id = m.inventory_item_id
		LEFT JOIN users u ON u.id = m.created_by`

	total := countRows(`SELECT COUNT(1)`+from+where, args...)

	query := `SELECT m.id, m.inventory_item_id, i.part_number, i.name, m.delta,
		m.before_quantity, m.after_quantity, COALESCE(m.reason,''), m.source,
		COALESCE(m.work_order_id,0), COALESCE(u.name,''), m.created_at` +
		from + where + ` ORDER BY m.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢異動紀錄失敗")
		return
	}
	defer rows.Close()

	sourceLabel := map[string]string{
		"manual":     "手動調庫",
		"checkout":   "結帳扣料",
		"legacy_fix": "舊資料補正",
		"import":     "匯入",
	}

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>時間</th><th>料號</th><th>名稱</th><th>來源</th>` +
		`<th class="num">異動</th><th class="num">前</th><th class="num">後</th><th>原因</th><th>操作人</th></tr>`)
	n := 0
	for rows.Next() {
		var mid, itemID int64
		var part, name, reason, src, uname, createdAt string
		var delta, before, after int
		var woID int64
		if err := rows.Scan(&mid, &itemID, &part, &name, &delta, &before, &after,
			&reason, &src, &woID, &uname, &createdAt); err != nil {
			continue
		}
		n++
		deltaCell := `<b style="color:#86efac">+` + strconv.Itoa(delta) + `</b>`
		if delta < 0 {
			deltaCell = `<b style="color:#f87171">` + strconv.Itoa(delta) + `</b>`
		}
		srcText := sourceLabel[src]
		if srcText == "" {
			srcText = src
		}
		tb.WriteString(`<tr><td style="font-size:12.5px;color:#94a3b8">` + htmlEsc(createdAt) +
			`</td><td><code>` + htmlEsc(part) + `</code></td><td>` + htmlEsc(name) +
			`</td><td>` + srcText + `</td><td class="num">` + deltaCell +
			`</td><td class="num">` + strconv.Itoa(before) + `</td><td class="num">` + strconv.Itoa(after) +
			`</td><td style="font-size:12.5px">` + orDash(htmlEsc(reason)) +
			`</td><td style="font-size:12.5px">` + orDash(htmlEsc(uname)) + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if n == 0 {
		table = `<div class="empty">沒有符合條件的異動紀錄</div>`
	}

	tab := func(label, key string) string {
		cls := "btn-s"
		if source == key {
			cls = "btn-p"
		}
		u := url.Values{}
		if q != "" {
			u.Set("q", q)
		}
		if key != "" {
			u.Set("source", key)
		}
		href := "/inventory/movements"
		if s := u.Encode(); s != "" {
			href += "?" + s
		}
		return `<a class="btn ` + cls + `" href="` + href + `">` + label + `</a>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>庫存異動紀錄（符合條件 ` + strconv.Itoa(total) + ` 筆）</h2>
      <div style="margin-bottom:12px">` +
		tab("全部", "") + ` ` + tab("手動調庫", "manual") + ` ` + tab("結帳扣料", "checkout") +
		` ` + tab("舊資料補正", "legacy_fix") +
		`　<a class="btn btn-s" href="/inventory">回庫存列表</a>
      </div>
      <form method="GET" action="/inventory/movements" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋料號 / 名稱 / 原因" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:280px">
        ` + hiddenInputs(qv, "q", "page", "per") + `
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/inventory/movements">清除</a>
      </form>` + table + pager("/inventory/movements", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/inventory/movements", qv, size) + `</div>
    </div>`

	renderPage(w, "庫存異動紀錄", "/inventory", user.Username, user.Role, body)
}

// recentMovements 某料號的近期異動（HTML 片段）。
func recentMovements(itemID int64, limit int) string {
	rows, err := db.DB.Query(`SELECT m.delta, m.before_quantity, m.after_quantity,
		COALESCE(m.reason,''), m.source, COALESCE(u.name,''), m.created_at
		FROM inventory_movements m
		LEFT JOIN users u ON u.id = m.created_by
		WHERE m.inventory_item_id = ?
		ORDER BY m.id DESC LIMIT ?`, itemID, limit)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>時間</th><th class="num">異動</th><th class="num">前</th><th class="num">後</th><th>來源</th><th>原因</th><th>操作人</th></tr>`)
	n := 0
	for rows.Next() {
		var delta, before, after int
		var reason, src, uname, createdAt string
		if err := rows.Scan(&delta, &before, &after, &reason, &src, &uname, &createdAt); err != nil {
			continue
		}
		n++
		tb.WriteString(`<tr><td style="font-size:12.5px;color:#94a3b8">` + htmlEsc(createdAt) +
			`</td><td class="num">` + strconv.Itoa(delta) + `</td><td class="num">` + strconv.Itoa(before) +
			`</td><td class="num">` + strconv.Itoa(after) + `</td><td>` + htmlEsc(src) +
			`</td><td style="font-size:12.5px">` + orDash(htmlEsc(reason)) +
			`</td><td style="font-size:12.5px">` + orDash(htmlEsc(uname)) + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	if n == 0 {
		return `<div class="card"><h2>近期異動</h2><div class="empty">此料號沒有異動紀錄</div></div>`
	}
	return `<div class="card"><h2>近期異動（最近 ` + strconv.Itoa(limit) + ` 筆）</h2>` + tb.String() + `</div>`
}

// recordMovement 寫入一筆庫存異動（供其他 handler 呼叫）。
//
// 用同一個 transaction 執行器讓呼叫端把「改庫存」與「寫紀錄」放在一起，
// 避免只改了庫存卻沒留下紀錄。
func recordMovement(tx db.TxExecutor, itemID int64, delta, before, after int, reason, source string, workOrderID, userID int64) error {
	var woArg, userArg interface{}
	if workOrderID > 0 {
		woArg = workOrderID
	}
	if userID > 0 {
		userArg = userID
	}
	_, err := tx.Exec(`INSERT INTO inventory_movements
		(inventory_item_id, delta, before_quantity, after_quantity, reason, source,
		 work_order_id, created_by)
		VALUES (?,?,?,?,?,?,?,?)`,
		itemID, delta, before, after, reason, source, woArg, userArg)
	return err
}
