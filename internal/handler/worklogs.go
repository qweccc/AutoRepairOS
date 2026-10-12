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
	ID               int64
	WorkOrderID      int64
	TechnicianID     int64
	WorkDate         string
	Hours            float64
	HourlyRate       float64
	LaborAmount      float64
	CommissionRate   float64
	CommissionAmount float64
	Notes            string
	TechnicianName   string
	PlateNumber      string
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

	// 取工單狀態、總額、保險件別、建立日
	var woStatus string
	var woTotal float64
	var woIsInsurance int
	var woCreatedAt string
	db.DB.QueryRow(`SELECT status, COALESCE(total_amount,0), COALESCE(is_insurance,0), COALESCE(created_at,'') FROM work_orders WHERE id = ?`, woID).Scan(&woStatus, &woTotal, &woIsInsurance, &woCreatedAt)
	// 工時表單日期預設成工單建立日（補舊工單時不用手改）
	woDateDefault := today()
	if len(woCreatedAt) >= 10 {
		woDateDefault = woCreatedAt[:10]
	}

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
			`</td><td><a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(woID, 10) +
			`/worklogs/` + strconv.FormatInt(lg.ID, 10) + `/edit">編輯</a> ` +
			`<a class="btn btn-d" href="/work-orders/` + strconv.FormatInt(woID, 10) +
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

	// 工單狀態標籤
	statusLabel := workOrderStatusLabel[woStatus]
	if statusLabel == "" {
		statusLabel = woStatus
	}
	insuranceLabel := "自費件"
	if woIsInsurance == 1 {
		insuranceLabel = "保險件"
	}

	body := flash(r) + `
    <div class="card">
      <h2>技師工時與佣金</h2>
      <table>
        <tr><th style="width:130px">工單狀態</th><td><span class="pill ` + statusPill[woStatus] + `">` + statusLabel + `</span></td>
            <th style="width:130px">保險件別</th><td>` + insuranceLabel + `</td></tr>
        <tr><th style="width:130px">工單總額</th><td class="num">` + money(woTotal) + `</td>
            <th style="width:130px">總工時</th><td class="num">` + trimNum(totalHours) + ` 小時</td></tr>
        <tr><th>總工資</th><td class="num">` + money(totalLabor) + `</td>
            <th>總佣金</th><td class="num" style="font-size:20px;font-weight:700;color:#38bdf8">` + money(totalCommission) + `</td></tr>
        <tr><th>紀錄筆數</th><td class="num">` + strconv.Itoa(len(list)) + `</td>
            <th></th><td></td></tr>
      </table>
    </div>

    <div class="card">
      <h2>工時明細</h2>` + tbl + `
      <form class="frm" method="POST" action="/work-orders/` + strconv.FormatInt(woID, 10) + `/worklogs" style="margin-top:18px">
        <label>技師<select name="technician_id" required><option value="0">-</option>` + technicianOptions() + `</select></label>
        <label>工作日期<input name="work_date" type="date" value="` + woDateDefault + `" required></label>
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
// actorID 是目前登入者。櫃檯與管理員可替任一技師登錄工時（以表單為準），
// 技師則只能替自己記（忽略表單值），避免代登或冒用。
func AddTechnicianWorklog(w http.ResponseWriter, r *http.Request, actor Actor) {
	woID := mustInt(r.PathValue("id"))

	techID := mustInt(r.FormValue("technician_id"))
	if actor.CanPickTechnician() {
		if techID <= 0 {
			fail(w, "請選擇技師")
			return
		}
	} else {
		// 技師只能替自己記，忽略表單送來的 technician_id。
		techID = actor.ID
	}

	if countRows("SELECT COUNT(1) FROM work_orders WHERE id = ?", woID) == 0 {
		http.NotFound(w, r)
		return
	}

	// 必須是 technician 角色：否則可以把佣金記到 owner/counter 名下。
	if countRows("SELECT COUNT(1) FROM users WHERE id = ? AND role = 'technician'", techID) == 0 {
		fail(w, "指定的使用者不是技師角色")
		return
	}

	// 工時是工單的內容之一，已結帳後不可再補登。
	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
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

// EditTechnicianWorklogForm 編輯工時紀錄頁。
func EditTechnicianWorklogForm(w http.ResponseWriter, r *http.Request, user model.User) {
	woID := mustInt(r.PathValue("id"))
	logID := mustInt(r.PathValue("log_id"))

	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
		return
	}

	var lg techWorklog
	var techName string
	err := db.DB.QueryRow(`SELECT `+techWorklogCols+`, u.name`+techWorklogFrom+
		`WHERE wl.id = ? AND wl.work_order_id = ?`, logID, woID).Scan(
		&lg.ID, &lg.WorkOrderID, &lg.TechnicianID, &lg.WorkDate,
		&lg.Hours, &lg.HourlyRate, &lg.LaborAmount,
		&lg.CommissionRate, &lg.CommissionAmount, &lg.Notes, &techName)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	body := `
    <div class="card">
      <h2>編輯工時紀錄</h2>
      <form class="frm" method="POST" action="/work-orders/` + strconv.FormatInt(woID, 10) + `/worklogs/` + strconv.FormatInt(logID, 10) + `">
        <label>技師<input value="` + htmlEsc(techName) + `" disabled></label>
        <label>工作日期<input name="work_date" type="date" value="` + htmlEsc(lg.WorkDate) + `" required></label>
        <label>工時（小時）<input name="hours" type="number" step="0.5" min="0" value="` + trimNum(lg.Hours) + `" required></label>
        <label>時薪<input name="hourly_rate" type="number" step="1" min="0" value="` + trimNum(lg.HourlyRate) + `" required></label>
        <label>佣金率（%）<input name="commission_rate" type="number" step="0.1" min="0" value="` + trimNum(lg.CommissionRate) + `" required></label>
        <label>備註<input name="notes" value="` + htmlEsc(lg.Notes) + `"></label>
        <div class="full">
          <button class="btn btn-p" type="submit">儲存</button>
          <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(woID, 10) + `/worklogs">取消</a>
        </div>
      </form>
    </div>`

	renderPage(w, "編輯工時", "/work-orders", user.Username, user.Role, body)
}

// UpdateTechnicianWorklog 更新工時紀錄，重算工資與佣金。
func UpdateTechnicianWorklog(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	logID := mustInt(r.PathValue("log_id"))

	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
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
	commission := labor * commRate / 100

	res, err := db.DB.Exec(`UPDATE technician_worklogs SET
		work_date = ?, hours = ?, hourly_rate = ?, labor_amount = ?,
		commission_rate = ?, commission_amount = ?, notes = ?
		WHERE id = ? AND work_order_id = ?`,
		workDate, hours, hourlyRate, labor, commRate, commission,
		nullStr(r.FormValue("notes")), logID, woID)
	if err != nil {
		fail(w, "更新工時紀錄失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10)+"/worklogs", "已更新工時紀錄")
}

// DeleteTechnicianWorklog 刪除單筆工時紀錄。
func DeleteTechnicianWorklog(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	logID := mustInt(r.PathValue("log_id"))

	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
		return
	}

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
	// 佣金是敏感財務資料，技師不看全店總表（只看自己單張工單的工時頁）。
	if !canSeeMoney(user) {
		http.Error(w, "權限不足：佣金統計僅限櫃檯人員與系統管理員使用", http.StatusForbidden)
		return
	}

	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = today()[:7]
	}

	// 只列真正在做事的人。
	//
	// 排除 staff_kind='dependent'（健保眷屬）：眷屬沒有在修車，出現在佣金表
	// 會讓財務數字失真（一排 0 佣金的人名），也會讓人誤以為要付他們錢。
	// 眷屬要看請到「員工/健保」頁。
	rows, err := db.DB.Query(`SELECT u.id, COALESCE(u.name,'-'), u.username,
		COALESCE(u.staff_branch,''),
		COALESCE(SUM(wl.hours),0), COALESCE(SUM(wl.labor_amount),0),
		COALESCE(SUM(wl.commission_amount),0), COUNT(wl.id)
		FROM users u
		LEFT JOIN technician_worklogs wl
		  ON wl.technician_id = u.id AND substr(wl.work_date,1,7) = ?
		WHERE u.role = 'technician'
		  AND COALESCE(u.staff_kind,'employee') = 'employee'
		GROUP BY u.id, u.name, u.username
		ORDER BY COALESCE(SUM(wl.commission_amount),0) DESC, u.staff_branch, u.name`, month)
	if err != nil {
		fail(w, "查詢佣金統計失敗")
		return
	}
	defer rows.Close()

	var b strings.Builder
	b.WriteString(`<table><tr><th>技師</th><th>帳號</th><th>部門</th><th class="num">工單數</th><th class="num">總工時</th><th class="num">總工資</th><th class="num">總佣金</th></tr>`)

	var totalOrders, totalHours, totalLabor, totalCommission float64
	n := 0
	for rows.Next() {
		var id int64
		var name, username, branch string
		var hours, labor, commission, orders float64
		if err := rows.Scan(&id, &name, &username, &branch, &hours, &labor, &commission, &orders); err != nil {
			continue
		}
		b.WriteString(`<tr><td>` + htmlEsc(name) + `</td><td><code>` + htmlEsc(username) +
			`</code></td><td style="font-size:12.5px;color:#94a3b8">` + orDash(branchLabel(branch)) +
			`</td><td class="num">` + money(orders) + `</td><td class="num">` + trimNum(hours) +
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
