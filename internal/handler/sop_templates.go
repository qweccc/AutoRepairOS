package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// sop_templates.go — SOP template CRUD.

// ListSOPTemplates 列出所有啟用的 SOP 模板。
func ListSOPTemplates(w http.ResponseWriter, r *http.Request, user model.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where := ""
	var args []interface{}
	if q != "" {
		where = " WHERE t.name LIKE ? OR t.repair_type LIKE ? "
		like := "%" + q + "%"
		args = append(args, like, like)
	}

	rows, err := db.DB.Query(`SELECT t.id, t.name, t.repair_type,
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
		formError(w, r, "編輯 SOP 模板", "/sop-templates/"+strconv.FormatInt(id, 10)+"/edit", "模板名稱為必填")
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
		formError(w, r, "編輯 SOP 模板", "/sop-templates/"+strconv.FormatInt(id, 10)+"/edit", "表單解析失敗")
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
		formError(w, r, "新增 SOP 模板", "/sop-templates/new", "模板名稱為必填")
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
		formError(w, r, "新增檢查項目", "/sop-templates/"+strconv.FormatInt(templateID, 10)+"/edit", "無效的模板 ID")
		return
	}
	desc := strings.TrimSpace(r.FormValue("description"))
	if desc == "" {
		formError(w, r, "新增檢查項目", "/sop-templates/"+strconv.FormatInt(templateID, 10)+"/edit", "檢查項目描述為必填")
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
