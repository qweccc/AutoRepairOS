package handler

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/service"
	"autorepairos/web/pages"
)

// 庫存盤點（FR-IN-06）：建單時快照帳面庫存，逐筆填入實盤量，
// 完成時以「絕對值校正」調整庫存並寫 inventory_movements（source=stocktake）。
// 負庫存 legacy 資料正是盤點要修正的對象，故完成調整不做库存不足檢查。

func loadStocktake(id int64) (model.Stocktake, error) {
	var st model.Stocktake
	err := db.DB.QueryRow(`SELECT st.id, st.code, st.scope_type, st.scope_value, st.status,
		COALESCE(st.note,''), st.item_count, COALESCE(st.created_by,0), COALESCE(st.completed_by,0),
		st.created_at, COALESCE(st.completed_at,''),
		(SELECT COUNT(1) FROM stocktake_items i WHERE i.stocktake_id=st.id AND i.counted_qty IS NOT NULL),
		(SELECT COUNT(1) FROM stocktake_items i WHERE i.stocktake_id=st.id AND i.counted_qty IS NOT NULL AND i.counted_qty <> i.system_qty)
		FROM stocktakes st WHERE st.id = ?`, id).
		Scan(&st.ID, &st.Code, &st.ScopeType, &st.ScopeValue, &st.Status,
			&st.Note, &st.ItemCount, &st.CreatedBy, &st.CompletedBy,
			&st.CreatedAt, &st.CompletedAt, &st.CountedCount, &st.DiffCount)
	return st, err
}

func loadStocktakeItems(stID int64, q string) ([]model.StocktakeItem, error) {
	query := `SELECT i.id, i.stocktake_id, i.inventory_item_id, i.part_number, i.name, i.bin_location,
		i.system_qty, i.counted_qty, COALESCE(i.counted_by,0), COALESCE(i.counted_at,'')
		FROM stocktake_items i
		LEFT JOIN inventory_items inv ON inv.id = i.inventory_item_id
		WHERE i.stocktake_id = ?`
	args := []interface{}{stID}
	if q != "" {
		query += ` AND (i.part_number LIKE ? OR i.name LIKE ? OR COALESCE(inv.barcode,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	query += ` ORDER BY i.bin_location, i.part_number, i.id`
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []model.StocktakeItem
	for rows.Next() {
		var it model.StocktakeItem
		var counted sql.NullInt64
		if err := rows.Scan(&it.ID, &it.StocktakeID, &it.InventoryItemID, &it.PartNumber, &it.Name, &it.BinLocation,
			&it.SystemQty, &counted, &it.CountedBy, &it.CountedAt); err != nil {
			return nil, err
		}
		if counted.Valid {
			v := int(counted.Int64)
			it.CountedQty = &v
		}
		list = append(list, it)
	}
	return list, rows.Err()
}

// ListStocktakes 盤點紀錄列表。
func ListStocktakes(w http.ResponseWriter, r *http.Request, user model.User) {
	rows, err := db.DB.Query(`SELECT st.id, st.code, st.scope_type, st.scope_value, st.status,
		COALESCE(st.note,''), st.item_count, COALESCE(st.created_by,0), COALESCE(st.completed_by,0),
		st.created_at, COALESCE(st.completed_at,''),
		(SELECT COUNT(1) FROM stocktake_items i WHERE i.stocktake_id=st.id AND i.counted_qty IS NOT NULL),
		(SELECT COUNT(1) FROM stocktake_items i WHERE i.stocktake_id=st.id AND i.counted_qty IS NOT NULL AND i.counted_qty <> i.system_qty)
		FROM stocktakes st ORDER BY st.created_at DESC, st.id DESC LIMIT 100`)
	if err != nil {
		log.Printf("讀取盤點紀錄失敗: %v", err)
		fail(w, "讀取盤點紀錄失敗")
		return
	}
	defer rows.Close()
	var list []model.Stocktake
	for rows.Next() {
		var st model.Stocktake
		if err := rows.Scan(&st.ID, &st.Code, &st.ScopeType, &st.ScopeValue, &st.Status,
			&st.Note, &st.ItemCount, &st.CreatedBy, &st.CompletedBy,
			&st.CreatedAt, &st.CompletedAt, &st.CountedCount, &st.DiffCount); err != nil {
			log.Printf("掃描盤點紀錄失敗: %v", err)
			fail(w, "讀取盤點紀錄失敗")
			return
		}
		list = append(list, st)
	}
	pages.ListStocktakesPage(list, user).Render(r.Context(), w)
}

// NewStocktakeForm 建立盤點單表單（範圍：全庫／分類／庫位）。
func NewStocktakeForm(w http.ResponseWriter, r *http.Request, user model.User) {
	var bins []string
	rows, err := db.DB.Query(`SELECT DISTINCT bin_location FROM active_inventory_items
		WHERE TRIM(COALESCE(bin_location,'')) <> '' ORDER BY bin_location LIMIT 500`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var b string
			if rows.Scan(&b) == nil {
				bins = append(bins, b)
			}
		}
	}
	pages.NewStocktakePage(lookupOptions("category", ""), bins, user).Render(r.Context(), w)
}

// CreateStocktake 建立盤點單並快照範圍內的帳面庫存。
func CreateStocktake(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	scopeType := strings.TrimSpace(r.FormValue("scope_type"))
	scopeValue := strings.TrimSpace(r.FormValue("scope_value"))
	note := strings.TrimSpace(r.FormValue("note"))
	switch scopeType {
	case "all":
		scopeValue = ""
	case "category", "bin":
		if scopeValue == "" {
			fail(w, "請選擇範圍值")
			return
		}
	default:
		fail(w, "範圍格式錯誤")
		return
	}

	code := "ST-" + time.Now().Format("20060102-150405")
	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO stocktakes (code, scope_type, scope_value, note, created_by) VALUES (?,?,?,?,?)`,
		code, scopeType, scopeValue, note, user.ID)
	if err != nil {
		log.Printf("建立盤點單失敗: %v", err)
		fail(w, "建立盤點單失敗")
		return
	}
	stID, _ := res.LastInsertId()

	insertItems := `INSERT INTO stocktake_items (stocktake_id, inventory_item_id, part_number, name, bin_location, system_qty)
		SELECT ?, id, part_number, name, COALESCE(bin_location,''), COALESCE(stock_quantity,0)
		FROM active_inventory_items`
	var args []interface{}
	args = append(args, stID)
	switch scopeType {
	case "category":
		insertItems += ` WHERE category = ?`
		args = append(args, scopeValue)
	case "bin":
		insertItems += ` WHERE bin_location = ?`
		args = append(args, scopeValue)
	}
	ires, err := tx.Exec(insertItems, args...)
	if err != nil {
		log.Printf("快照盤點明細失敗: %v", err)
		fail(w, "建立盤點明細失敗")
		return
	}
	n, _ := ires.RowsAffected()
	if n == 0 {
		fail(w, "範圍內沒有零件，請改選範圍")
		return
	}
	if _, err := tx.Exec(`UPDATE stocktakes SET item_count = ? WHERE id = ?`, n, stID); err != nil {
		fail(w, "更新盤點筆數失敗")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}
	redirectMsg(w, r, "/stocktakes/"+strconv.FormatInt(stID, 10), "盤點單已建立")
}

// ShowStocktake 盤點單（計數表）。HX-Request 時只回列片段（即時搜尋用）。
func ShowStocktake(w http.ResponseWriter, r *http.Request, user model.User) {
	stID := mustInt(r.PathValue("id"))
	st, err := loadStocktake(stID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := loadStocktakeItems(stID, q)
	if err != nil {
		log.Printf("讀取盤點明細失敗: %v", err)
		fail(w, "讀取盤點明細失敗")
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		pages.StocktakeRows(st.ID, items, st.Status == "counting").Render(r.Context(), w)
		return
	}
	pages.StocktakePage(st, items, flashText(r), user).Render(r.Context(), w)
}

// UpdateStocktakeItem 儲存單筆實盤量（HTMX 列更新＋進度 OOB）。
func UpdateStocktakeItem(w http.ResponseWriter, r *http.Request, user model.User) {
	stID := mustInt(r.PathValue("id"))
	itemID := mustInt(r.PathValue("item_id"))
	st, err := loadStocktake(stID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if st.Status != "counting" {
		fail(w, "盤點單已完成或取消，無法修改")
		return
	}
	raw := strings.TrimSpace(r.FormValue("counted_qty"))
	var counted interface{}
	if raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			fail(w, "實盤量必須是 0 以上的整數")
			return
		}
		counted = n
	}
	res, err := db.DB.Exec(`UPDATE stocktake_items SET counted_qty = ?, counted_by = ?, counted_at = datetime('now')
		WHERE id = ? AND stocktake_id = ?`, counted, user.ID, itemID, stID)
	if err != nil {
		log.Printf("更新實盤量失敗: %v", err)
		fail(w, "更新實盤量失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	items, err := loadStocktakeItems(stID, "")
	if err != nil {
		fail(w, "讀取盤點明細失敗")
		return
	}
	var updated model.StocktakeItem
	for _, it := range items {
		if it.ID == itemID {
			updated = it
			break
		}
	}
	st, _ = loadStocktake(stID)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	pages.StocktakeRow(st.ID, updated, true).Render(r.Context(), w)
	pages.StocktakeProgress(st).Render(r.Context(), w)
}

// CompleteStocktake 完成盤點：以實盤量校正庫存（單筆交易），寫異動與稽核。
func CompleteStocktake(w http.ResponseWriter, r *http.Request) {
	user, _ := currentUser(r)
	stID := mustInt(r.PathValue("id"))
	st, err := loadStocktake(stID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if st.Status != "counting" {
		fail(w, "盤點單已完成或取消")
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT inventory_item_id, counted_qty FROM stocktake_items
		WHERE stocktake_id = ? AND counted_qty IS NOT NULL`, stID)
	if err != nil {
		fail(w, "讀取盤點明細失敗")
		return
	}
	type countRow struct {
		itemID  int64
		counted int
	}
	var toApply []countRow
	for rows.Next() {
		var cr countRow
		if err := rows.Scan(&cr.itemID, &cr.counted); err != nil {
			rows.Close()
			fail(w, "讀取盤點明細失敗")
			return
		}
		toApply = append(toApply, cr)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		fail(w, "讀取盤點明細失敗")
		return
	}

	svc := service.NewInventoryService(db.DB)
	adjusted, surplus, shortage := 0, 0, 0
	for _, cr := range toApply {
		changed, err := svc.ApplyStocktakeCountTx(r.Context(), tx, cr.itemID, cr.counted, stID, user.ID)
		if err != nil {
			log.Printf("盤點調整失敗（零件 #%d）: %v", cr.itemID, err)
			fail(w, fmt.Sprintf("盤點調整失敗（零件 #%d）：%v", cr.itemID, err))
			return
		}
		if changed {
			adjusted++
			var before int
			if err := tx.QueryRow(`SELECT before_quantity FROM inventory_movements
				WHERE inventory_item_id = ? AND source = 'stocktake' ORDER BY id DESC LIMIT 1`, cr.itemID).Scan(&before); err == nil {
				if cr.counted > before {
					surplus++
				} else {
					shortage++
				}
			}
		}
	}

	if _, err := tx.Exec(`UPDATE stocktakes SET status = 'completed', completed_by = ?, completed_at = datetime('now') WHERE id = ?`,
		user.ID, stID); err != nil {
		fail(w, "更新盤點單失敗")
		return
	}
	if _, err := tx.Exec(`INSERT INTO audit_log (actor_id, action, entity_type, entity_id, detail_json)
		VALUES (?, 'stocktake_complete', 'stocktake', ?, ?)`,
		user.ID, stID, fmt.Sprintf(`{"code":%q,"adjusted":%d,"surplus":%d,"shortage":%d,"counted":%d,"total":%d}`,
			st.Code, adjusted, surplus, shortage, len(toApply), st.ItemCount)); err != nil {
		fail(w, "寫入稽核紀錄失敗")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}
	redirectMsg(w, r, "/stocktakes/"+strconv.FormatInt(stID, 10),
		fmt.Sprintf("盤點完成：調整 %d 筆（盤盈 %d／盤虧 %d）", adjusted, surplus, shortage))
}

// CancelStocktake 取消盤點單（不影響庫存）。
func CancelStocktake(w http.ResponseWriter, r *http.Request) {
	stID := mustInt(r.PathValue("id"))
	res, err := db.DB.Exec(`UPDATE stocktakes SET status = 'cancelled' WHERE id = ? AND status = 'counting'`, stID)
	if err != nil {
		fail(w, "取消盤點單失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/stocktakes", "盤點單已取消")
}
