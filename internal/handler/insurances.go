package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// 車輛投保紀錄與定檢排程。
//
// 這兩塊是車行的固定業務，舊系統各有一張表帶著歷史資料：
//   Car Insurance List  2,667 筆  投保紀錄（險種、發照日、卡號）
//   Car Check List     21,176 筆  定檢排程（排定日、完成日）
// 先前遷移只做營運主流程，這兩張漏掉，因此車主問「我上次保到什麼時候」
// 或「下次驗車何時」時查不到。

const insuranceClassLabel = "保險險種"

// ListVehicleInsurances 投保紀錄列表。
func ListVehicleInsurances(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	company := strings.TrimSpace(qv.Get("company"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ? OR COALESCE(vi.card_no,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if company != "" {
		where += ` AND vi.company_no = ?`
		args = append(args, company)
	}

	from := ` FROM vehicle_insurances vi
		JOIN vehicles v ON v.id = vi.vehicle_id
		JOIN customers c ON c.id = v.customer_id
		LEFT JOIN insurance_companies ic ON ic.id = vi.insurance_company_id`

	total := countRows(`SELECT COUNT(1)`+from+where, args...)

	query := `SELECT vi.id, vi.vehicle_id, v.plate_number, COALESCE(vi.insurance_company_id,0),
		COALESCE(ic.name, vi.company_no, ''), COALESCE(vi.company_no,''),
		COALESCE(vi.insurance_class,''), COALESCE(vi.insurance_class_name,''),
		COALESCE(vi.license_date,''), COALESCE(vi.card_no,''), COALESCE(vi.amount_raw,''),
		COALESCE(vi.notes,''), COALESCE(c.name,'')
		` + from + where + ` ORDER BY vi.license_date DESC, vi.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢投保紀錄失敗")
		return
	}
	defer rows.Close()

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>車牌</th><th>車主</th><th>保險公司</th><th>險種</th>` +
		`<th>發照日</th><th>保險卡號</th><th>保額／備註</th></tr>`)
	n := 0
	for rows.Next() {
		var id, vid, compID int64
		var plate, compName, compNo, cls, clsName, date, card, amount, notes, custName string
		if err := rows.Scan(&id, &vid, &plate, &compID, &compName, &compNo,
			&cls, &clsName, &date, &card, &amount, &notes, &custName); err != nil {
			continue
		}
		n++
		clsCell := orDash(htmlEsc(cls))
		if clsName != "" {
			clsCell = `<span class="pill" style="background:#1e3a5f;color:#bae6fd">` + htmlEsc(clsName) + `</span>`
		}
		tb.WriteString(`<tr><td><a href="/vehicles/` + strconv.FormatInt(vid, 10) + `"><b>` +
			htmlEsc(plate) + `</b></a></td><td style="font-size:12.5px">` + htmlEsc(custName) +
			`</td><td>` + orDash(htmlEsc(compName)) + `</td><td>` + clsCell +
			`</td><td style="font-size:12.5px">` + orDash(normDate(date)) +
			`</td><td><code style="font-size:12px">` + orDash(htmlEsc(card)) +
			`</code></td><td style="font-size:12px;color:#94a3b8">` + orDash(htmlEsc(amount)) + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if n == 0 {
		table = `<div class="empty">沒有符合條件的投保紀錄</div>`
	}

	// 保險公司篩選籤（依實際資料列出，附筆數）
	var compTabs strings.Builder
	compTabs.WriteString(`<a class="btn ` + btnIf(company == "", "btn-p", "btn-s") + `" href="/insurances">全部</a> `)
	crows, cerr := db.DB.Query(`SELECT COALESCE(ic.name, vi.company_no, '(未知)') nm, COUNT(1) n
		FROM vehicle_insurances vi
		LEFT JOIN insurance_companies ic ON ic.id = vi.insurance_company_id
		GROUP BY nm ORDER BY n DESC LIMIT 12`)
	if cerr == nil {
		defer crows.Close()
		for crows.Next() {
			var nm string
			var cnt int
			if crows.Scan(&nm, &cnt) != nil {
				continue
			}
			u := url.Values{}
			u.Set("company", nm)
			compTabs.WriteString(`<a class="btn ` + btnIf(company == nm, "btn-p", "btn-s") + `" href="/insurances?` +
				u.Encode() + `">` + htmlEsc(nm) + ` ` + strconv.Itoa(cnt) + `</a> `)
		}
	}

	body := flash(r) + `
    <div class="card">
      <h2>車輛投保紀錄（符合條件 ` + strconv.Itoa(total) + ` 筆）</h2>
      <p style="color:#94a3b8;font-size:13.5px;margin:0 0 14px">
        舊系統「保險名單」表（Car Insurance List）遷移而來。保險理賠是車行主要收入來源，
        這一頁查「這台車保了什麼、哪家保的、何時發照」。
      </p>
      <div style="margin-bottom:12px;display:flex;gap:6px;flex-wrap:wrap;align-items:center">
        ` + compTabs.String() + `
      </div>
      <form method="GET" action="/insurances" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋車牌 / 車主 / 保險卡號" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:280px">
        ` + hiddenInputs(qv, "q", "page", "per") + `
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/insurances">清除</a>
        <a class="btn btn-s" href="/inspections">定檢排程</a>
      </form>` + table + pager("/insurances", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/insurances", qv, size) + `</div>
    </div>`

	renderPage(w, "車輛投保紀錄", "/insurances", user.Username, user.Role, body)
}

// ListVehicleInspections 定檢排程列表。
func ListVehicleInspections(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	pendingOnly := qv.Get("pending") == "1"
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if pendingOnly {
		where += ` AND COALESCE(vi.check_date,'') = ''`
	}

	from := ` FROM vehicle_inspections vi
		JOIN vehicles v ON v.id = vi.vehicle_id
		JOIN customers c ON c.id = v.customer_id`

	total := countRows(`SELECT COUNT(1)`+from+where, args...)
	pendingCount := countRows(`SELECT COUNT(1) FROM vehicle_inspections WHERE COALESCE(check_date,'')=''`)

	query := `SELECT vi.id, vi.vehicle_id, v.plate_number, COALESCE(vi.order_date,''),
		COALESCE(vi.check_date,''), COALESCE(vi.firm_no,''), COALESCE(vi.notes,''),
		COALESCE(c.name,'')
		` + from + where + ` ORDER BY vi.order_date DESC, vi.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢定檢排程失敗")
		return
	}
	defer rows.Close()

	today := today()
	var tb strings.Builder
	tb.WriteString(`<table><tr><th>車牌</th><th>車主</th><th>排定日</th><th>完成日</th><th>狀態</th></tr>`)
	n := 0
	for rows.Next() {
		var id, vid int64
		var plate, orderDate, checkDate, firmNo, notes, custName string
		if err := rows.Scan(&id, &vid, &plate, &orderDate, &checkDate, &firmNo, &notes, &custName); err != nil {
			continue
		}
		n++
		status := `<span class="pill p-completed">已完成</span>`
		if checkDate == "" {
			// 未完成：排定日已過 = 逾期，否則待辦
			if od := normDate(orderDate); od != "" && od < today {
				status = `<span class="pill p-apprno">已逾期</span>`
			} else {
				status = `<span class="pill p-waitparts">待檢驗</span>`
			}
		}
		tb.WriteString(`<tr><td><a href="/vehicles/` + strconv.FormatInt(vid, 10) + `"><b>` +
			htmlEsc(plate) + `</b></a></td><td style="font-size:12.5px">` + htmlEsc(custName) +
			`</td><td style="font-size:12.5px">` + orDash(normDate(orderDate)) +
			`</td><td style="font-size:12.5px">` + orDash(normDate(checkDate)) +
			`</td><td>` + status + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if n == 0 {
		table = `<div class="empty">沒有符合條件的定檢排程</div>`
	}

	pendingNote := ""
	if pendingOnly {
		pendingNote = ` <a href="/inspections" style="font-size:13px">（顯示全部）</a>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>車輛定檢排程（符合條件 ` + strconv.Itoa(total) + ` 筆，未完成 ` + strconv.Itoa(pendingCount) + ` 筆）` + pendingNote + `</h2>
      <p style="color:#94a3b8;font-size:13.5px;margin:0 0 14px">
        舊系統「驗車名單」表（Car Check List）遷移而來。舊系統只記錄排定日，
        實際完成幾乎沒回填，所以「完成日」為空代表尚未檢驗。
      </p>
      <form method="GET" action="/inspections" style="margin-bottom:12px">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋車牌 / 車主" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:280px">
        ` + hiddenInputs(qv, "q", "page", "per") + `
        <label style="font-size:13px;margin-left:10px"><input type="checkbox" name="pending" value="1"` + chkIf(pendingOnly) + `> 只看未完成</label>
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/inspections">清除</a>
        <a class="btn btn-s" href="/insurances">投保紀錄</a>
      </form>` + table + pager("/inspections", qv, total, page, size) +
		`<div style="margin-top:6px">` + perPageSel("/inspections", qv, size) + `</div>
    </div>`

	renderPage(w, "車輛定檢排程", "/inspections", user.Username, user.Role, body)
}
