package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// --- 技師工時/佣金 ---

const techWorklogCols = `wl.id, wl.work_order_id, wl.technician_id, wl.work_date,
	COALESCE(wl.hours,0), COALESCE(wl.hourly_rate,0), COALESCE(wl.labor_amount,0),
	COALESCE(wl.commission_rate,0), COALESCE(wl.commission_amount,0),
	COALESCE(wl.notes,'')`

const techWorklogFrom = ` FROM technician_worklogs wl
	JOIN work_orders wo ON wo.id = wl.work_order_id
	JOIN users u ON u.id = wl.technician_id
	JOIN vehicles v ON v.id = wo.vehicle_id `

type techWorklog struct {
	ID              int64
	WorkOrderID     int64
	TechnicianID    int64
	WorkDate        string
	Hours           float64
	HourlyRate      float64
	LaborAmount     float64
	CommissionRate  float64
	CommissionAmount float64
	Notes           string
	TechnicianName  string
	PlateNumber     string
}

func scanTechWorklog(row interface{ Scan(...interface{}) error }) (techWorklog, error) {
	var w techWorklog
	err := row.Scan(&w.ID, &w.WorkOrderID, &w.TechnicianID, &w.WorkDate,
		&w.Hours, &w.HourlyRate, &w.LaborAmount,
		&w.CommissionRate, &w.CommissionAmount, &w.Notes)
	return w, err
}

// ListTechnicianWorklogs 顯示單張工單的工時紀錄，並含技師小計。
func ListTechnicianWorklogs(w http.ResponseWriter, r *http.Request, user model.User) {
	woID := mustInt(r.PathValue("id"))

	rows, err := db.DB.Query(`SELECT `+techWorklogCols+`, u.name, v.plate_number`+techWorklogFrom+
		`WHERE wl.work_order_id = ? ORDER BY wl.work_date DESC, wl.id DESC`, woID)
	if err != nil {
		fail(w, "查詢工時紀錄失敗")
		return
	}
	defer rows.Close()

	var list []techWorklog
	techTotals := make(map[string][2]float64) // name -> {hours, commission}
	var totalHours, totalLabor, totalCommission float64

	for rows.Next() {
		var lg techWorklog
		var techName, plate string
		if err := rows.Scan(&lg.ID, &lg.WorkOrderID, &lg.TechnicianID, &lg.WorkDate,
			&lg.Hours, &lg.HourlyRate, &lg.LaborAmount,
			&lg.CommissionRate, &lg.CommissionAmount, &lg.Notes, &techName, &plate); err != nil {
			continue
		}
		lg.TechnicianName = techName
		lg.PlateNumber = plate
		list = append(list, lg)

		cur := techTotals[techName]
		techTotals[techName] = [2]float64{cur[0] + lg.Hours, cur[1] + lg.CommissionAmount}
		totalHours += lg.Hours
		totalLabor += lg.LaborAmount
		totalCommission += lg.CommissionAmount
	}

	var b strings.Builder
	b.WriteString(`<table><tr><th>日期</th><th>技師</th><th class="num">工時</th><th class="num">時薪</th><th class="num">工資</th><th class="num">佣金率</th><th class="num">佣金</th><th>備註</th><th>操作</th></tr>`)
	for _, lg := range list {
		b.WriteString(`<tr><td>` + htmlEsc(lg.WorkDate) + `</td><td>` + htmlEsc(lg.TechnicianName) +
			`</td><td class="num">` + trimNum(lg.Hours) + `</td><td class="num">` + money(lg.HourlyRate) +
			`</td><td class="num">` + money(lg.LaborAmount) + `</td><td class="num">` + trimNum(lg.CommissionRate) +
			`%</td><td class="num">` + money(lg.CommissionAmount) + `</td><td>` + htmlEsc(orDash(lg.Notes)) +
			`</td><td><a class="btn btn-d" href="/work-orders/` + strconv.FormatInt(woID, 10) +
			`/worklogs/` + strconv.FormatInt(lg.ID, 10) + `/delete" onclick="return confirm('刪除這筆工時紀錄？')">刪除</a></td></tr>`)
	}
	b.WriteString(`</table>`)

	tbl := b.String()
	if len(list) == 0 {
		tbl = `<div class="empty">尚未記錄任何技師工時</div>`
	}

	var st strings.Builder
	st.WriteString(`<table><tr><th>技師</th><th class="num">總工時</th><th class="num">總佣金</th></tr>`)
	for name, t := range techTotals {
		st.WriteString(`<tr><td>` + htmlEsc(name) + `</td><td class="num">` + trimNum(t[0]) +
			`</td><td class="num">` + money(t[1]) + `</td></tr>`)
	}
	st.WriteString(`</table>`)
	techSummary := st.String()
	if len(techTotals) == 0 {
		techSummary = `<div class="empty">尚無技師資料</div>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>技師工時與佣金</h2>
      <table>
        <tr><th style="width:130px">總工時</th><td class="num">` + trimNum(totalHours) + ` 小時</td>
            <th style="width:130px">總工資</th><td class="num">` + money(totalLabor) + `</td></tr>
        <tr><th>總佣金</th><td class="num" style="font-size:20px;font-weight:700;color:#38bdf8">` + money(totalCommission) + `</td>
            <th>紀錄筆數</th><td class="num">` + strconv.Itoa(len(list)) + `</td></tr>
      </table>
    </div>

    <div class="card">
      <h2>工時明細</h2>` + tbl + `
      <form class="frm" method="POST" action="/work-orders/` + strconv.FormatInt(woID, 10) + `/worklogs" style="margin-top:18px">
        <label>技師<select name="technician_id" required><option value="0">-</option>` + technicianOptions() + `</select></label>
        <label>工作日期<input name="work_date" type="date" value="` + today() + `" required></label>
        <label>工時（小時）<input name="hours" type="number" step="0.5" min="0" value="1" required></label>
        <label>時薪<input name="hourly_rate" type="number" step="1" min="0" value="0" required></label>
        <label>佣金率（%）<input name="commission_rate" type="number" step="0.1" min="0" value="0" required></label>
        <label>備註<input name="notes"></label>
        <div class="full"><button class="btn btn-p" type="submit">新增工時紀錄</button>
          <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(woID, 10) + `">返回工單</a></div>
      </form>
    </div>

    <div class="card">
      <h2>各技師統計</h2>` + techSummary + `
    </div>`

	renderPage(w, "技師工時", "/work-orders", user.Username, user.Role, body)
}

// AddTechnicianWorklog 新增工時紀錄，自動算工資與佣金金額。
func AddTechnicianWorklog(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	techID := mustInt(r.FormValue("technician_id"))
	if techID <= 0 {
		fail(w, "請選擇技師")
		return
	}

	if countRows("SELECT COUNT(1) FROM work_orders WHERE id = ?", woID) == 0 {
		http.NotFound(w, r)
		return
	}

	workDate := strings.TrimSpace(r.FormValue("work_date"))
	if _, err := timeParseDate(workDate); err != nil {
		fail(w, "工作日期格式錯誤")
		return
	}

	hours := atofDefault(r.FormValue("hours"), 0)
	if hours <= 0 {
		fail(w, "工時必須大於 0")
		return
	}
	hourlyRate := atofDefault(r.FormValue("hourly_rate"), 0)
	commRate := atofDefault(r.FormValue("commission_rate"), 0)
	labor := hours * hourlyRate

	// 佣金以該工單的工資類項目小計為基準
	commission := labor * commRate / 100

	if _, err := db.DB.Exec(`INSERT INTO technician_worklogs
		(work_order_id, technician_id, work_date, hours, hourly_rate, labor_amount,
		 commission_rate, commission_amount, notes)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		woID, techID, workDate, hours, hourlyRate, labor,
		commRate, commission, nullStr(r.FormValue("notes"))); err != nil {
		fail(w, "新增工時紀錄失敗")
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10)+"/worklogs", "已新增工時紀錄")
}

// DeleteTechnicianWorklog 刪除單筆工時紀錄。
func DeleteTechnicianWorklog(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	logID := mustInt(r.PathValue("log_id"))

	res, err := db.DB.Exec("DELETE FROM technician_worklogs WHERE id = ? AND work_order_id = ?", logID, woID)
	if err != nil {
		fail(w, "刪除工時紀錄失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10)+"/worklogs", "已刪除工時紀錄")
}

// ListTechnicianCommissions 全店技師佣金總覽（依月份篩選）。
func ListTechnicianCommissions(w http.ResponseWriter, r *http.Request, user model.User) {
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = today()[:7]
	}

	rows, err := db.DB.Query(`SELECT u.id, COALESCE(u.name,'-'), u.username,
		COALESCE(SUM(wl.hours),0), COALESCE(SUM(wl.labor_amount),0),
		COALESCE(SUM(wl.commission_amount),0), COUNT(wl.id)
		FROM users u
		LEFT JOIN technician_worklogs wl
		  ON wl.technician_id = u.id AND substr(wl.work_date,1,7) = ?
		WHERE u.role = 'technician'
		GROUP BY u.id, u.name, u.username
		ORDER BY COALESCE(SUM(wl.commission_amount),0) DESC`, month)
	if err != nil {
		fail(w, "查詢佣金統計失敗")
		return
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString(`<table><tr><th>技師</th><th>帳號</th><th class="num">工單數</th><th class="num">總工時</th><th class="num">總工資</th><th class="num">總佣金</th></tr>`)

	var totalOrders, totalHours, totalLabor, totalCommission float64
	n := 0
	for rows.Next() {
		var id int64
		var name, username string
		var hours, labor, commission, orders float64
		if err := rows.Scan(&id, &name, &username, &hours, &labor, &commission, &orders); err != nil {
			continue
		}
		b.WriteString(`<tr><td>` + htmlEsc(name) + `</td><td><code>` + htmlEsc(username) +
			`</code></td><td class="num">` + money(orders) + `</td><td class="num">` + trimNum(hours) +
			`</td><td class="num">` + money(labor) + `</td><td class="num" style="color:#38bdf8;font-weight:700">` +
			money(commission) + `</td></tr>`)
		totalOrders += orders
		totalHours += hours
		totalLabor += labor
		totalCommission += commission
		n++
	}
	b.WriteString(`</table>`)

	tbl := b.String()
	if n == 0 {
		tbl = `<div class="empty">沒有技師資料</div>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>技師佣金統計 — ` + htmlEsc(month) + `</h2>
      <form method="GET" action="/technicians/commissions" style="margin-bottom:14px">
        <label style="font-size:12px;color:#94a3b8">月份</label>
        <input name="month" value="` + htmlEsc(month) + `" placeholder="YYYY-MM"
               style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:130px">
        <button class="btn btn-p" type="submit">查詢</button>
      </form>
      <table>
        <tr><th style="width:130px">總工單數</th><td class="num">` + money(totalOrders) +
            `</td><th style="width:130px">總工時</th><td class="num">` + trimNum(totalHours) + ` 小時</td></tr>
        <tr><th>總工資</th><td class="num">` + money(totalLabor) +
            `</td><th>總佣金</th><td class="num" style="font-size:20px;font-weight:700;color:#38bdf8">` + money(totalCommission) + `</td></tr>
      </table>
    </div>
    <div class="card">
      <h2>各技師明細</h2>` + tbl + `
    </div>`

	renderPage(w, "技師佣金統計", "/technicians/commissions", user.Username, user.Role, body)
}

// trimNum 格式化小數，最多兩位，去掉多餘的 0。
func trimNum(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

func timeParseDate(s string) (string, error) {
	if s == "" {
		return today(), nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", err
	}
	return s, nil
}
