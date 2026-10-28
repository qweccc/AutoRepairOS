package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// --- SOP 模板管理 ---

// ListSOPTemplates 列出所有啟用的 SOP 模板。
func ListSOPTemplates(w http.ResponseWriter, r *http.Request, user model.User) {
	rows, err := db.DB.Query(`SELECT t.id, t.name, t.repair_type, t.active, t.sort_order,
		(SELECT COUNT(1) FROM active_sop_items WHERE template_id = t.id) as item_count
		FROM active_sop_templates t ORDER BY t.sort_order, t.name`)
	if err != nil {
		fail(w, "查詢 SOP 模板失敗")
		return
	}
	defer rows.Close()

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where := ""
	var args []interface{}
	if q != "" {
		where = " WHERE t.name LIKE ? OR t.repair_type LIKE ? "
		like := "%" + q + "%"
		args = append(args, like, like)
	}

	rows, err = db.DB.Query(`SELECT t.id, t.name, t.repair_type,
		(SELECT COUNT(1) FROM active_sop_items WHERE template_id = t.id) as item_count
		FROM active_sop_templates t `+where+` ORDER BY t.sort_order, t.name`, args...)
	if err != nil {
		fail(w, "查詢 SOP 模板失敗")
		return
	}
	defer rows.Close()

	var list []model.SOPTemplate
	for rows.Next() {
		var t model.SOPTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.RepairType, &t.ItemCount); err != nil {
			continue
		}
		list = append(list, t)
	}

	var listRows []pages.SOPTemplateListRow
	for _, t := range list {
		listRows = append(listRows, pages.SOPTemplateListRow{
			ID: t.ID, Name: t.Name, RepairType: t.RepairType, ItemCount: t.ItemCount,
		})
	}
	pages.ListSOPTemplatesPage(pages.SOPTemplateListData{
		Rows: listRows, Query: q, User: user, Flash: r.URL.Query().Get("msg"),
	}).Render(r.Context(), w)
}

// NewSOPTemplateForm 新增 SOP 模板頁面。
func NewSOPTemplateForm(w http.ResponseWriter, r *http.Request, user model.User) {
	pages.NewSOPTemplatePage(user).Render(r.Context(), w)
}

// EditSOPTemplateForm 編輯 SOP 模板頁面。
func EditSOPTemplateForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	var name, repairType string
	if err := db.DB.QueryRow(`SELECT name, COALESCE(repair_type,'') FROM active_sop_templates WHERE id = ?`, id).
		Scan(&name, &repairType); err != nil {
		http.NotFound(w, r)
		return
	}

	rows, err := db.DB.Query(`SELECT id, description, required, sort_order
		FROM active_sop_items WHERE template_id = ? ORDER BY sort_order, id`, id)
	if err != nil {
		fail(w, "查詢檢查項目失敗")
		return
	}
	defer rows.Close()

	var items []pages.SOPTemplateEditItem
	for rows.Next() {
		var it pages.SOPTemplateEditItem
		var reqInt int
		if err := rows.Scan(&it.ID, &it.Description, &reqInt, &it.SortOrder); err != nil {
			continue
		}
		it.Required = reqInt != 0
		items = append(items, it)
	}

	pages.EditSOPTemplatePage(pages.SOPTemplateEditData{
		ID: id, Name: name, RepairType: repairType, Items: items, User: user,
	}).Render(r.Context(), w)
}

// UpdateSOPTemplate 更新 SOP 模板本體（名稱/維修類型）。
func UpdateSOPTemplate(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		fail(w, "模板名稱為必填")
		return
	}
	repairType := strings.TrimSpace(r.FormValue("repair_type"))

	res, err := db.DB.Exec(`UPDATE sop_templates SET name=?, repair_type=? WHERE id=?`,
		name, repairType, id)
	if err != nil {
		fail(w, "更新失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/sop-templates", "模板已更新")
}

// DeleteSOPTemplate 刪除 SOP 模板（連同項目）。
func DeleteSOPTemplate(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	tx.Exec(`UPDATE sop_items SET deleted_at = datetime('now') WHERE template_id = ? AND deleted_at IS NULL`, id)
	tx.Exec(`UPDATE sop_templates SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL`, id)

	if err := tx.Commit(); err != nil {
		fail(w, "刪除失敗")
		return
	}
	redirectMsg(w, r, "/sop-templates", "模板已刪除")
}

// ReplaceSOPItems 整批重寫檢查項目。
func ReplaceSOPItems(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	if countRows("SELECT COUNT(1) FROM active_sop_templates WHERE id = ?", id) == 0 {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		fail(w, "表單解析失敗")
		return
	}

	descs := r.Form["item_desc"]
	reqs := r.Form["required"]
	itemIDs := r.Form["item_id"]

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE sop_items SET deleted_at = datetime('now') WHERE template_id = ? AND deleted_at IS NULL`, id); err != nil {
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
		req := 0
		if i < len(reqs) && reqs[i] == "1" {
			req = 1
		}
		oldID := int64(0)
		if i < len(itemIDs) {
			oldID = mustInt(itemIDs[i])
		}
		if oldID > 0 {
			if _, err := tx.Exec(`UPDATE sop_items SET description=?, required=?, sort_order=?, deleted_at=NULL WHERE id=?`,
				desc, req, seq, oldID); err != nil {
				fail(w, "更新項目失敗")
				return
			}
		} else {
			if _, err := tx.Exec(`INSERT INTO sop_items (template_id, description, required, sort_order) VALUES (?,?,?,?)`,
				id, desc, req, seq); err != nil {
				fail(w, "寫入項目失敗")
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}
	redirectMsg(w, r, "/sop-templates/"+strconv.FormatInt(id, 10)+"/edit",
		"檢查項目已儲存（"+strconv.Itoa(seq)+" 項）")
}

// CreateSOPTemplate 建立新 SOP 模板。
func CreateSOPTemplate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		fail(w, "模板名稱為必填")
		return
	}
	repairType := strings.TrimSpace(r.FormValue("repair_type"))

	if _, err := db.DB.Exec(`INSERT INTO sop_templates (name, repair_type) VALUES (?, ?)`,
		name, repairType); err != nil {
		fail(w, "建立 SOP 模板失敗")
		return
	}

	redirectMsg(w, r, "/sop-templates", "模板已建立")
}

// CreateSOPItem 在模板下新增檢查項目。
func CreateSOPItem(w http.ResponseWriter, r *http.Request) {
	templateID := mustInt(r.PathValue("template_id"))
	if templateID <= 0 {
		fail(w, "無效的模板 ID")
		return
	}
	desc := strings.TrimSpace(r.FormValue("description"))
	if desc == "" {
		fail(w, "檢查項目描述為必填")
		return
	}
	required := 1
	if r.FormValue("required") == "0" {
		required = 0
	}

	res, err := db.DB.Exec(`INSERT INTO sop_items (template_id, description, required,
		sort_order) VALUES (?, ?, ?, COALESCE(
			(SELECT MAX(sort_order) FROM sop_items WHERE template_id = ?), 0
		) + 1)`,
		templateID, desc, required, templateID)
	if err != nil {
		fail(w, "新增檢查項目失敗")
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "id": id})
}

// --- 工單掛 SOP 模板 ---

// AttachSOPTemplate 將 SOP 模板掛到工單，並生成逐項勾選紀錄。
func AttachSOPTemplate(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	templateID := mustInt(r.FormValue("template_id"))

	if woID <= 0 || templateID <= 0 {
		fail(w, "無效的工單或模板 ID")
		return
	}

	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
		return
	}

	// 確認模板存在且啟用
	var tplID int64
	if err := db.DB.QueryRow(`SELECT id FROM active_sop_templates WHERE id = ?`,
		templateID).Scan(&tplID); err != nil {
		fail(w, "找不到該 SOP 模板")
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	// 建立工單-模板關聯（忽略重複）
	if _, err := tx.Exec(`INSERT OR IGNORE INTO work_order_sop_templates
		(work_order_id, template_id) VALUES (?, ?)`, woID, templateID); err != nil {
		fail(w, "關聯模板失敗")
		return
	}

	// 複製模板項目到工單勾選紀錄（快照，忽略已存在的項目）
	if _, err := tx.Exec(`INSERT OR IGNORE INTO work_order_sop_records
		(work_order_id, sop_item_id, template_id, description, required, sort_order)
		SELECT ?, si.id, si.template_id, si.description, si.required, si.sort_order
		FROM active_sop_items si WHERE si.template_id = ?`, woID, templateID); err != nil {
		fail(w, "建立檢查項目紀錄失敗")
		return
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10), "已掛入 SOP 模板")
}

// DetachSOPTemplate 移除工單的 SOP 模板及相關紀錄。
func DetachSOPTemplate(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	templateID := mustInt(r.PathValue("template_id"))

	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
		return
	}

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	tx.Exec(`DELETE FROM work_order_sop_records WHERE work_order_id = ? AND template_id = ?`,
		woID, templateID)
	tx.Exec(`DELETE FROM work_order_sop_templates WHERE work_order_id = ? AND template_id = ?`,
		woID, templateID)

	if err := tx.Commit(); err != nil {
		fail(w, "移除失敗")
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10), "已移除 SOP 模板")
}

// --- 逐項勾選 ---

// ToggleSOPRecord 切換工單 SOP 項目的完成狀態（逐項）。
func ToggleSOPRecord(w http.ResponseWriter, r *http.Request, user model.User) {
	woID := mustInt(r.PathValue("id"))
	recordID := mustInt(r.PathValue("record_id"))

	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
		return
	}

	// 讀取目前狀態
	var completed int
	if err := db.DB.QueryRow(`SELECT completed FROM work_order_sop_records
		WHERE id = ? AND work_order_id = ?`, recordID, woID).Scan(&completed); err != nil {
		http.NotFound(w, r)
		return
	}

	if completed == 0 {
		// 標記完成：記錄完成者與時間
		db.DB.Exec(`UPDATE work_order_sop_records
			SET completed = 1, completed_by = ?, completed_at = datetime('now','localtime')
			WHERE id = ? AND work_order_id = ?`, user.ID, recordID, woID)
	} else {
		// 取消完成
		db.DB.Exec(`UPDATE work_order_sop_records
			SET completed = 0, completed_by = NULL, completed_at = NULL
			WHERE id = ? AND work_order_id = ?`, recordID, woID)
	}

	// 更新工單的 sop_completed 欄位（所有必填項目都完成 → 1）
	updateSOPCompleted(woID)

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10), "SOP 項目已更新")
}

// updateSOPCompleted 更新工單的 sop_completed 欄位。
// 邏輯：若工單有 SOP 紀錄，且所有必填項目都已完成 → sop_completed=1，否則=0。
// 若工單沒有任何 SOP 紀錄（未掛模板），維持原值不動。
func updateSOPCompleted(woID int64) {
	var totalRequired, completedRequired int
	err := db.DB.QueryRow(`SELECT
		COUNT(1),
		SUM(CASE WHEN completed = 1 THEN 1 ELSE 0 END)
		FROM work_order_sop_records
		WHERE work_order_id = ? AND required = 1`, woID).Scan(&totalRequired, &completedRequired)
	if err != nil {
		return
	}
	if totalRequired == 0 {
		// 沒有必填項目（可能只有選填或未掛模板），不強制 sop_completed
		return
	}
	sopDone := 0
	if completedRequired >= totalRequired {
		sopDone = 1
	}
	db.DB.Exec(`UPDATE work_orders SET sop_completed = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`, sopDone, woID)
}

// LoadSOPRecords 載入工單的 SOP 勾選紀錄。
func LoadSOPRecords(woID int64) []model.SOPRecord {
	rows, err := db.DB.Query(`SELECT r.id, r.work_order_id, r.sop_item_id, r.template_id,
		r.description, r.required, r.completed,
		COALESCE(r.completed_by, 0), COALESCE(u.name, ''),
		COALESCE(r.completed_at, ''), r.sort_order
		FROM work_order_sop_records r
		LEFT JOIN users u ON u.id = r.completed_by
		WHERE r.work_order_id = ?
		ORDER BY r.template_id, r.sort_order`, woID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var list []model.SOPRecord
	for rows.Next() {
		var rec model.SOPRecord
		var reqInt, compInt int
		if err := rows.Scan(&rec.ID, &rec.WorkOrderID, &rec.SOPItemID, &rec.TemplateID,
			&rec.Description, &reqInt, &compInt,
			&rec.CompletedBy, &rec.CompletedByName,
			&rec.CompletedAtText, &rec.SortOrder); err != nil {
			continue
		}
		rec.Required = reqInt != 0
		rec.Completed = compInt != 0
		list = append(list, rec)
	}
	return list
}

// LoadAttachedTemplates 載入工單已掛入的 SOP 模板 ID。
func LoadAttachedTemplates(woID int64) []int64 {
	rows, err := db.DB.Query(`SELECT template_id FROM work_order_sop_templates
		WHERE work_order_id = ? ORDER BY template_id`, woID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// SOPTemplateOptions 可用 SOP 模板下拉選項 HTML。
func SOPTemplateOptions(excludeIDs []int64) string {
	rows, err := db.DB.Query(`SELECT id, name, repair_type FROM active_sop_templates
		ORDER BY sort_order, name`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	exclude := make(map[int64]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		exclude[id] = true
	}

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name, rtype string
		if err := rows.Scan(&id, &name, &rtype); err != nil {
			continue
		}
		if exclude[id] {
			continue
		}
		label := name
		if rtype != "" {
			label += " (" + rtype + ")"
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(label) + `</option>`)
	}
	return b.String()
}

// CheckSOPCompletionForStatus 檢查工單的所有必填 SOP 項目是否已完成。
// 回傳 nil 表示可完工（或無 SOP 限制）；回傳 error 表示不可完工。
func CheckSOPCompletionForStatus(woID int64, targetStatus string) error {
	// 只在轉「completed」或「paid」時檢查
	if targetStatus != "completed" && targetStatus != "paid" {
		return nil
	}

	// 查詢未完成的必填項目數
	var incomplete int
	err := db.DB.QueryRow(`SELECT COUNT(1) FROM work_order_sop_records
		WHERE work_order_id = ? AND required = 1 AND completed = 0`, woID).Scan(&incomplete)
	if err != nil {
		return nil // 查詢失敗不擋，避免影響沒用 SOP 的舊工單
	}

	if incomplete > 0 {
		return &SOPIncompleteError{Count: incomplete}
	}
	return nil
}

// SOPIncompleteError 未完成 SOP 必填項目錯誤。
type SOPIncompleteError struct {
	Count int
}

func (e *SOPIncompleteError) Error() string {
	return "尚有 " + strconv.Itoa(e.Count) + " 項必填 SOP 項目未完成，無法完工"
}

// SOPAllRequiredComplete 回傳工單所有必填 SOP 是否已完成（供模板用）。
func SOPAllRequiredComplete(woID int64) bool {
	var incomplete int
	err := db.DB.QueryRow(`SELECT COUNT(1) FROM work_order_sop_records
		WHERE work_order_id = ? AND required = 1 AND completed = 0`, woID).Scan(&incomplete)
	if err != nil {
		return true // 查不到就不擋
	}
	return incomplete == 0
}
