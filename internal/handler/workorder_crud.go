package handler

import (
	"errors"
	"net/http"
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

// SearchWorkOrdersAPI 工單即時搜尋 API（HTMX 用）。

func CreateWorkOrder(w http.ResponseWriter, r *http.Request) {
	vehicleID := mustInt(r.FormValue("vehicle_id"))
	mileage := atoiDefault(r.FormValue("current_mileage"), -1)

	if vehicleID == 0 {
		user, _ := currentUser(r)
		w.WriteHeader(http.StatusBadRequest)
		data := buildNewWorkOrderPageData(r, user, 0, "", 0, "", 0, "", "請選擇車輛", strings.TrimSpace(r.FormValue("customer_complaint")), strings.TrimSpace(r.FormValue("current_mileage")))
		_ = pages.NewWorkOrderPage(data, user).Render(r.Context(), w)
		return
	}
	if mileage < 0 {
		user, _ := currentUser(r)
		w.WriteHeader(http.StatusBadRequest)
		data := buildNewWorkOrderPageData(r, user, vehicleID, "", 0, "", 0, "", "請填寫進廠里程", strings.TrimSpace(r.FormValue("customer_complaint")), strings.TrimSpace(r.FormValue("current_mileage")))
		_ = pages.NewWorkOrderPage(data, user).Render(r.Context(), w)
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
		formError(w, r, "新增工單", "/work-orders/new", "請選擇保險公司")
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
			formError(w, r, "新增工單", "/work-orders/new", "理賠日期格式錯誤")
			return
		}
	}
	claimAmount, amountErr := parseClaimAmount(r.FormValue("claim_amount"))
	if amountErr != nil || claimAmount < 0 {
		formError(w, r, "新增工單", "/work-orders/new", "理賠金額必須為不小於 0 的數字")
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
			formError(w, r, "編輯理賠資料", "/work-orders/"+strconv.FormatInt(id, 10), "理賠日期格式錯誤")
			return
		}
	}
	amount, err := parseClaimAmount(r.FormValue("claim_amount"))
	if err != nil || amount < 0 {
		formError(w, r, "編輯理賠資料", "/work-orders/"+strconv.FormatInt(id, 10), "理賠金額必須為不小於 0 的數字")
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
