package handler

import (
"database/sql"
"errors"
"log"
"net/http"
"net/url"
"strconv"
"strings"
"time"

"autorepairos/internal/db"
"autorepairos/internal/model"
"autorepairos/web/pages"
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
	COALESCE(wo.claim_amount,0), COALESCE(wo.claim_date,''), COALESCE(wo.claim_note,''),
	wo.created_at, wo.updated_at`


func scanWorkOrder(rows interface{ Scan(...interface{}) error }) (model.WorkOrder, error) {
	var wo model.WorkOrder
	// Scan 目標數必須與 workOrderCols 的欄位數完全一致。
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
		&wo.ClaimAmount, &wo.ClaimDate, &wo.ClaimNote,
		&createdAt, &updatedAt)
	if err != nil {
		return wo, err
	}
	wo.IsInsurance = isIns != 0
	wo.CreatedAt = parseFlexTime(createdAt)
	wo.UpdatedAt = parseFlexTime(updatedAt)
	return wo, nil
}

const workOrderFrom = ` FROM active_work_orders wo
	JOIN active_vehicles v ON v.id = wo.vehicle_id
	JOIN active_customers c ON c.id = wo.customer_id
	LEFT JOIN users u ON u.id = wo.technician_id
	LEFT JOIN insurance_companies ic ON ic.id = wo.insurance_company_id `


func ListWorkOrders(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	status := qv.Get("status")
	q := strings.TrimSpace(qv.Get("q"))
	from := strings.TrimSpace(qv.Get("from"))
	to := strings.TrimSpace(qv.Get("to"))
	tech := strings.TrimSpace(qv.Get("tech")) // 技师筛选（users.id，'0' = 未指派）
	part := strings.TrimSpace(qv.Get("part")) // 零件筛选：料号或名称的文字查询
	mine := qv.Get("mine") == "1"             // 技师只看自己的工单
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if mine {
		where += ` AND wo.technician_id = ?`
		args = append(args, user.ID)
	}
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
			LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
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
	if err := db.DB.QueryRow(`SELECT COUNT(1) FROM active_work_orders wo
		JOIN active_vehicles v ON v.id = wo.vehicle_id
		JOIN active_customers c ON c.id = wo.customer_id`+where, args...).Scan(&total); err != nil {
		log.Printf("查詢工單總數失敗: %v", err)
		fail(w, "查詢工單總數失敗")
		return
	}

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
			log.Printf("讀取工單列表失敗: %v", err)
			fail(w, "讀取工單列表失敗")
			return
		}
		list = append(list, wo)
	}
	if err := rows.Err(); err != nil {
		log.Printf("列舉工單列表失敗: %v", err)
		fail(w, "讀取工單列表失敗")
		return
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
			LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
			WHERE i.item_type = 'part' AND i.work_order_id IN (` + strings.Join(ids, ",") + `)
			GROUP BY i.work_order_id, COALESCE(ii.part_number,''), COALESCE(ii.name, i.description,'')
			ORDER BY i.work_order_id, COALESCE(ii.part_number,'')`
		if pr, perr := db.DB.Query(q); perr == nil {
			for pr.Next() {
				var wid int64
				var part, name string
				var qty float64
				if err := pr.Scan(&wid, &part, &name, &qty); err != nil {
					log.Printf("讀取工單零件摘要失敗: %v", err)
					pr.Close()
					fail(w, "讀取工單零件摘要失敗")
					return
				}
				i, ok := idx[wid]
				if !ok {
					continue
				}
				if list[i].PartSummary != "" {
					list[i].PartSummary += "、"
				}
				list[i].PartSummary += partDisplayLabel(part, name) + "×" + trimQty(qty)
				list[i].PartCount++
			}
			if err := pr.Err(); err != nil {
				log.Printf("列舉工單零件摘要失敗: %v", err)
				pr.Close()
				fail(w, "讀取工單零件摘要失敗")
				return
			}
			pr.Close()
		} else {
			log.Printf("查詢工單零件摘要失敗: %v", perr)
			fail(w, "查詢工單零件摘要失敗")
			return
		}
	}

	var woRows []pages.WorkOrderRow
	for _, wo := range list {
		woRows = append(woRows, pages.WorkOrderRow{
			ID:             wo.ID,
			PlateNumber:    wo.PlateNumber,
			BrandModel:     wo.BrandModel,
			CustomerName:   wo.CustomerName,
			CustomerPhone:  wo.CustomerPhone,
			TechnicianName: wo.TechnicianName,
			Status:         wo.Status,
			StatusLabel:    workOrderStatusLabel[wo.Status],
			StatusPill:     statusPill[wo.Status],
			ApprovalStatus: wo.ApprovalStatus,
			ApprovalLabel:  approvalLabel[wo.ApprovalStatus],
			ApprovalPill:   approvalPill[wo.ApprovalStatus],
			IsInsurance:    wo.IsInsurance,
			PartSummary:    wo.PartSummary,
			PartCount:      wo.PartCount,
			CurrentMileage: wo.CurrentMileage,
			TotalAmount:    wo.TotalAmount,
			CreatedAt:      woCreatedText(wo.CreatedAt),
		})
	}

	// 狀態分頁籤（保留搜尋與日期條件）
	var tabs strings.Builder
	if user.Role == "technician" {
		mineURL := "/work-orders?mine=1"
		if q != "" {
			mineURL += "&q=" + url.QueryEscape(q)
		}
		tabs.WriteString(`<a href="` + mineURL + `" class="btn ` + btnIf(mine, "btn-p", "btn-s") + `">我的工單</a> `)
	}
	tabs.WriteString(`<a href="` + woTabURL(qv, "") + `" class="btn ` + btnIf(WorkOrderStatus(status) == "" && !mine, "btn-p", "btn-s") + `">全部</a> `)
	for _, s := range []string{"pending", "in_progress", "waiting_parts", "completed", "paid"} {
		tabs.WriteString(`<a href="` + woTabURL(qv, s) + `" class="btn ` + btnIf(WorkOrderStatus(status) == WorkOrderStatus(s), "btn-p", "btn-s") + `">` +
			workOrderStatusLabel[s] + `</a> `)
	}

	pages.ListWorkOrdersPage(woRows, total, page, size,
		q, part, status, tech, from, to,
		tabs.String(), woTechFilterOptions(tech),
		user).Render(r.Context(), w)
}

// SearchWorkOrdersAPI 工單即時搜尋 API（HTMX 用）。

func SearchWorkOrdersAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("error") == "1" {
		w.WriteHeader(http.StatusInternalServerError)
		pages.WorkOrderSearchError().Render(r.Context(), w)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	part := strings.TrimSpace(r.URL.Query().Get("part"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	tech := strings.TrimSpace(r.URL.Query().Get("tech"))

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (v.plate_number LIKE ? OR c.name LIKE ? OR c.phone LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if status != "" {
		if _, ok := workOrderStatusLabel[status]; ok {
			where += ` AND wo.status = ?`
			args = append(args, status)
		}
	}
	if part != "" {
		where += ` AND EXISTS (SELECT 1 FROM work_order_items i
			LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
			WHERE i.work_order_id = wo.id
			  AND (ii.part_number LIKE ? OR ii.name LIKE ? OR i.description LIKE ?))`
		like := "%" + part + "%"
		args = append(args, like, like, like)
	}
	if tech == "0" {
		where += ` AND COALESCE(wo.technician_id,0) = 0`
	} else if n := mustInt(tech); n > 0 {
		where += ` AND wo.technician_id = ?`
		args = append(args, n)
	}

	rows, err := db.DB.Query(`SELECT `+workOrderCols+workOrderFrom+where+
		` ORDER BY wo.created_at DESC, wo.id DESC LIMIT 50`, args...)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		pages.WorkOrderSearchError().Render(r.Context(), w)
		return
	}
	defer rows.Close()

	var result []pages.WorkOrderRow
	for rows.Next() {
		wo, err := scanWorkOrder(rows)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			pages.WorkOrderSearchError().Render(r.Context(), w)
			return
		}
		result = append(result, pages.WorkOrderRow{
			ID:             wo.ID,
			PlateNumber:    wo.PlateNumber,
			BrandModel:     wo.BrandModel,
			CustomerName:   wo.CustomerName,
			CustomerPhone:  wo.CustomerPhone,
			TechnicianName: wo.TechnicianName,
			Status:         wo.Status,
			StatusLabel:    workOrderStatusLabel[wo.Status],
			StatusPill:     statusPill[wo.Status],
			ApprovalStatus: wo.ApprovalStatus,
			ApprovalLabel:  approvalLabel[wo.ApprovalStatus],
			ApprovalPill:   approvalPill[wo.ApprovalStatus],
			IsInsurance:    wo.IsInsurance,
			CurrentMileage: wo.CurrentMileage,
			TotalAmount:    wo.TotalAmount,
			CreatedAt:      woCreatedText(wo.CreatedAt),
		})
	}
	if err := rows.Err(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		pages.WorkOrderSearchError().Render(r.Context(), w)
		return
	}
	pages.WorkOrderSearchRows(result).Render(r.Context(), w)
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
//
//	technicianOptions()     指派技師 —— 只列「現在會動手的人」（排除行政/眷屬/廠商）
//	woTechFilterOptions()   查歷史  —— 要能查到已離職或轉行政者做的舊單，
//	                                   否則 2016 年那張單會永遠找不到人。
//
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


func partDisplayLabel(part, name string) string {
	part = strings.TrimSpace(part)
	name = strings.TrimSpace(name)
	switch {
	case name != "" && part != "" && name != part:
		return name + " (" + part + ")"
	case name != "":
		return name
	default:
		return part
	}
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
		LEFT JOIN active_inventory_items ii ON ii.id = i.inventory_item_id
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
	err := db.DB.QueryRow("SELECT customer_id FROM active_vehicles WHERE id = ?", vehicleID).Scan(&customerID)
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
	accidentDate := normDateOrEmpty(r.FormValue("accident_date"))
	deductible := atofDefault(r.FormValue("deductible_amount"), 0)
	claimDate := normDateOrEmpty(r.FormValue("claim_date"))
	if claimDate != "" {
		if _, err := time.Parse("2006-01-02", claimDate); err != nil {
			fail(w, "理賠日期格式錯誤")
			return
		}
	}
	claimAmount, amountErr := parseClaimAmount(r.FormValue("claim_amount"))
	if amountErr != nil || claimAmount < 0 {
		fail(w, "理賠金額必須為不小於 0 的數字")
		return
	}

	// 預約來源：由預約進廠開單時，工單與預約要在同一交易內一起寫入。
	// 分開做的話，中途失敗會留下「工單建了但預約沒標記」（或反之），
	// 於是預約列表與工單列表對不起來。
	appointmentID := mustInt(r.FormValue("appointment_id"))

	tx, err := db.DB.Begin()
	if err != nil {
		fail(w, "資料庫忙碌")
		return
	}
	defer tx.Rollback()

	var apptArg interface{}
	if appointmentID > 0 {
		var apptVehicle, apptCustomer int64
		var apptStatus string
		err := tx.QueryRow(`SELECT vehicle_id, customer_id, status
			FROM appointments WHERE id = ?`, appointmentID).
			Scan(&apptVehicle, &apptCustomer, &apptStatus)
		switch {
		case err != nil:
			// 預約不存在：退回一般開單。不該因為來源不見就擋下整張工單——
			// 車已經在廠裡了，先能開單比計較來源重要。
			appointmentID = 0
		case apptVehicle != vehicleID || apptCustomer != customerID:
			// 預約與工單的車輛／客戶不符（櫃檯改選了車）。此時不能把這筆預約
			// 標記成已進廠，那會讓預約狀態與事實不符。
			appointmentID = 0
		case apptStatus == "cancelled" || apptStatus == "completed":
			appointmentID = 0
		default:
			apptArg = appointmentID
		}
	}

	res, err := tx.Exec(`INSERT INTO work_orders
		(vehicle_id, customer_id, technician_id, status, current_mileage,
		 customer_complaint, approval_status, total_amount, sop_completed,
		 insurance_company_id, claim_no, is_insurance, accident_date, deductible_amount,
		 claim_amount, claim_date, claim_note, appointment_id)
		VALUES (?,?,?,'pending',?,?,?,0,0,?,?,?,?,?,?,?,?,?)`,
		vehicleID, customerID, techArg, mileage,
		nullStr(r.FormValue("customer_complaint")), approval,
		insArg, nullStr(claimNo), isIns, nullStr(accidentDate), deductible,
		claimAmount, claimDate, strings.TrimSpace(r.FormValue("claim_note")),
		apptArg)
	if err != nil {
		fail(w, "建立工單失敗")
		return
	}

	id, _ := res.LastInsertId()

	// 預約標記為已進廠。限定狀態：已在 arrived 的不重複寫、已取消／已完成的不改。
	if apptArg != nil {
		if _, err := tx.Exec(`UPDATE appointments
			SET status = 'arrived', updated_at = CURRENT_TIMESTAMP
			WHERE id = ? AND status IN ('booked','confirmed')`, appointmentID); err != nil {
			fail(w, "更新預約狀態失敗")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		fail(w, "建立工單失敗")
		return
	}

	// 里程履歷（FR-VH-03）：進廠工單的里程自動帶入履歷，
	// 來源標記 work_order 供追溯。寫入失敗不擋開單——里程是輔助資訊，
	// 工單本身才是主記錄。
	RecordMileage(vehicleID, mileage, "work_order", id, "", 0)

	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "工單已建立")
}


func parseClaimAmount(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

// UpdateWorkOrderClaim updates claim details on an existing, mutable work order.

func UpdateWorkOrderClaim(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	if err := requireOrderMutable(id); err != nil {
		fail(w, err.Error())
		return
	}
	claimDate := normDateOrEmpty(r.FormValue("claim_date"))
	if claimDate != "" {
		if _, err := time.Parse("2006-01-02", claimDate); err != nil {
			fail(w, "理賠日期格式錯誤")
			return
		}
	}
	amount, err := parseClaimAmount(r.FormValue("claim_amount"))
	if err != nil || amount < 0 {
		fail(w, "理賠金額必須為不小於 0 的數字")
		return
	}
	res, err := db.DB.Exec(`UPDATE work_orders SET claim_date=?, claim_amount=?, claim_note=?,
		updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		claimDate, amount, strings.TrimSpace(r.FormValue("claim_note")), id)
	if err != nil {
		fail(w, "更新理賠資料失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/work-orders/"+strconv.FormatInt(id, 10), "理賠資料已更新")
}


func ShowWorkOrder(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	allowed, err := canViewWorkOrder(user, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			log.Printf("檢查工單權限失敗（工單 #%d）: %v", id, err)
			fail(w, "無法確認工單權限")
		}
		return
	}
	if !allowed {
		http.Error(w, "沒有查看此工單的權限", http.StatusForbidden)
		return
	}

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

	// 追加維修授權僅限櫃檯以上（POST /work-orders/{id}/approval-request 是
	// CounterUp），且已結帳的工單不能再追加。兩者都符合才顯示表單。
	canCreateApproval := canEdit && (user.Role == "owner" || user.Role == "counter")

	// 技師名稱對照表（供項目表格顯示）。
	techNames := map[int64]string{}
	for _, it := range items {
		if it.PrimaryTechnicianID > 0 {
			if _, ok := techNames[it.PrimaryTechnicianID]; !ok {
				techNames[it.PrimaryTechnicianID] = techName(it.PrimaryTechnicianID)
			}
		}
		if it.AssistantTechID > 0 {
			if _, ok := techNames[it.AssistantTechID]; !ok {
				techNames[it.AssistantTechID] = techName(it.AssistantTechID)
			}
		}
	}

	// T2.6：載入工單的 SOP 檢查紀錄與已掛模板。
	sopRecords := LoadSOPRecords(id)
	attachedTemplates := LoadAttachedTemplates(id)
	sopTemplateOpts := SOPTemplateOptions(attachedTemplates)

	pages.ShowWorkOrderPage(wo, items, itemTotal,
		workOrderStatusLabel, statusPill, approvalLabel, approvalPill,
		techNames, canEdit, flashText(r),
		sopRecords, sopTemplateOpts, attachedTemplates,
		canCreateApproval, pendingApprovalRows(id), user).Render(r.Context(), w)
}


func countStatus(id int64) string {
	var s string
	if err := db.DB.QueryRow("SELECT status FROM active_work_orders WHERE id = ?", id).Scan(&s); err != nil {
		return ""
	}
	return s
}

// requireOrderMutable 擋下已結帳工單的內容變更。
// 已結帳代表已收款、已扣料、已開立收據，此時改內容會讓金額與實物對不上。

func requireOrderMutable(woID int64) error {
	if WorkOrderStatus(countStatus(woID)) == WorkOrderStatusPaid {
		return errOrderLocked
	}
	return nil
}

var errOrderLocked = errors.New("此工單已結帳，內容不可再修改")


func branchLabel(code string) string {
	return lookupLabel("branch", strings.ToUpper(strings.TrimSpace(code)))
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

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

