// Package handler 提供 AutoRepairOS 的 HTTP 端點與頁面渲染。
package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// ============================================================
// 保養套餐範本的 CRUD（maintenance_templates + maintenance_template_items）
//
// 原本只有唯讀列表（舊系統遷入的 21 個里程級距），導致車行無法建立
// 自己的標準工項（例如本店「小保養」的固定項目組合），每次開單都要手動加。
//
// 設計要點：
//   - 套餐本體與項目分開存，項目可整批重寫（表單一次送出全部列）。
//     不用「逐項增刪」是因為編輯時最常見的操作是調整順序與單價，
//     整批重寫在 UI 上更直覺，也不會有「漏刪一項」的殘留。
//   - 舊系統 code（A0002 / G000 …）是 UNIQUE 且舊工單可能參照，建立後不可改。
//     要換代碼請新建並停用舊的。
//   - 沒有 active 欄位（舊表沒有），因此「停用」用刪除表達——套餐是範本不是
//     憑證，刪除不影響已開出的工單（工單項目是快照，不參照範本）。
// ============================================================

// NewMaintenanceTemplateForm 新增保養套餐。
func NewMaintenanceTemplateForm(w http.ResponseWriter, r *http.Request, user model.User) {
	pages.NewMaintenanceTemplatePage(user).Render(r.Context(), w)
}

// CreateMaintenanceTemplate 建立保養套餐（只建本體，項目稍後在編輯頁補）。
func CreateMaintenanceTemplate(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.FormValue("code"))
	name := strings.TrimSpace(r.FormValue("name"))
	if code == "" || name == "" {
		fail(w, "代碼與名稱為必填")
		return
	}

	res, err := db.DB.Exec(`INSERT INTO maintenance_templates (code, name, notes) VALUES (?,?,?)`,
		code, name, nullStr(r.FormValue("notes")))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			fail(w, "代碼「"+code+"」已存在，請用其他代碼")
			return
		}
		fail(w, "建立保養套餐失敗")
		return
	}
	id, _ := res.LastInsertId()
	// 直接導向編輯頁，讓使用者立刻補項目——否則會建出一個空套餐卻無從下手。
	redirectMsg(w, r, "/maintenance-templates/"+strconv.FormatInt(id, 10)+"/edit",
		"套餐已建立，請加入標準項目")
}

// EditMaintenanceTemplateForm 編輯套餐與項目（整批重寫項目）。
func EditMaintenanceTemplateForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	var code, name, notes string
	if err := db.DB.QueryRow(`SELECT code, name, COALESCE(notes,'') FROM active_maintenance_templates WHERE id = ?`, id).
		Scan(&code, &name, &notes); err != nil {
		http.NotFound(w, r)
		return
	}

	rows, err := db.DB.Query(`SELECT i.sequence_no, COALESCE(i.part_number,''),
		COALESCE(inv.name, i.description, ''), i.quantity, i.unit_price
		FROM active_maintenance_template_items i
		LEFT JOIN active_inventory_items inv ON inv.part_number = i.part_number
		WHERE i.template_id = ? ORDER BY i.sequence_no, i.id`, id)
	if err != nil {
		fail(w, "查詢套餐項目失敗")
		return
	}
	defer rows.Close()

	var itemRows []pages.MaintenanceTemplateEditItem
	n := 0
	for rows.Next() {
		var seq int
		var part, desc string
		var qty, price float64
		if rows.Scan(&seq, &part, &desc, &qty, &price) != nil {
			continue
		}
		n++
		itemRows = append(itemRows, pages.MaintenanceTemplateEditItem{
			PartNumber: part, Description: desc, Quantity: trimQty(qty), Price: money(price),
		})
	}
	pages.EditMaintenanceTemplatePage(pages.MaintenanceTemplateEditData{
		ID: id, Code: code, Name: name, Notes: notes, Items: itemRows, User: user,
	}).Render(r.Context(), w)
}

// UpdateMaintenanceTemplate 更新套餐名稱與備註（code 不可改）。
func UpdateMaintenanceTemplate(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		fail(w, "套餐名稱為必填")
		return
	}
	res, err := db.DB.Exec(`UPDATE maintenance_templates SET name=?, notes=? WHERE id=?`,
		name, nullStr(r.FormValue("notes")), id)
	if err != nil {
		fail(w, "更新失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/maintenance-templates/"+strconv.FormatInt(id, 10)+"/edit", "套餐已更新")
}

// ReplaceMaintenanceTemplateItems 整批重寫套餐項目。
//
// 為什麼整批取代而不是逐項增刪：編輯時最常見的操作是調整順序與單價，
// 整批重寫在 UI 上最直覺，也不會有「改到一半漏刪一項」的殘留。
// 空白列（名稱為空）直接忽略，讓使用者可以留著空列稍後再填。
func ReplaceMaintenanceTemplateItems(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	if countRows("SELECT COUNT(1) FROM active_maintenance_templates WHERE id = ?", id) == 0 {
		http.NotFound(w, r)
		return
	}

	// 必須先 ParseForm：r.Form 在呼叫 ParseForm/ParseMultipartForm 之前是空的。
	// 用 r.FormValue() 會隱含解析，但直接取 r.Form[...] 不會——
	// 少了這行會拿到 0 個欄位，所有項目被靜默丟棄（回傳「已儲存（0 項）」）。
	if err := r.ParseForm(); err != nil {
		fail(w, "表單解析失敗")
		return
	}

	parts := r.Form["part_number"]
	descs := r.Form["item_desc"]
	qtys := r.Form["item_qty"]
	prices := r.Form["item_price"]

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	// 先清空再重填。整批在同一個 transaction 內，失敗會整個回滾，
	// 不會出現「刪了舊的但新的沒寫入」的半套狀態。
	if _, err := tx.Exec(`UPDATE maintenance_template_items SET deleted_at = datetime('now')
		WHERE template_id = ? AND deleted_at IS NULL`, id); err != nil {
		fail(w, "重設項目失敗")
		return
	}

	seq := 0
	for i, desc := range descs {
		desc = strings.TrimSpace(desc)
		if desc == "" {
			continue
		}
		seq++
		part := ""
		if i < len(parts) {
			part = strings.TrimSpace(parts[i])
		}
		qty := 1.0
		if i < len(qtys) {
			if v := atofDefault(qtys[i], 1); v > 0 {
				qty = v
			}
		}
		price := 0.0
		if i < len(prices) {
			price = atofDefault(prices[i], 0)
		}
		if _, err := tx.Exec(`INSERT INTO maintenance_template_items
			(template_id, sequence_no, part_number, description, quantity, unit_price)
			VALUES (?,?,?,?,?,?)`, id, seq, nullStr(part), desc, qty, price); err != nil {
			fail(w, "寫入項目失敗")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}
	redirectMsg(w, r, "/maintenance-templates/"+strconv.FormatInt(id, 10)+"/edit",
		"標準項目已儲存（"+strconv.Itoa(seq)+" 項）")
}

// DeleteMaintenanceTemplate 刪除套餐（連同項目）。
// 套餐是範本不是憑證，刪除不影響已開出的工單——工單項目是快照，不參照範本。
func DeleteMaintenanceTemplate(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE maintenance_template_items SET deleted_at = datetime('now')
		WHERE template_id = ? AND deleted_at IS NULL`, id); err != nil {
		fail(w, "刪除項目失敗")
		return
	}
	res, err := tx.Exec(`UPDATE maintenance_templates SET deleted_at = datetime('now')
		WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		fail(w, "刪除套餐失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}
	redirectMsg(w, r, "/maintenance-templates", "保養套餐已刪除")
}

var _ = model.MaintenanceTemplate{}
