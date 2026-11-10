package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
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
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_inventory_items`).Scan(&s.TotalAll)
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_inventory_items
		WHERE COALESCE(stock_quantity,0) < 0`).Scan(&s.Total)
	db.DB.QueryRow(`SELECT COALESCE(MIN(stock_quantity),0) FROM active_inventory_items
		WHERE COALESCE(stock_quantity,0) < 0`).Scan(&s.WorstQty)
	db.DB.QueryRow(`SELECT COALESCE(SUM(-stock_quantity),0) FROM active_inventory_items
		WHERE COALESCE(stock_quantity,0) < 0`).Scan(&s.SumDeficit)
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_inventory_items
		WHERE COALESCE(stock_quantity,0) < 0 AND COALESCE(selling_price,0) > 0`).Scan(&s.SellableOnly)
	db.DB.QueryRow(`SELECT COUNT(1) FROM active_inventory_items
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
	total := countRows(`SELECT COUNT(1) FROM active_inventory_items i`+where, args...)

	query := `SELECT ` + inventoryCols + ` FROM active_inventory_items i
		LEFT JOIN active_suppliers s ON s.id = i.supplier_id` + where + order +
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

	var pageRows []pages.NegativeStockRow
	for _, it := range list {
		suggest := -it.StockQuantity
		if it.MinSafetyStock > 0 {
			suggest = it.MinSafetyStock - it.StockQuantity
		}
		pageRows = append(pageRows, pages.NegativeStockRow{ID: it.ID, PartNumber: it.PartNumber,
			Name: it.Name, BinLocation: orDash(it.BinLocation), SupplierName: orDash(it.SupplierName),
			SellingPrice: money(it.SellingPrice), StockQuantity: it.StockQuantity, Suggested: suggest})
	}
	pages.NegativeStockPage(pages.NegativeStockStats{
		Total: stats.Total, TotalAll: stats.TotalAll, WorstQty: stats.WorstQty,
		SumDeficit: stats.SumDeficit, SellableOnly: stats.SellableOnly, WithSupplier: stats.WithSupplier,
	}, pageRows, total, page, size, q, sortBy, hiddenInputs(qv, "q", "page", "per"),
		pager("/inventory/negative", qv, total, page, size), perPageSel("/inventory/negative", qv, size),
		flash(r), user).Render(r.Context(), w)
}

// NegativeStockFixForm 單筆補正表單。
func NegativeStockFixForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.URL.Query().Get("id"))

	row := db.DB.QueryRow(`SELECT `+inventoryCols+` FROM active_inventory_items i
		LEFT JOIN active_suppliers s ON s.id = i.supplier_id WHERE i.id = ?`, id)
	it, err := scanInventoryItem(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	pages.NegativeStockFixPage(it, recentMovements(id, 10), user).Render(r.Context(), w)
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
	if err := db.DB.QueryRow("SELECT COALESCE(stock_quantity,0) FROM active_inventory_items WHERE id = ?", id).
		Scan(&current); err != nil {
		http.NotFound(w, r)
		return
	}

	var minSafe int
	db.DB.QueryRow("SELECT COALESCE(min_safety_stock,0) FROM active_inventory_items WHERE id = ?", id).Scan(&minSafe)

	var target int
	fixPath := "/inventory/negative?id=" + strconv.FormatInt(id, 10)
	switch mode {
	case "zero":
		target = 0
	case "safety":
		target = minSafe
	case "custom":
		target = atoiDefault(r.FormValue("target"), -1)
		if target < 0 {
			formError(w, r, "負庫存補正", fixPath, "指定數量不可為負數")
			return
		}
	default:
		formError(w, r, "負庫存補正", fixPath, "未知的補正方式")
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
		JOIN active_inventory_items i ON i.id = m.inventory_item_id
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

	var pageRows []pages.InventoryMovementRow
	for rows.Next() {
		var mid, itemID int64
		var part, name, reason, src, uname, createdAt string
		var delta, before, after int
		var woID int64
		if err := rows.Scan(&mid, &itemID, &part, &name, &delta, &before, &after,
			&reason, &src, &woID, &uname, &createdAt); err != nil {
			continue
		}
		srcText := sourceLabel[src]
		if srcText == "" {
			srcText = src
		}
		pageRows = append(pageRows, pages.InventoryMovementRow{CreatedAt: createdAt, PartNumber: part,
			Name: name, Source: srcText, Reason: orDash(reason), User: orDash(uname),
			Delta: delta, Before: before, After: after, DeltaPositive: delta >= 0})
	}
	pages.MovementLogPage(pageRows, total, page, size, q, source,
		hiddenInputs(qv, "q", "page", "per"), pager("/inventory/movements", qv, total, page, size),
		perPageSel("/inventory/movements", qv, size), user).Render(r.Context(), w)
}

// recentMovements 某料號的近期異動（HTML 片段）。
func recentMovements(itemID int64, limit int) []pages.InventoryMovementRow {
	rows, err := db.DB.Query(`SELECT m.delta, m.before_quantity, m.after_quantity,
		COALESCE(m.reason,''), m.source, COALESCE(u.name,''), m.created_at
		FROM inventory_movements m
		LEFT JOIN users u ON u.id = m.created_by
		WHERE m.inventory_item_id = ?
		ORDER BY m.id DESC LIMIT ?`, itemID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []pages.InventoryMovementRow
	for rows.Next() {
		var delta, before, after int
		var reason, src, uname, createdAt string
		if err := rows.Scan(&delta, &before, &after, &reason, &src, &uname, &createdAt); err != nil {
			continue
		}
		result = append(result, pages.InventoryMovementRow{CreatedAt: createdAt, Delta: delta,
			Before: before, After: after, Source: src, Reason: orDash(reason), User: orDash(uname),
			DeltaPositive: delta >= 0})
	}
	return result
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
