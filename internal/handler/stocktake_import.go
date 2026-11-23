package handler

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ImportStocktakeCSV 把填好「實盤數」的負庫存盤點單 CSV 匯入成盤點草稿。
//
// 流程：负库存页的「下載盤點單 CSV」→ 店内逐架清点填實盤數 → 回頁「上傳」，
// 系统建立一张 scope_type=csv 的盤点单（只含 CSV 列出的料號），已填實盤者
// 直接带 counted_qty，未填者留空待補。之后在 /stocktakes/{id} 核对／完成，
// 與既有 HTMX 逐筆輸入同一套完成校正（source=stocktake）。
func ImportStocktakeCSV(w http.ResponseWriter, r *http.Request, user model.User) {
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		fail(w, "讀取上傳檔案失敗")
		return
	}
	f, _, err := r.FormFile("csv")
	if err != nil {
		fail(w, "請選擇要匯入的 CSV 檔案")
		return
	}
	defer f.Close()

	recs, err := csv.NewReader(f).ReadAll()
	if err != nil || len(recs) < 2 {
		fail(w, "CSV 內容不正確（至少需要表頭與一列資料）")
		return
	}

	idxPart, idxCounted := -1, -1
	for i, name := range recs[0] {
		n := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\ufeff")))
		switch n {
		case "料號", "part_number":
			idxPart = i
		case "實盤數", "counted_qty", "counted":
			idxCounted = i
		}
	}
	if idxPart < 0 {
		fail(w, "CSV 缺少「料號」欄位")
		return
	}

	type row struct {
		part    string
		counted interface{}
	}
	var items []row
	var badRows int
	for _, rec := range recs[1:] {
		if len(rec) == 0 || strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue
		}
		if idxPart >= len(rec) {
			badRows++
			continue
		}
		part := strings.TrimSpace(rec[idxPart])
		if part == "" {
			continue
		}
		var counted interface{}
		if idxCounted >= 0 && idxCounted < len(rec) {
			raw := strings.TrimSpace(rec[idxCounted])
			if raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 0 {
					badRows++
					continue
				}
				counted = n
			}
		}
		items = append(items, row{part: part, counted: counted})
	}
	if len(items) == 0 {
		fail(w, "CSV 沒有可匯入的資料列")
		return
	}

	code := "ST-" + time.Now().Format("20060102-150405")
	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO stocktakes (code, scope_type, scope_value, note, created_by)
		VALUES (?,?,?,?,?)`, code, "csv", "", "CSV 匯入", user.ID)
	if err != nil {
		log.Printf("建立 CSV 盤點單失敗: %v", err)
		fail(w, "建立盤點單失敗")
		return
	}
	stID, _ := res.LastInsertId()

	imported, skipped := 0, 0
	for _, it := range items {
		var invID int64
		var name, binLoc string
		var systemQty int
		err := tx.QueryRow(`SELECT id, name, COALESCE(bin_location,''), COALESCE(stock_quantity,0)
			FROM active_inventory_items WHERE part_number = ? LIMIT 1`, it.part).
			Scan(&invID, &name, &binLoc, &systemQty)
		if err != nil {
			skipped++
			continue
		}
		if _, err := tx.Exec(`INSERT INTO stocktake_items
			(stocktake_id, inventory_item_id, part_number, name, bin_location, system_qty, counted_qty, counted_by)
			VALUES (?,?,?,?,?,?,?,?)`,
			stID, invID, it.part, name, binLoc, systemQty, it.counted, user.ID); err != nil {
			skipped++
			continue
		}
		imported++
	}
	if imported == 0 {
		fail(w, "CSV 內的料號都不存在（或已停用），未建立盤點單")
		return
	}
	if _, err := tx.Exec(`UPDATE stocktakes SET item_count = ? WHERE id = ?`, imported, stID); err != nil {
		fail(w, "更新盤點筆數失敗")
		return
	}
	if _, err := tx.Exec(`INSERT INTO audit_log (actor_id, action, entity_type, entity_id, detail_json)
		VALUES (?, 'stocktake_csv_import', 'stocktake', ?, ?)`, user.ID, stID,
		fmt.Sprintf(`{"code":%q,"imported":%d,"skipped":%d,"bad_rows":%d}`, code, imported, skipped, badRows)); err != nil {
		log.Printf("寫入 CSV 匯入稽核紀錄失敗: %v", err)
		fail(w, "寫入稽核紀錄失敗")
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}
	redirectMsg(w, r, "/stocktakes/"+strconv.FormatInt(stID, 10),
		fmt.Sprintf("已建立盤點草稿：匯入 %d 筆、略過 %d 筆", imported, skipped))
}
