package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ListPhrases 列出所有啟用的技師片語。
func ListPhrases(w http.ResponseWriter, r *http.Request, user model.User) {
	// 片語庫對所有登入用戶開放（技師、櫃檯、老闆都需要）
	rows, err := db.DB.Query(`SELECT id, phrase, category, sort_order, active, created_at
		FROM active_technician_phrases ORDER BY category, sort_order, phrase`)
	if err != nil {
		fail(w, "查詢片語失敗")
		return
	}
	defer rows.Close()

	var list []model.TechnicianPhrase
	for rows.Next() {
		var p model.TechnicianPhrase
		if err := rows.Scan(&p.ID, &p.Phrase, &p.Category, &p.SortOrder, &p.Active, &p.CreatedAt); err != nil {
			continue
		}
		list = append(list, p)
	}

	// 簡單的字串頁面，不需要 Templ
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(generatePhrasesHTML(list)))
}

// CreatePhrase 新增技師片語。
func CreatePhrase(w http.ResponseWriter, r *http.Request, user model.User) {
	phrase := strings.TrimSpace(r.FormValue("phrase"))
	if phrase == "" {
		fail(w, "片語內容為必填")
		return
	}
	category := strings.TrimSpace(r.FormValue("category"))
	sortOrder := atoiDefault(r.FormValue("sort_order"), 0)

	res, err := db.DB.Exec(`INSERT INTO technician_phrases (phrase, category, sort_order)
		VALUES (?, ?, ?)`, phrase, category, sortOrder)
	if err != nil {
		fail(w, "新增片語失敗")
		return
	}
	id, _ := res.LastInsertId()
	redirectMsg(w, r, "/phrases", "片語已新增 (ID: "+strconv.FormatInt(id, 10)+")")
}

// EditPhraseForm 編輯片語表單。
func EditPhraseForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT id, phrase, category, sort_order, active
		FROM active_technician_phrases WHERE id = ?`, id)
	var p model.TechnicianPhrase
	if err := row.Scan(&p.ID, &p.Phrase, &p.Category, &p.SortOrder, &p.Active); err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(generatePhraseEditForm(p)))
}

// UpdatePhrase 更新片語。
func UpdatePhrase(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	phrase := strings.TrimSpace(r.FormValue("phrase"))
	if phrase == "" {
		fail(w, "片語內容為必填")
		return
	}
	category := strings.TrimSpace(r.FormValue("category"))
	sortOrder := atoiDefault(r.FormValue("sort_order"), 0)
	active := 1
	if r.FormValue("active") == "0" {
		active = 0
	}

	_, err := db.DB.Exec(`UPDATE technician_phrases SET phrase=?, category=?, sort_order=?, active=?
		WHERE id=?`, phrase, category, sortOrder, active, id)
	if err != nil {
		fail(w, "更新片語失敗")
		return
	}
	redirectMsg(w, r, "/phrases", "片語已更新")
}

// DeletePhrase 刪除片語（軟刪除）。
func DeletePhrase(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	_, err := db.DB.Exec(`UPDATE technician_phrases SET deleted_at = datetime('now','localtime')
		WHERE id = ? AND deleted_at IS NULL`, id)
	if err != nil {
		fail(w, "刪除片語失敗")
		return
	}
	redirectMsg(w, r, "/phrases", "片語已刪除")
}

// SearchPhrasesAPI 即時搜尋片語 API（供工單項目描述使用）。
func SearchPhrasesAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))

	where := ` WHERE active = 1 AND deleted_at IS NULL`
	args := []interface{}{}
	if q != "" {
		where += ` AND phrase LIKE ?`
		args = append(args, "%"+q+"%")
	}
	if category != "" {
		where += ` AND category = ?`
		args = append(args, category)
	}

	query := `SELECT id, phrase, category FROM active_technician_phrases` + where +
		` ORDER BY category, sort_order, phrase LIMIT 50`

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, []interface{}{})
		return
	}
	defer rows.Close()

	type phraseResult struct {
		ID       int64  `json:"id"`
		Phrase   string `json:"phrase"`
		Category string `json:"category"`
	}
	var results []phraseResult
	for rows.Next() {
		var p phraseResult
		if err := rows.Scan(&p.ID, &p.Phrase, &p.Category); err != nil {
			continue
		}
		results = append(results, p)
	}
	writeJSON(w, http.StatusOK, results)
}

// generatePhrasesHTML 產生片語列表 HTML（簡單頁面，不需 Templ）。
func generatePhrasesHTML(phrases []model.TechnicianPhrase) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="zh-Hant"><head><meta charset="utf-8"><title>技師片語庫</title>`)
	b.WriteString(`<link rel="stylesheet" href="/static/app.css">`)
	b.WriteString(`<style>.phrase-item { padding: 8px 12px; border-bottom: 1px solid #334155; }</style>`)
	b.WriteString(`</head><body><div class="card"><h2>技師常用片語庫</h2>`)
	b.WriteString(`<form method="POST" action="/phrases/new" class="frm">`)
	b.WriteString(`<label>片語內容<input name="phrase" required placeholder="例：煞車皮剩2mm"></label>`)
	b.WriteString(`<label>分類<input name="category" placeholder="例：煞車、引擎"></label>`)
	b.WriteString(`<label>排序<input name="sort_order" type="number" value="0"></label>`)
	b.WriteString(`<div class="full"><button class="btn btn-p" type="submit">新增片語</button></div>`)
	b.WriteString(`</form>`)
	b.WriteString(`<table><tr><th>片語</th><th>分類</th><th>排序</th><th>啟用</th><th>操作</th></tr>`)
	for _, p := range phrases {
		b.WriteString(`<tr class="phrase-item">`)
		b.WriteString(`<td>` + htmlEsc(p.Phrase) + `</td>`)
		b.WriteString(`<td>` + htmlEsc(p.Category) + `</td>`)
		b.WriteString(`<td class="num">` + strconv.Itoa(p.SortOrder) + `</td>`)
		active := "啟用"
		if p.Active == 0 {
			active = "停用"
		}
		b.WriteString(`<td>` + active + `</td>`)
		b.WriteString(`<td><a class="btn btn-s" href="/phrases/` + strconv.FormatInt(p.ID, 10) + `/edit">編輯</a>`)
		b.WriteString(`<a class="btn btn-d" href="/phrases/` + strconv.FormatInt(p.ID, 10) + `/delete" hx-post="/phrases/` + strconv.FormatInt(p.ID, 10) + `/delete" data-confirm="確定刪除此片語？">刪除</a></td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table></div></body></html>`)
	return b.String()
}

// generatePhraseEditForm 產生編輯表單 HTML。
func generatePhraseEditForm(p model.TechnicianPhrase) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="zh-Hant"><head><meta charset="utf-8"><title>編輯片語</title>`)
	b.WriteString(`<link rel="stylesheet" href="/static/app.css">`)
	b.WriteString(`</head><body><div class="card"><h2>編輯片語 #` + strconv.FormatInt(p.ID, 10) + `</h2>`)
	b.WriteString(`<form method="POST" action="/phrases/` + strconv.FormatInt(p.ID, 10) + "/update\" class=\"frm\">")
	b.WriteString(`<label>片語內容<input name="phrase" value="` + htmlEsc(p.Phrase) + `" required></label>`)
	b.WriteString(`<label>分類<input name="category" value="` + htmlEsc(p.Category) + `"></label>`)
	b.WriteString(`<label>排序<input name="sort_order" type="number" value="` + strconv.Itoa(p.SortOrder) + `"></label>`)
	b.WriteString(`<label>啟用<select name="active">`)
	b.WriteString(`<option value="1" ` + chkIf(p.Active == 1) + `>啟用</option>`)
	b.WriteString(`<option value="0" ` + chkIf(p.Active == 0) + `>停用</option>`)
	b.WriteString(`</select></label>`)
	b.WriteString(`<div class="full"><button class="btn btn-p" type="submit">更新</button>`)
	b.WriteString(`<a class="btn btn-s" href="/phrases">取消</a></div>`)
	b.WriteString(`</form></div></body></html>`)
	return b.String()
}
