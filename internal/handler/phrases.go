package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/components"
	"autorepairos/web/pages"
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

	// 為什麼走 renderPage：這個頁面原本是自行拼 `<!DOCTYPE html>...`，完全沒有
	// shell 與 nav，等于「整頁沒有右側導覽」——使用者在其他頁面都有右側選單，
	// 只有這裡會突然失去導覽。全站版面只有 renderPage（字串頁）與
	// components.Layout（templ 頁）兩種來源，頁面就必須走其中之一。
	pages.PhrasesPage(list, user, r.URL.Query().Get("msg")).Render(r.Context(), w)
}

// CreatePhrase 新增技師片語。
func CreatePhrase(w http.ResponseWriter, r *http.Request, user model.User) {
	phrase := strings.TrimSpace(r.FormValue("phrase"))
	if phrase == "" {
		formError(w, r, "新增片語", "/phrases", "片語內容為必填")
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

	pages.PhraseEditPage(p, user, r.URL.Query().Get("msg")).Render(r.Context(), w)
}

// UpdatePhrase 更新片語。
func UpdatePhrase(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	phrase := strings.TrimSpace(r.FormValue("phrase"))
	if phrase == "" {
		formError(w, r, "編輯片語", "/phrases/"+strconv.FormatInt(id, 10)+"/edit", "片語內容為必填")
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
	if r.Header.Get("HX-Request") == "true" {
		fragment := make([]components.PhraseLookupResult, 0, len(results))
		for _, p := range results {
			fragment = append(fragment, components.PhraseLookupResult{
				ID: p.ID, Phrase: p.Phrase, Category: p.Category,
			})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := components.PhraseLookupResults(fragment).Render(r.Context(), w); err != nil {
			http.Error(w, "渲染片語結果失敗", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func SearchEstimateItemsAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// phrases
	pwhere := ` WHERE active = 1 AND deleted_at IS NULL`
	pargs := []interface{}{}
	if q != "" {
		pwhere += ` AND phrase LIKE ?`
		pargs = append(pargs, "%"+q+"%")
	}
	prows, _ := db.DB.Query(`SELECT id, phrase, category FROM active_technician_phrases`+pwhere+` ORDER BY category, sort_order, phrase LIMIT 20`, pargs...)
	var phrases []components.PhraseLookupResult
	if prows != nil {
		defer prows.Close()
		for prows.Next() {
			var id int64
			var phrase, cat string
			if err := prows.Scan(&id, &phrase, &cat); err == nil {
				phrases = append(phrases, components.PhraseLookupResult{ID: id, Phrase: phrase, Category: cat})
			}
		}
	}
	_ = components.PhraseLookupResults(phrases).Render(r.Context(), w)
	// parts
	like := "%" + q + "%"
	partsRows, _ := db.DB.Query(`SELECT id, name, part_number, COALESCE(bin_location,''), COALESCE(selling_price,0), COALESCE(stock_quantity,0)
		FROM active_inventory_items
		WHERE name LIKE ? OR part_number LIKE ? OR COALESCE(barcode,'') LIKE ?
		ORDER BY name LIMIT 20`, like, like, like)
	var parts []components.PartLookupResult
	if partsRows != nil {
		defer partsRows.Close()
		for partsRows.Next() {
			var id int64
			var name, pn, bin string
			var price float64
			var stock int
			if err := partsRows.Scan(&id, &name, &pn, &bin, &price, &stock); err == nil {
				parts = append(parts, components.PartLookupResult{ID: id, Name: name, PartNumber: pn, BinLocation: bin, Price: money(price), Stock: stock})
			}
		}
	}
	_ = components.PartLookupResults(parts).Render(r.Context(), w)
}

// 只回傳 body 內容（不自造 DOCTYPE/html/head）：版面外壳（含右側導覽）由
// renderPage 統一提供。少了這層，這頁就不會有右側選單，使用者會覺得「只有
// 這一頁沒有導覽」。
func generatePhrasesHTML(phrases []model.TechnicianPhrase) string {
	var b strings.Builder
	b.WriteString(`<div class="card"><h2>技師常用片語庫</h2>`)
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
		b.WriteString(`<td><a class="btn btn-s" href="/phrases/` + strconv.FormatInt(p.ID, 10) + `/edit">編輯</a> `)
		b.WriteString(`<a class="btn btn-d" href="/phrases/` + strconv.FormatInt(p.ID, 10) + `/delete" hx-post="/phrases/` + strconv.FormatInt(p.ID, 10) + `/delete" data-confirm="確定刪除此片語？">刪除</a></td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table></div>`)
	return b.String()
}

// generatePhraseEditForm 產生編輯表單的「頁面內容」HTML（同樣交給 renderPage 包殼）。
func generatePhraseEditForm(p model.TechnicianPhrase) string {
	var b strings.Builder
	b.WriteString(`<div class="card"><h2>編輯片語 #` + strconv.FormatInt(p.ID, 10) + `</h2>`)
	b.WriteString(`<form method="POST" action="/phrases/` + strconv.FormatInt(p.ID, 10) + `/update" class="frm">`)
	b.WriteString(`<label>片語內容<input name="phrase" value="` + htmlEsc(p.Phrase) + `" required></label>`)
	b.WriteString(`<label>分類<input name="category" value="` + htmlEsc(p.Category) + `"></label>`)
	b.WriteString(`<label>排序<input name="sort_order" type="number" value="` + strconv.Itoa(p.SortOrder) + `"></label>`)
	b.WriteString(`<label>啟用<select name="active">`)
	b.WriteString(`<option value="1" ` + chkIf(p.Active == 1) + `>啟用</option>`)
	b.WriteString(`<option value="0" ` + chkIf(p.Active == 0) + `>停用</option>`)
	b.WriteString(`</select></label>`)
	b.WriteString(`<div class="full"><button class="btn btn-p" type="submit">更新</button>`)
	b.WriteString(`<a class="btn btn-s" href="/phrases">取消</a></div>`)
	b.WriteString(`</form></div>`)
	return b.String()
}
