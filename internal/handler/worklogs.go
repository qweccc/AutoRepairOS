package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// --- 技師工時/佣金 ---

const techWorklogCols = `wl.id, wl.work_order_id, wl.technician_id, wl.work_date,
	COALESCE(wl.hours,0), COALESCE(wl.hourly_rate,0), COALESCE(wl.labor_amount,0),
	COALESCE(wl.commission_rate,0), COALESCE(wl.commission_amount,0),
	COALESCE(wl.notes,'')`

const techWorklogFrom = ` FROM active_technician_worklogs wl
	JOIN active_work_orders wo ON wo.id = wl.work_order_id
	JOIN users u ON u.id = wl.technician_id
	JOIN active_vehicles v ON v.id = wo.vehicle_id `

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

func phase5Technicians() []pages.Phase5Option {
	rows, err := db.DB.Query(`SELECT u.id, u.name, u.username, COALESCE(u.staff_branch,'')
		FROM users u WHERE u.role='technician' AND COALESCE(u.staff_kind,'employee')='employee'
		AND UPPER(COALESCE(u.staff_branch,'')) NOT IN ('E','G','H') AND COALESCE(u.active,1)=1
		ORDER BY u.staff_branch, u.name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []pages.Phase5Option
	for rows.Next() {
		var id int64
		var name, username, branch string
		if rows.Scan(&id, &name, &username, &branch) == nil {
			label := name + " (" + username + ")"
			if dept := branchLabel(branch); dept != "" {
				label += "　" + dept
			}
			out = append(out, pages.Phase5Option{Value: strconv.FormatInt(id, 10), Label: label})
		}
	}
	return out
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
	db.DB.QueryRow(`SELECT status, COALESCE(total_amount,0), COALESCE(is_insurance,0), COALESCE(created_at,'') FROM active_work_orders WHERE id = ?`, woID).Scan(&woStatus, &woTotal, &woIsInsurance, &woCreatedAt)
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

	showMoney := canSeeMoney(user)
	moneyFor := func(v float64) string {
		if showMoney {
			return money(v)
		}
		return "—"
	}

	rowsOut := make([]pages.Phase5WorklogRow, 0, len(list))
	for _, lg := range list {
		rowsOut = append(rowsOut, pages.Phase5WorklogRow{Date: lg.WorkDate, Technician: lg.TechnicianName, Hours: trimNum(lg.Hours), Rate: moneyFor(lg.HourlyRate), Labor: moneyFor(lg.LaborAmount), CommissionRate: trimNum(lg.CommissionRate), Commission: moneyFor(lg.CommissionAmount), Notes: orDash(lg.Notes), ID: strconv.FormatInt(lg.ID, 10)})
	}
	summaries := make([]pages.Phase5WorklogSummaryRow, 0, len(techTotals))
	for name, t := range techTotals {
		summaries = append(summaries, pages.Phase5WorklogSummaryRow{Name: name, Hours: trimNum(t[0]), Commission: moneyFor(t[1])})
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

	_ = pages.WorklogsPage(pages.Phase5WorklogData{Flash: flash(r), WOID: strconv.FormatInt(woID, 10), StatusLabel: statusLabel, StatusPill: statusPill[woStatus], InsuranceLabel: insuranceLabel, WODateDefault: woDateDefault, WOAmount: moneyFor(woTotal), TotalHours: trimNum(totalHours), TotalLabor: moneyFor(totalLabor), TotalCommission: moneyFor(totalCommission), Count: strconv.Itoa(len(list)), Rows: rowsOut, Summaries: summaries, Technicians: phase5Technicians()}, user).Render(r.Context(), w)
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

	if countRows("SELECT COUNT(1) FROM active_work_orders WHERE id = ?", woID) == 0 {
		http.NotFound(w, r)
		return
	}

	// 必須是 technician 角色：否則可以把佣金記到 owner/counter 名下。
	if countRows("SELECT COUNT(1) FROM users WHERE id = ? AND role = 'technician'", techID) == 0 {
		fail(w, "指定的使用者不是技師角色")
		return
	}

	// 技師只能對自己被派工的工單登錄工時，避免對未參與的工單灌水佣金；
	// 櫃檯/管理員（CanPickTechnician）仍可代任何技師登錄。
	if !actor.CanPickTechnician() {
		if countRows("SELECT COUNT(1) FROM active_work_orders WHERE id = ? AND technician_id = ?", woID, actor.ID) == 0 {
			fail(w, "您未被派工這張工單，無法登錄工時")
			return
		}
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

	_ = pages.EditWorklogPage(pages.Phase5EditWorklogData{WOID: strconv.FormatInt(woID, 10), LogID: strconv.FormatInt(logID, 10), Technician: techName, Date: lg.WorkDate, Hours: trimNum(lg.Hours), Rate: trimNum(lg.HourlyRate), CommissionRate: trimNum(lg.CommissionRate), Notes: lg.Notes}, user).Render(r.Context(), w)
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

	res, err := db.DB.Exec(`UPDATE technician_worklogs SET deleted_at = datetime('now')
		WHERE id = ? AND work_order_id = ? AND deleted_at IS NULL`, logID, woID)
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
		LEFT JOIN active_technician_worklogs wl
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

	var outRows []pages.Phase5CommissionRow

	var totalOrders, totalHours, totalLabor, totalCommission float64
	for rows.Next() {
		var id int64
		var name, username, branch string
		var hours, labor, commission, orders float64
		if err := rows.Scan(&id, &name, &username, &branch, &hours, &labor, &commission, &orders); err != nil {
			continue
		}
		outRows = append(outRows, pages.Phase5CommissionRow{Name: name, Username: username, Branch: orDash(branchLabel(branch)), Orders: money(orders), Hours: trimNum(hours), Labor: money(labor), Commission: money(commission)})
		totalOrders += orders
		totalHours += hours
		totalLabor += labor
		totalCommission += commission
	}
	_ = pages.CommissionsPage(pages.Phase5CommissionsData{Flash: flash(r), Month: month, TotalOrders: money(totalOrders), TotalHours: trimNum(totalHours), TotalLabor: money(totalLabor), TotalCommission: money(totalCommission), Rows: outRows}, user).Render(r.Context(), w)
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
