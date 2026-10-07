package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// workOrderCols 的欄位順序必須與 scanWorkOrder 的 Scan 目標完全一致。
// 新增欄位時兩邊都要同步，且順序不可調換——
// SQLite driver 只比對數量，順序錯了不會報錯而是默默塞錯值。
const workOrderCols = `wo.id, wo.vehicle_id, wo.customer_id, COALESCE(wo.technician_id,0),
	COALESCE(v.plate_number,''), COALESCE(v.brand_model,''), COALESCE(c.name,''),
	COALESCE(c.phone,''), COALESCE(u.name,''), wo.status, wo.current_mileage,
	COALESCE(wo.customer_complaint,''), wo.approval_status, COALESCE(wo.total_amount,0),
	COALESCE(wo.sop_completed,0), COALESCE(wo.notes,''),
	COALESCE(wo.insurance_company_id,0), COALESCE(ic.name,''),
	COALESCE(wo.claim_no,''), COALESCE(wo.is_insurance,0),
	COALESCE(wo.accident_date,''), COALESCE(wo.deductible_amount,0),
	wo.created_at, wo.updated_at`

func scanWorkOrder(rows interface{ Scan(...interface{}) error }) (model.WorkOrder, error) {
	var wo model.WorkOrder
	// Scan 目標數（24）必須與 workOrderCols 的欄位數（24）完全一致。
	//
	// 時間欄位刻意掃成字串再自行解析：舊系統遷移進來的 created_at 是
	// '2016/02/22'，直接掃進 time.Time 會解析失敗，而錯誤被呼叫端的
	// continue 吞掉 → 列表只顯示新工單、舊工單全部消失。
	var isIns int
	var createdAt, updatedAt string
	err := rows.Scan(&wo.ID, &wo.VehicleID, &wo.CustomerID, &wo.TechnicianID,
		&wo.PlateNumber, &wo.BrandModel, &wo.CustomerName, &wo.CustomerPhone, &wo.TechnicianName,
		&wo.Status, &wo.CurrentMileage, &wo.CustomerComplaint, &wo.ApprovalStatus,
		&wo.TotalAmount, &wo.SOPCompleted, &wo.Notes,
		&wo.InsuranceCompanyID, &wo.InsuranceName, &wo.ClaimNo, &isIns,
		&wo.AccidentDate, &wo.DeductibleAmount,
		&createdAt, &updatedAt)
	if err != nil {
		return wo, err
	}
	wo.IsInsurance = isIns != 0
	wo.CreatedAt = parseFlexTime(createdAt)
	wo.UpdatedAt = parseFlexTime(updatedAt)
	return wo, nil
}

const workOrderFrom = ` FROM work_orders wo
	JOIN vehicles v ON v.id = wo.vehicle_id
	JOIN customers c ON c.id = wo.customer_id
	LEFT JOIN users u ON u.id = wo.technician_id
	LEFT JOIN insurance_companies ic ON ic.id = wo.insurance_company_id `

func ListWorkOrders(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	status := qv.Get("status")
	q := strings.TrimSpace(qv.Get("q"))
	from := strings.TrimSpace(qv.Get("from"))
	to := strings.TrimSpace(qv.Get("to"))
	tech := strings.TrimSpace(qv.Get("tech")) // 技師篩選（users.id，'0' = 未指派）
	part := strings.TrimSpace(qv.Get("part")) // 零件篩選：料號或名稱的文字查詢
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if status != "" {
		if _, ok := workOrderStatusLabel[status]; ok {
			where += ` AND wo.status = ?`
			args = append(args, status)
		}
	}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ? OR c.phone LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	// 技師篩選。
	//   tech='0'     → 只看未指派（technician_id 為 0 或 NULL）
	//   tech='<id>'  → 該技師
	// 未指派刻意用獨立值而非留空：留空等於「不篩選」，
	// 而「找出漏派工的單」正是這頁最需要的查詢之一。
	if tech == "0" {
		where += ` AND COALESCE(wo.technician_id,0) = 0`
	} else if n := mustInt(tech); n > 0 {
		where += ` AND wo.technician_id = ?`
		args = append(args, n)
	}
	// 零件篩選：工單明細曾用過這個料號或名稱。
	//
	// 為什麼用文字而非下拉：料號有 10,366 個，做成 <select> 會讓這一頁
	// 從 13 KB 膨脹到 647 KB。用 LIKE 打「雨刷」「BE071」比捲動一萬項快。
	//
	// 用 EXISTS 而非 JOIN：同一張工單可能分多列用同一個料號
	// （實測 5,489 組，最多 10 列），JOIN 會讓該工單重複出現。
	if part != "" {
		where += ` AND EXISTS (SELECT 1 FROM work_order_items i
			LEFT JOIN inventory_items ii ON ii.id = i.inventory_item_id
			WHERE i.work_order_id = wo.id
			  AND (ii.part_number LIKE ? OR ii.name LIKE ? OR i.description LIKE ?))`
		like := "%" + part + "%"
		args = append(args, like, like, like)
	}
	// 日期區間。
	//
	// 注意：SQLite 的 date() 不吃斜線格式（date('2016/02/22') 會回 NULL），
	// 而舊系統遷移進來的 created_at 正是 'YYYY/MM/DD'。
	// 因此先用 replace() 正規化成 '-' 再比對，兩種格式都能正確篩選。
	const woDate = `date(replace(wo.created_at,'/','-'))`
	if from != "" {
		where += ` AND ` + woDate + ` >= date(?)`
		args = append(args, from)
	}
	if to != "" {
		where += ` AND ` + woDate + ` <= date(?)`
		args = append(args, to)
	}

	// 總筆數（分頁用）
	total := 0
	db.DB.QueryRow(`SELECT COUNT(1) FROM work_orders wo
		JOIN vehicles v ON v.id = wo.vehicle_id
		JOIN customers c ON c.id = wo.customer_id`+where, args...).Scan(&total)

	query := `SELECT ` + workOrderCols + workOrderFrom + where +
		` ORDER BY wo.created_at DESC, wo.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢工單失敗")
		return
	}
	defer rows.Close()

	var list []model.WorkOrder
	for rows.Next() {
		wo, err := scanWorkOrder(rows)
		if err != nil {
			continue
		}
		list = append(list, wo)
	}
	rows.Close()

	// 零件欄：整頁一次查回，避免每列一次查詢（N+1）。
	// 同料號分多列的情況在這裡彙總（見 woPartUsage 的說明）。
	if len(list) > 0 {
		ids := make([]string, 0, len(list))
		idx := make(map[int64]int, len(list))
		for i, wo := range list {
			ids = append(ids, strconv.FormatInt(wo.ID, 10))
			idx[wo.ID] = i
		}
		q := `SELECT i.work_order_id, COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,''),
				COALESCE(SUM(i.quantity),0)
			FROM work_order_items i
			LEFT JOIN inventory_items ii ON ii.id = i.inventory_item_id
			WHERE i.item_type = 'part' AND i.work_order_id IN (` + strings.Join(ids, ",") + `)
			GROUP BY i.work_order_id, COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,'')
			ORDER BY i.work_order_id, COALESCE(ii.part_number,'')`
		if pr, perr := db.DB.Query(q); perr == nil {
			for pr.Next() {
				var wid int64
				var part, name string
				var qty float64
				if err := pr.Scan(&wid, &part, &name, &qty); err != nil {
					continue
				}
				i, ok := idx[wid]
				if !ok {
					continue
				}
				if list[i].PartSummary != "" {
					list[i].PartSummary += "、"
				}
				list[i].PartSummary += part + "×" + trimQty(qty)
				list[i].PartCount++
			}
			pr.Close()
		}
	}

	var tb strings.Builder
	tb.WriteString(`<table><tr><th>#</th><th>車牌</th><th>客戶</th><th>技師</th><th>狀態</th><th>核准</th><th>零件</th><th class="num">里程</th><th class="num">金額</th><th>日期</th></tr>`)
	for _, wo := range list {
		insTag := ""
		if wo.IsInsurance {
			insTag = ` <span class="pill p-waitparts">保險</span>`
		}
		// 零件欄：只在有明細時顯示，且彙總同料號的多列。
		// 一次查詢取回整頁（最多 50 筆）用過的零件，避免每列一次查詢。
		partCell := `<span style="color:#475569">—</span>`
		if wo.PartCount > 0 {
			partCell = htmlEsc(wo.PartSummary)
			if wo.PartCount > 1 {
				partCell += ` <span style="color:#64748b;font-size:11.5px">等 ` +
					strconv.Itoa(wo.PartCount) + ` 項</span>`
			}
		}
		tb.WriteString(`<tr><td><a href="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `">` + strconv.FormatInt(wo.ID, 10) + `</a></td><td><b>` +
			htmlEsc(wo.PlateNumber) + `</b><br><span style="color:#64748b;font-size:12px">` + htmlEsc(wo.BrandModel) + `</span></td><td>` +
			htmlEsc(wo.CustomerName) + `<br><span style="color:#64748b;font-size:12px">` + htmlEsc(wo.CustomerPhone) + `</span></td><td>` +
			htmlEsc(wo.TechnicianName) + `</td><td><span class="pill ` + statusPill[wo.Status] + `">` + workOrderStatusLabel[wo.Status] +
			`</span>` + insTag + `</td><td><span class="pill ` + approvalPill[wo.ApprovalStatus] + `">` + approvalLabel[wo.ApprovalStatus] +
			`</span></td><td style="font-size:12.5px;max-width:190px">` + partCell +
			`</td><td class="num">` + strconv.Itoa(wo.CurrentMileage) + `</td><td class="num">` + money(wo.TotalAmount) +
			`</td><td style="font-size:12.5px;color:#94a3b8">` + htmlEsc(woCreatedText(wo.CreatedAt)) + `</td></tr>`)
	}
	tb.WriteString(`</table>`)

	table := tb.String()
	if len(list) == 0 {
		table = `<div class="empty">沒有符合條件的工單</div>`
	}

	// 狀態分頁籤（保留搜尋與日期條件）
	var tabs strings.Builder
	tabs.WriteString(`<a href="` + woTabURL(qv, "") + `" class="btn ` + btnIf(status == "", "btn-p", "btn-s") + `">全部</a> `)
	for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid"} {
		tabs.WriteString(`<a href="` + woTabURL(qv, s) + `" class="btn ` + btnIf(status == s, "btn-p", "btn-s") + `">` +
			workOrderStatusLabel[s] + `</a> `)
	}

	pg := pager("/work-orders", qv, total, page, size)

	body := flash(r) + `
    <div class="card">
      <h2>工單列表（符合條件 ` + strconv.Itoa(total) + ` 筆）</h2>
      <div style="margin-bottom:12px;display:flex;gap:8px;align-items:center;flex-wrap:wrap">
        <a class="btn btn-p" href="/work-orders/new">＋ 開立新工單</a>
        <span style="color:#64748b;font-size:12.5px">（可搜尋車牌 / 車主 / 電話，並限定日期區間）</span>
      </div>
      <div style="margin-bottom:12px">` + tabs.String() + `</div>
      <form method="GET" action="/work-orders" style="margin-bottom:12px;display:flex;gap:8px;flex-wrap:wrap;align-items:center">
        <input name="q" value="` + htmlEsc(q) + `" placeholder="搜尋車牌 / 客戶 / 電話" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:240px">
        <label style="font-size:12.5px;color:#94a3b8">技師
          <select name="tech" style="padding:7px 9px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0">
            <option value="">全部</option>
            <option value="0"` + selIf(tech == "0") + `>— 未指派 —</option>` +
		woTechFilterOptions(tech) + `
          </select>
        </label>
        <label style="font-size:12.5px;color:#94a3b8">零件
          <input name="part" value="` + htmlEsc(part) + `" placeholder="料號 / 名稱，例：雨刷" style="padding:8px 10px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;width:190px">
        </label>
        <input type="hidden" name="status" value="` + htmlEsc(status) + `">
        <label style="font-size:12.5px;color:#94a3b8">日期從
          <input type="date" name="from" value="` + htmlEsc(from) + `" style="padding:7px 9px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0">
        </label>
        <label style="font-size:12.5px;color:#94a3b8">到
          <input type="date" name="to" value="` + htmlEsc(to) + `" style="padding:7px 9px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0">
        </label>
        <button class="btn btn-p" type="submit">搜尋</button>
        <a class="btn btn-s" href="/work-orders">清除</a>
      </form>` + table + pg + `<div style="margin-top:6px">` + perPageSel("/work-orders", qv, size) + `</div>
    </div>`

	renderPage(w, "工單管理", "/work-orders", user.Username, user.Role, body)
}

// woCreatedText 工單日期顯示。
//
// 舊資料遷移進來的 created_at 是 'YYYY/MM/DD' 字串（SQLite 無嚴格型別，
// driver 會依欄位宣告轉成 time.Time）；新開的工單則是 DATETIME。
// 這裡統一輸出成 YYYY-MM-DD，無法解析時退回原字串。
func woCreatedText(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

// woTabURL 產生狀態分頁籤連結，保留 q / from / to / tech / part 條件。
func woTabURL(qv url.Values, status string) string {
	out := url.Values{}
	for _, k := range []string{"q", "from", "to", "tech", "part"} {
		if v := qv.Get(k); v != "" {
			out.Set(k, v)
		}
	}
	if status != "" {
		out.Set("status", status)
	}
	if s := out.Encode(); s != "" {
		return "/work-orders?" + s
	}
	return "/work-orders"
}

// woTechFilterOptions 工單列表的技師篩選下拉。
//
// 與 technicianOptions() 分開，因為用途不同：
//   technicianOptions()     指派技師 —— 只列「現在會動手的人」（排除行政/眷屬/廠商）
//   woTechFilterOptions()   查歷史  —— 要能查到已離職或轉行政者做的舊單，
//                                      否則 2016 年那張單會永遠找不到人。
// 兩者都排除 role='technician' 以外的帳號。
func woTechFilterOptions(selected string) string {
	rows, err := db.DB.Query(`SELECT u.id, u.name, COALESCE(u.username,''), COALESCE(u.staff_branch,'')
		FROM users u
		WHERE u.role = 'technician' AND COALESCE(u.staff_kind,'employee') = 'employee'
		ORDER BY u.staff_branch, u.name`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name, username, branch string
		if err := rows.Scan(&id, &name, &username, &branch); err != nil {
			continue
		}
		label := name + ` (` + username + `)`
		if dept := branchLabel(branch); dept != "" {
			label += `　` + dept
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `"` +
			selIf(strconv.FormatInt(id, 10) == selected) + `>` + htmlEsc(label) + `</option>`)
	}
	return b.String()
}

// trimQty 數量顯示：整數不帶小數點（2 而非 2.0），有小數才保留一位。
// 舊資料的 quantity 是 REAL（1.0、2.0…），直接印會出現一堆 .0。
func trimQty(q float64) string {
	if q == float64(int64(q)) {
		return strconv.FormatInt(int64(q), 10)
	}
	return strconv.FormatFloat(q, 'f', 1, 64)
}

// woPartUsage 回傳指定工單用過的零件彙總（料號/名稱/數量），供列表展開顯示。
//
// 為什麼要彙總：同一張工單可能分多列用同一個料號（實測 5,489 組，最多 10 列），
// 逐列顯示會讓「用了幾個」看起來比實際少。這裡 SUM(quantity) 後才對。
func woPartUsage(workOrderID int64) []struct {
	Part string
	Name string
	Qty  float64
} {
	rows, err := db.DB.Query(`SELECT COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,''),
			COALESCE(SUM(i.quantity),0)
		FROM work_order_items i
		LEFT JOIN inventory_items ii ON ii.id = i.inventory_item_id
		WHERE i.work_order_id = ? AND i.item_type = 'part'
		GROUP BY COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,'')
		ORDER BY COALESCE(ii.part_number,'')`, workOrderID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []struct {
		Part string
		Name string
		Qty  float64
	}
	for rows.Next() {
		var r struct {
			Part string
			Name string
			Qty  float64
		}
		if err := rows.Scan(&r.Part, &r.Name, &r.Qty); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

func CreateWorkOrder(w http.ResponseWriter, r *http.Request) {
	vehicleID := mustInt(r.FormValue("vehicle_id"))
	mileage := atoiDefault(r.FormValue("current_mileage"), -1)

	if vehicleID == 0 {
		fail(w, "請選擇車輛")
		return
	}
	if mileage < 0 {
		fail(w, "請填寫進廠里程")
		return
	}

	var customerID int64
	err := db.DB.QueryRow("SELECT customer_id FROM vehicles WHERE id = ?", vehicleID).Scan(&customerID)
	if err != nil {
		fail(w, "找不到該車輛")
		return
	}

	approval := r.FormValue("approval_status")
	if approval != "pending" {
		approval = "not_required"
	}

	techID := mustInt(r.FormValue("technician_id"))
	if techID > 0 && countRows("SELECT COUNT(1) FROM users WHERE id = ? AND role = 'technician'", techID) == 0 {
		techID = 0
	}

	var techArg interface{}
	if techID > 0 {
		techArg = techID
	}

	// 保險理賠（CAR35 P0-1）
	// 保險公司必須是「啟用中」才允許綁定，避免選到已停用的歷史資料。
	// 前端在保險欄位 disabled 時不會送出 insurance_company_id，
	// 因此以 _mirror 的 hidden 欄位為準（未勾保險件時 mirror 為空字串）。
	insID := mustInt(r.FormValue("insurance_company_id_mirror"))
	if insID == 0 {
		insID = mustInt(r.FormValue("insurance_company_id"))
	}
	if insID > 0 && countRows(
		"SELECT COUNT(1) FROM insurance_companies WHERE id = ? AND active = 1", insID) == 0 {
		fail(w, "找不到這家保險公司（或已停用）")
		return
	}
	isIns := 0
	if mustInt(r.FormValue("is_insurance")) == 1 {
		isIns = 1
	}
	// 勾選保險件卻沒選公司 → 報表無法歸屬，直接擋下讓櫃檯補齊。
	if isIns == 1 && insID == 0 {
		fail(w, "請選擇保險公司")
		return
	}
	var insArg interface{}
	if insID > 0 {
		insArg = insID
	}
	claimNo := strings.TrimSpace(r.FormValue("claim_no"))
	accidentDate := strings.TrimSpace(r.FormValue("accident_date"))
	deductible := atofDefault(r.FormValue("deductible_amount"), 0)

	res, err := db.DB.Exec(`INSERT INTO work_orders
		(vehicle_id, customer_id, technician_id, status, current_mileage,
		 customer_complaint, approval_status, total_amount, sop_completed,
		 insurance_company_id, claim_no, is_insurance, accident_date, deductible_amount)
		VALUES (?,?,?,'pending',?,?,?,0,0,?,?,?,?,?)`,
		vehicleID, customerID, techArg, mileage,
		nullStr(r.FormValue("customer_complaint")), approval,
		insArg, nullStr(claimNo), isIns, nullStr(accidentDate), deductible)
	if err != nil {
		fail(w, "建立工單失敗")
		return
	}

	id, _ := res.LastInsertId()
	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "工單已建立")
}

func ShowWorkOrder(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))

	row := db.DB.QueryRow(`SELECT `+workOrderCols+workOrderFrom+` WHERE wo.id = ?`, id)
	wo, err := scanWorkOrder(row)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	items, itemTotal := loadWorkOrderItems(id)

	// 已結帳工單唯讀：隱藏所有編輯與刪除控件（後端也擋，見 requireOrderMutable）。
	locked := wo.Status == "paid"
	canEdit := !locked

	var ib strings.Builder
	ib.WriteString(`<table><tr><th>類型</th><th>描述</th><th class="num">數量</th><th class="num">單價</th><th class="num">小計</th><th>主技師</th><th>助手</th><th>操作</th></tr>`)
	for _, it := range items {
		typeLabel := map[string]string{"labor": "工資", "part": "零件", "subcontract": "外包"}[it.ItemType]
		delCell := `<td style="color:#64748b">—</td>`
		if canEdit {
			delCell = `<td><a class="btn btn-d" href="/work-orders/` + strconv.FormatInt(id, 10) + `/items/` +
				strconv.FormatInt(it.ID, 10) + `/delete" onclick="return confirm('刪除此項目？')">刪除</a></td>`
		}
		ib.WriteString(`<tr><td><span class="pill p-progress">` + typeLabel + `</span></td><td>` + htmlEsc(it.Description) +
			`</td><td class="num">` + strconv.Itoa(it.Quantity) + `</td><td class="num">` + money(it.UnitPrice) +
			`</td><td class="num">` + money(it.Subtotal) + `</td><td>` + htmlEsc(techName(it.PrimaryTechnicianID)) +
			`</td><td>` + htmlEsc(techName(it.AssistantTechID)) + `</td>` + delCell + `</tr>`)
	}
	ib.WriteString(`</table>`)

	itemsTable := ib.String()
	if len(items) == 0 {
		itemsTable = `<div class="empty">尚未加入任何工資或零件</div>`
	}

	// 保險理賠資訊（CAR35 P0-1，對應 Header.Insurance）
	insuranceRow := ""
	if wo.IsInsurance || wo.InsuranceCompanyID > 0 {
		insuranceRow = `<tr><th>件別</th><td><span class="pill p-progress">保險理賠</span></td>
			<th>保險公司</th><td>` + htmlEsc(orDash(wo.InsuranceName)) + `</td></tr>
			<tr><th>理賠編號</th><td>` + htmlEsc(orDash(wo.ClaimNo)) + `</td>
			<th>事故日期</th><td>` + htmlEsc(orDash(wo.AccidentDate)) + `</td></tr>
			<tr><th>自負額</th><td>` + money(wo.DeductibleAmount) + `</td><th></th><td></td></tr>`
	} else {
		insuranceRow = `<tr><th>件別</th><td><span class="pill">自費</span></td>
			<th></th><td></td></tr>`
	}

	body := flash(r) + `
    <div class="card">
      <h2>工單 #` + strconv.FormatInt(wo.ID, 10) + ` — ` + htmlEsc(wo.PlateNumber) + `</h2>
      <table>
        <tr><th style="width:130px">車型</th><td>` + htmlEsc(wo.BrandModel) + `</td><th style="width:130px">進廠里程</th><td>` + strconv.Itoa(wo.CurrentMileage) + ` km</td></tr>
        <tr><th>客戶</th><td>` + htmlEsc(wo.CustomerName) + ` / ` + htmlEsc(wo.CustomerPhone) + `</td><th>技師</th><td>` + htmlEsc(orDash(wo.TechnicianName)) + `</td></tr>
        <tr><th>狀態</th><td><span class="pill ` + statusPill[wo.Status] + `">` + workOrderStatusLabel[wo.Status] + `</span></td>
            <th>核准狀態</th><td><span class="pill ` + approvalPill[wo.ApprovalStatus] + `">` + approvalLabel[wo.ApprovalStatus] + `</span></td></tr>
        <tr><th>客戶反映</th><td colspan="3">` + htmlEsc(orDash(wo.CustomerComplaint)) + `</td></tr>
        <tr><th>SOP</th><td>` + yesNo(wo.SOPCompleted) + `</td><th>建立時間</th><td>` + wo.CreatedAt.Format("2006-01-02 15:04") + `</td></tr>
        ` + insuranceRow + `
      </table>
      <div style="margin-top:16px">` + statusControls(wo, canEdit) + `
        <div style="margin-top:14px;padding-top:14px;border-top:1px solid #334155">
          <a class="btn btn-p" href="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/receipt">🧾 收據列印</a>
          <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/worklogs">⏱ 技師工時</a>
          <a class="btn btn-s" href="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/photos">📷 車損照片</a>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>工單項目</h2>` + itemsTable + `
      ` + addItemForm(wo, canEdit) + `
    </div>

    <div class="card">
      <h2>合計</h2>
      <table>
        <tr><th>項目小計</th><td class="num">` + money(itemTotal) + `</td></tr>
        <tr><th>工單總額</th><td class="num" style="font-size:20px;font-weight:700;color:#38bdf8">` + money(wo.TotalAmount) + `</td></tr>
      </table>
      <p style="color:#64748b;font-size:12.5px;margin-top:10px">新增或刪除項目時自動重算工單總額。</p>
    </div>`

	renderPage(w, "工單 #"+strconv.FormatInt(wo.ID, 10), "/work-orders", user.Username, user.Role, body)
}

func UpdateWorkOrderStatus(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	status := r.FormValue("status")
	if _, ok := workOrderStatusLabel[status]; !ok {
		fail(w, "無效狀態")
		return
	}

	// 結帳扣料的異動紀錄需要操作人
	actorID := int64(0)
	if u, uerr := currentUser(r); uerr == nil {
		actorID = u.ID
	}

	// 已結帳的工單是財務憑證，不允許再改狀態：否則扣料與營收會與實物對不上。
	if currentStatus := countStatus(id); currentStatus == "paid" {
		if status == "paid" {
			// 冪等：重複結帳視為成功，但不得重複扣料或重複計營收。
			redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10),
				"此工單已是「已結帳」，未重複計算")
			return
		}
		fail(w, "此工單已結帳，不可變更為「"+workOrderStatusLabel[status]+"」。如需更正請另行開立沖銷單。")
		return
	}

	// 結帳扣料與累計營收必須在同一個 transaction 內完成，並用「目前狀態 = paid」
	// 作為條件（compare-and-set）。如此重複送出或並行請求最多只有一次會生效，
	// 不會重複扣庫存或重複累計消費。
	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	cas, err := tx.Exec(`UPDATE work_orders SET status=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status != 'paid'`, status, id)
	if err != nil {
		fail(w, "更新狀態失敗")
		return
	}
	if n, _ := cas.RowsAffected(); n == 0 {
		if countStatus(id) == "paid" {
			redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10),
				"此工單已是「已結帳」，未重複計算")
			return
		}
		http.NotFound(w, r)
		return
	}

	if status == "paid" {

		// 扣料：讀出該工單所有「零件類」項目，按實際數量扣庫（Go 側計算，避免複雜 UPDATE 子查詢）
		var deltas []struct {
			invID int64
			qty   int
		}
		rows, err := tx.Query(`SELECT inventory_item_id, quantity
											FROM work_order_items
											WHERE work_order_id = ? AND item_type = 'part' AND inventory_item_id > 0 AND quantity > 0
											ORDER BY id`, id)
		if err != nil {
			fail(w, "讀取工單項目失敗")
			return
		}
		for rows.Next() {
			var invID int64
			var qty int
			if err := rows.Scan(&invID, &qty); err != nil {
				rows.Close()
				fail(w, "讀取工單項目失敗")
				return
			}
			deltas = append(deltas, struct {
				invID int64
				qty   int
			}{invID, qty})
		}
		rows.Close()

		partsDeducted := 0
		for _, d := range deltas {
			// 先檢查足額，避免結帳時超賣
			var avail int
			if err := tx.QueryRow("SELECT COALESCE(stock_quantity,0) FROM inventory_items WHERE id = ?", d.invID).Scan(&avail); err != nil {
				fail(w, "查詢庫存失敗")
				return
			}
			if avail < d.qty {
				fail(w, "結帳時庫存不足："+strconv.FormatInt(d.invID, 10)+" 僅剩 "+strconv.Itoa(avail)+"，需要 "+strconv.Itoa(d.qty))
				return
			}
			if _, err := tx.Exec("UPDATE inventory_items SET stock_quantity = stock_quantity - ? WHERE id = ?", d.qty, d.invID); err != nil {
				fail(w, "扣減庫存失敗")
				return
			}
			// 留痕：結帳扣料同樣寫入異動紀錄，否則庫存對帳會出現缺口
			if err := recordMovement(tx, d.invID, -d.qty, avail, avail-d.qty,
				"工單 #"+strconv.FormatInt(id, 10)+" 結帳扣料", "checkout", id, actorID); err != nil {
				fail(w, "寫入異動紀錄失敗")
				return
			}
			partsDeducted++
		}

		// 累計客戶消費（扣料不改變工單金額）。
		// 不以 partsDeducted 為條件：純工資的工單同樣要累計消費。
		// 必須用 tx 而非 db.DB：此時 transaction 仍持有寫入鎖，
		// 用獨立連線會因 SQLite 單寫者限制而失敗。
		if _, err := tx.Exec(`UPDATE customers SET total_spent = COALESCE(total_spent,0) +
			(SELECT total_amount FROM work_orders WHERE id = ?), unpaid_amount = 0 WHERE id =
			(SELECT customer_id FROM work_orders WHERE id = ?)`, id, id); err != nil {
			fail(w, "累計消費失敗")
			return
		}
	}

	// 非結帳的狀態轉換也要提交，否則上面的 CAS 會被 rollback。
	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "狀態已更新為「"+workOrderStatusLabel[status]+"」")
}

// countStatus 讀取工單目前狀態；查無回傳空字串。
func countStatus(id int64) string {
	var s string
	if err := db.DB.QueryRow("SELECT status FROM work_orders WHERE id = ?", id).Scan(&s); err != nil {
		return ""
	}
	return s
}

// requireOrderMutable 擋下已結帳工單的內容變更。
// 已結帳代表已收款、已扣料、已開立收據，此時改內容會讓金額與實物對不上。
func requireOrderMutable(woID int64) error {
	if countStatus(woID) == "paid" {
		return errOrderLocked
	}
	return nil
}

var errOrderLocked = errors.New("此工單已結帳，內容不可再修改")

func UpdateWorkOrderApproval(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	approval := r.FormValue("approval_status")
	if _, ok := approvalLabel[approval]; !ok {
		fail(w, "無效核准狀態")
		return
	}
	if err := requireOrderMutable(id); err != nil {
		fail(w, err.Error())
		return
	}

	res, err := db.DB.Exec("UPDATE work_orders SET approval_status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?", approval, id)
	if err != nil {
		fail(w, "更新核准狀態失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "核准狀態已更新為「"+approvalLabel[approval]+"」")
}

func ToggleWorkOrderSOP(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	if err := requireOrderMutable(id); err != nil {
		fail(w, err.Error())
		return
	}

	res, err := db.DB.Exec("UPDATE work_orders SET sop_completed = 1 - COALESCE(sop_completed,0), updated_at=CURRENT_TIMESTAMP WHERE id=?", id)
	if err != nil {
		fail(w, "更新 SOP 失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "SOP 狀態已切換")
}

func AddWorkOrderItem(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	if err := requireOrderMutable(woID); err != nil {
		fail(w, err.Error())
		return
	}
	desc := strings.TrimSpace(r.FormValue("description"))

	qty := atoiDefault(r.FormValue("quantity"), 0)
	if qty < 1 {
		fail(w, "數量必須大於 0")
		return
	}

	itemType := r.FormValue("item_type")
	if _, ok := map[string]bool{"labor": true, "part": true, "subcontract": true}[itemType]; !ok {
		itemType = "labor"
	}

	invID := mustInt(r.FormValue("inventory_item_id"))
	if invID < 0 {
		invID = 0
	}

	unitPrice := atofDefault(r.FormValue("unit_price"), 0)
	if itemType == "part" && invID > 0 {
		var name string
		var sell float64
		if err := db.DB.QueryRow("SELECT name, COALESCE(selling_price,0) FROM inventory_items WHERE id=?", invID).
			Scan(&name, &sell); err == nil {
			if desc == "" {
				desc = name
			}
			unitPrice = sell
		} else {
			fail(w, "找不到該零件")
			return
		}
	}

	if desc == "" {
		fail(w, "描述為必填")
		return
	}

	var invArg interface{}
	if itemType == "part" && invID > 0 {
		invArg = invID
	}

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	var pt, at interface{}
	if v := mustInt(r.FormValue("primary_technician_id")); v > 0 {
		pt = v
	}
	if v := mustInt(r.FormValue("assistant_technician_id")); v > 0 {
		at = v
	}

	if _, err := tx.Exec(`INSERT INTO work_order_items
		(work_order_id, item_type, inventory_item_id, description, quantity, unit_price, subtotal,
		 primary_technician_id, assistant_technician_id)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		woID, itemType, invArg, desc, qty, unitPrice, float64(qty)*unitPrice, pt, at); err != nil {
		fail(w, "加入項目失敗")
		return
	}

	// 零件不於此處扣庫存。
	// 扣料統一在結帳（status=paid）時才執行，好處是未成交的工單不會佔用庫存、
	// 也不會在技師改單時需要回補。結帳路徑會先檢查足額再扣，避免超賣。
	// 庫存不足的即時回饋交由 UI 顯示預估可扣量，不阻擋加入工單。

	if err := recalcWorkOrderTotal(tx, woID); err != nil {
		fail(w, "重算金額失敗")
		return
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10), "已加入項目")
}

func DeleteWorkOrderItem(w http.ResponseWriter, r *http.Request) {
	woID := mustInt(r.PathValue("id"))
	itemID := mustInt(r.PathValue("item_id"))

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

	// 讀取項目（僅用於校驗存在性；扣料在結帳時才發生，故此處不回補庫存）
	var itemType string
	err = tx.QueryRow("SELECT item_type FROM work_order_items WHERE id = ? AND work_order_id = ?", itemID, woID).
		Scan(&itemType)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 已結帳（paid）的工單不允許再改動項目，否則庫存與帳務會對不上。
	var status string
	if err := tx.QueryRow("SELECT status FROM work_orders WHERE id = ?", woID).Scan(&status); err == nil && status == "paid" {
		fail(w, "工單已結帳，無法刪除項目")
		return
	}

	if _, err := tx.Exec("DELETE FROM work_order_items WHERE id = ? AND work_order_id = ?", itemID, woID); err != nil {
		fail(w, "刪除項目失敗")
		return
	}

	if err := recalcWorkOrderTotal(tx, woID); err != nil {
		fail(w, "重算金額失敗")
		return
	}

	if err := tx.Commit(); err != nil {
		fail(w, "提交失敗")
		return
	}

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(woID, 10), "已刪除項目")
}

func loadWorkOrderItems(woID int64) ([]model.WorkOrderItem, float64) {
	rows, err := db.DB.Query(`SELECT id, work_order_id, item_type, COALESCE(inventory_item_id,0),
		description, COALESCE(quantity,0), COALESCE(unit_price,0), COALESCE(subtotal,0),
		COALESCE(primary_technician_id,0), COALESCE(assistant_technician_id,0), COALESCE(photo_url,'')
		FROM work_order_items WHERE work_order_id = ? ORDER BY id`, woID)
	if err != nil {
		return nil, 0
	}
	defer rows.Close()

	var list []model.WorkOrderItem
	total := 0.0
	for rows.Next() {
		var it model.WorkOrderItem
		if err := rows.Scan(&it.ID, &it.WorkOrderID, &it.ItemType, &it.InventoryItemID,
			&it.Description, &it.Quantity, &it.UnitPrice, &it.Subtotal,
			&it.PrimaryTechnicianID, &it.AssistantTechID, &it.PhotoURL); err != nil {
			continue
		}
		total += it.Subtotal
		list = append(list, it)
	}
	return list, total
}

func recalcWorkOrderTotal(tx *sql.Tx, woID int64) error {
	_, err := tx.Exec(`UPDATE work_orders SET total_amount =
		COALESCE((SELECT SUM(subtotal) FROM work_order_items WHERE work_order_id = ?), 0),
		updated_at = CURRENT_TIMESTAMP WHERE id = ?`, woID, woID)
	return err
}

func statusOptions(current string) string {
	var b strings.Builder
	for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid"} {
		b.WriteString(`<option value="` + s + `"` + selIf(current == s) + `>` + workOrderStatusLabel[s] + `</option>`)
	}
	return b.String()
}

func approvalOptions(current string) string {
	var b strings.Builder
	for _, s := range []string{"not_required", "pending", "approved", "rejected"} {
		b.WriteString(`<option value="` + s + `"` + selIf(current == s) + `>` + approvalLabel[s] + `</option>`)
	}
	return b.String()
}

// technicianOptions 技師下拉選單。
//
// 只列「真的會動手的人」，兩層過濾缺一不可：
//   staff_kind='employee' —— 舊系統的 Staff 表混了協力廠商（上上汽車、元發電機）
//                            與眷屬（車行家族），協力廠商請到「廠商」頁。
//   staff_branch NOT IN ('E','G','H') —— E 是行政、G 是眷屬、H 是廠商。
//                            舊資料有 40 個眷屬編號（G001…G038）在遷移時被判成
//                            employee，只靠 staff_kind 擋不掉，會讓行政與眷屬
//                            出現在技師選單裡，污染技師產值與佣金統計。
//
// 註：users 表沒有「在職」欄位（舊系統的到職/離職日未遷），所以擋不掉已離職者。
// 要處理得先加欄位，不在本次範圍。
func technicianOptions() string {
	rows, err := db.DB.Query(`SELECT u.id, u.name, u.username, COALESCE(u.staff_branch,'')
		FROM users u
		WHERE u.role = 'technician'
		  AND COALESCE(u.staff_kind,'employee') = 'employee'
		  AND UPPER(COALESCE(u.staff_branch,'')) NOT IN ('E','G','H')
		ORDER BY u.staff_branch, u.name`)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var b strings.Builder
	for rows.Next() {
		var id int64
		var name, username, branch string
		if err := rows.Scan(&id, &name, &username, &branch); err != nil {
			continue
		}
		label := name + ` (` + username + `)`
		if dept := branchLabel(branch); dept != "" {
			label += `　` + dept
		}
		b.WriteString(`<option value="` + strconv.FormatInt(id, 10) + `">` + htmlEsc(label) + `</option>`)
	}
	return b.String()
}

// branchLabel CAR35 部門代碼 → 中文（舊系統 Branch 表）。
func branchLabel(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "A":
		return "引擎"
	case "B":
		return "電機"
	case "C":
		return "鈑金"
	case "D":
		return "烤漆"
	case "E":
		return "行政"
	case "F":
		return "零件"
	case "G":
		return "眷屬"
	case "H":
		return "廠商"
	}
	return ""
}

func techName(id int64) string {
	if id == 0 {
		return "-"
	}
	var name string
	if err := db.DB.QueryRow("SELECT name FROM users WHERE id = ?", id).Scan(&name); err != nil {
		return "-"
	}
	return name
}

func hiddenStatus(status string) string {
	if status == "" {
		return ""
	}
	return `<input type="hidden" name="status" value="` + htmlEsc(status) + `">`
}

func btnIf(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func yesNo(b bool) string {
	if b {
		return `<span style="color:#86efac">已完成</span>`
	}
	return `<span style="color:#fca5a5">未完成</span>`
}

// statusControls 產生狀態／核准／SOP 的表單；已結帳時改為唯讀提示。
func statusControls(wo model.WorkOrder, canEdit bool) string {
	if !canEdit {
		return `<div style="padding:10px 14px;border-radius:8px;background:#052e16;
          color:#86efac;border:1px solid #166534;font-size:13.5px">
          此工單已結帳，內容唯讀。如需更正請另行開立沖銷單。
        </div>`
	}

	return `
        <form class="inline" method="POST" action="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/status">
          <label style="font-size:12px;color:#94a3b8">變更狀態</label>
          <select name="status" style="padding:6px 9px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;margin:0 8px">` +
		statusOptions(wo.Status) + `</select>
          <button class="btn btn-p" type="submit">更新狀態</button>
        </form>
        <form class="inline" method="POST" action="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/approval">
          <label style="font-size:12px;color:#94a3b8;margin-left:16px">核准</label>
          <select name="approval_status" style="padding:6px 9px;border-radius:7px;border:1px solid #334155;background:#0f172a;color:#e2e8f0;margin:0 8px">` +
		approvalOptions(wo.ApprovalStatus) + `</select>
          <button class="btn btn-p" type="submit">更新核准</button>
        </form>
        <form class="inline" method="POST" action="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/sop">
          <button class="btn btn-s" type="submit">` + sopToggleLabel(wo.SOPCompleted) + `</button>
        </form>`
}

// addItemForm 產生加入項目的表單；已結帳時不提供。
func addItemForm(wo model.WorkOrder, canEdit bool) string {
	if !canEdit {
		return ""
	}

	return `
      <form class="frm" method="POST" action="/work-orders/` + strconv.FormatInt(wo.ID, 10) + `/items" style="margin-top:18px">
        <label>類型<select name="item_type">
          <option value="labor">工資</option>
          <option value="part">零件</option>
          <option value="subcontract">外包</option>
        </select></label>
        <label>零件（若為零件類）<select name="inventory_item_id"><option value="0">不使用庫存</option>` +
		inventoryOptions() + `</select></label>
        <label class="full">描述<input name="description" required></label>
        <label>數量<input name="quantity" type="number" min="1" value="1" required></label>
        <label>單價<input name="unit_price" type="number" min="0" step="1" value="0" required></label>
        <label>主技師<select name="primary_technician_id"><option value="0">-</option>` + technicianOptions() + `</select></label>
        <label>助手<select name="assistant_technician_id"><option value="0">-</option>` + technicianOptions() + `</select></label>
        <div class="full"><button class="btn btn-p" type="submit">加入項目</button></div>
      </form>`
}

func sopToggleLabel(done bool) string {
	if done {
		return "取消 SOP 完成"
	}
	return "標記 SOP 完成"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
