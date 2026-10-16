package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// 保養套餐範本。
//
// 舊系統的「保養紀錄」表（Maintain Record Header/Detail）定義了標準保養單：
//   5千公里定期保養、1萬公里定期保養…（21 個里程級距）
//   過戶、理賠（特殊單）
// 開工單時可一鍵套用，不必每次手動加項目。

// ListMaintenanceTemplates 保養套餐列表。
func ListMaintenanceTemplates(w http.ResponseWriter, r *http.Request, user model.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (t.code LIKE ? OR t.name LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}

	rows, err := db.DB.Query(`SELECT t.id, t.code, t.name, COALESCE(t.notes,''),
		(SELECT COUNT(1) FROM maintenance_template_items i WHERE i.template_id = t.id)
		FROM maintenance_templates t`+where+` ORDER BY t.code`, args...)
	if err != nil {
		fail(w, "查詢保養套餐失敗")
		return
	}
	defer rows.Close()

	var list []model.MaintenanceTemplate
	for rows.Next() {
		var t model.MaintenanceTemplate
		if err := rows.Scan(&t.ID, &t.Code, &t.Name, &t.Notes, &t.ItemCount); err != nil {
			continue
		}
		list = append(list, t)
	}

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>代碼</th><th>套餐名稱</th><th class="num">項目數</th><th>操作</th></tr>`)
	for _, t := range list {
		tb.WriteString(`<tr><td><code>` + htmlEsc(t.Code) + `</code></td><td><b>` + htmlEsc(t.Name) +
			`</b></td><td class="num">` + strconv.Itoa(t.ItemCount) +
			`</td><td><a class="btn btn-s" href="/maintenance-templates?id=` +
			strconv.FormatInt(t.ID, 10) + `">查看項目</a> ` +
			`<a class="btn btn-p" href="/maintenance-templates/` +
			strconv.FormatInt(t.ID, 10) + `/edit">編輯</a> ` +
			`<form method="POST" action="/maintenance-templates/` +
			strconv.FormatInt(t.ID, 10) + `/delete" style="display:inline" onsubmit="return confirm('確定刪除套餐「` +
			htmlEsc(t.Code) + `」？已開出的工單不受影響。')"><button class="btn btn-d" type="submit">刪除</button></form></td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有保養套餐</div>`
	}

	// 指定 ?id= 時顯示該套餐的項目明細
	detail := ""
	if id := mustInt(r.URL.Query().Get("id")); id > 0 {
		detail = templateDetail(id)
	}

	body := flash(r) + `
    <div class="card">
      <h2>保養套餐範本（` + strconv.Itoa(len(list)) + ` 個）</h2>
      <p style="color:#94a3b8;font-size:13.5px;margin:0 0 14px">
        舊系統「保養紀錄」表遷移而來。開工單時可依里程級距套用標準項目，
        不必每次手動加。項目內容為舊系統定義，單價多為 0（當時未填）。
      </p>
      <form method="GET" action="/maintenance-templates" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋代碼 / 名稱" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:280px">
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/maintenance-templates">清除</a>
        <a class="btn btn-p" href="/maintenance-templates/new">＋ 新增套餐</a>
      </form>` + table + `
    </div>` + detail

	renderPage(w, "保養套餐", "/maintenance-templates", user.Username, user.Role, body)
}

// templateDetail 某套餐的項目明細（HTML 片段）。
func templateDetail(id int64) string {
	var name, code string
	if err := db.DB.QueryRow("SELECT code, name FROM maintenance_templates WHERE id = ?", id).
		Scan(&code, &name); err != nil {
		return ""
	}

	rows, err := db.DB.Query(`SELECT i.sequence_no, COALESCE(i.part_number,''),
		COALESCE(inv.name, i.description, ''), i.quantity, i.unit_price
		FROM maintenance_template_items i
		LEFT JOIN inventory_items inv ON inv.part_number = i.part_number
		WHERE i.template_id = ? ORDER BY i.sequence_no, i.id`, id)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var tb strings.Builder
	tb.WriteString(`<table><tr><th class="num">項次</th><th>料號</th><th>名稱</th><th class="num">數量</th><th class="num">單價</th></tr>`)
	n := 0
	for rows.Next() {
		var seq int
		var part, desc string
		var qty, price float64
		if err := rows.Scan(&seq, &part, &desc, &qty, &price); err != nil {
			continue
		}
		n++
		tb.WriteString(`<tr><td class="num">` + strconv.Itoa(seq) + `</td><td><code>` + htmlEsc(part) +
			`</code></td><td>` + htmlEsc(desc) + `</td><td class="num">` + money(qty) +
			`</td><td class="num">` + money(price) + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	if n == 0 {
		return `<div class="card"><h2>` + htmlEsc(code) + ` ` + htmlEsc(name) +
			`</h2><div class="empty">此套餐沒有項目</div></div>`
	}
	return `<div class="card"><h2>` + htmlEsc(code) + ` ` + htmlEsc(name) +
		`（` + strconv.Itoa(n) + ` 項）</h2>` + tb.String() + `</div>`
}

// ListExpenses 零用金／雜支支出。
//
// 金額沿用舊系統原值（分），不換算 —— 依使用者指示「先按本來單位」。
// 因此頁面明確標示單位未確認，避免誤讀為元。
func ListExpenses(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (COALESCE(part_number,'') LIKE ? OR COALESCE(notes,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}

	total := countRows(`SELECT COUNT(1) FROM expenses`+where, args...)

	query := `SELECT id, sequence_no, COALESCE(spend_date,''), amount_in, amount_out,
		COALESCE(part_number,''), COALESCE(notes,'')
		FROM expenses` + where + ` ORDER BY spend_date DESC, id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢支出失敗")
		return
	}
	defer rows.Close()

	var sumIn, sumOut float64
	db.DB.QueryRow(`SELECT COALESCE(SUM(amount_in),0), COALESCE(SUM(amount_out),0) FROM expenses`).
		Scan(&sumIn, &sumOut)

	var expRows []pages.ExpenseRow
	for rows.Next() {
		var id int64
		var seq int
		var date, part, notes string
		var amtIn, amtOut float64
		if err := rows.Scan(&id, &seq, &date, &amtIn, &amtOut, &part, &notes); err != nil {
			continue
		}
		expRows = append(expRows, pages.ExpenseRow{
			ID:     id,
			Date:   normDate(date),
			Part:   part,
			Notes:  notes,
			AmtIn:  amtIn,
			AmtOut: amtOut,
		})
	}

	pages.ListExpensesPage(expRows, total, page, size, q, sumIn, sumOut).Render(r.Context(), w)
}

// SearchExpensesAPI 支出即時搜尋 API（HTMX 用）。
func SearchExpensesAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (COALESCE(part_number,'') LIKE ? OR COALESCE(notes,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	rows, err := db.DB.Query(`SELECT id, sequence_no, COALESCE(spend_date,''), amount_in, amount_out,
		COALESCE(part_number,''), COALESCE(notes,'')
		FROM expenses`+where+` ORDER BY spend_date DESC, id DESC LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.ExpenseRow
	for rows.Next() {
		var id int64
		var seq int
		var date, part, notes string
		var amtIn, amtOut float64
		if err := rows.Scan(&id, &seq, &date, &amtIn, &amtOut, &part, &notes); err != nil {
			continue
		}
		result = append(result, pages.ExpenseRow{
			ID:     id,
			Date:   normDate(date),
			Part:   part,
			Notes:  notes,
			AmtIn:  amtIn,
			AmtOut: amtOut,
		})
	}
	pages.ExpenseSearchRows(result).Render(r.Context(), w)
}
