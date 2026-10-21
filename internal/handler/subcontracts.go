package handler

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

// 外包加工管理（SRS FR-SUB-01/02）。
//
// 外包工項（item_type='subcontract'）記錄協力廠商、成本、收費與進度。
// 估價單上的外包與實際分開追蹤，方便掌握外包狀態與毛利。

const subcontractCols = `sc.id, sc.work_order_id, COALESCE(sc.supplier_id,0),
	COALESCE(sup.name,''), sc.description, COALESCE(sc.cost_amount,0),
	COALESCE(sc.charge_amount,0), sc.status, COALESCE(sc.expected_date,''),
	COALESCE(sc.completed_date,''), COALESCE(sc.notes,''), sc.created_at`

func scanSubcontract(rows interface{ Scan(...interface{}) error }) (model.Subcontract, error) {
	var s model.Subcontract
	var createdAt string
	err := rows.Scan(&s.ID, &s.WorkOrderID, &s.SupplierID, &s.SupplierName,
		&s.Description, &s.CostAmount, &s.ChargeAmount, &s.Status,
		&s.ExpectedDate, &s.CompletedDate, &s.Notes, &createdAt)
	s.CreatedAt = parseFlexTime(createdAt)
	return s, err
}

var subcontractStatusLabel = map[string]string{
	"pending":     "待處理",
	"in_progress": "進行中",
	"completed":   "已完成",
	"cancelled":   "已取消",
}

var subcontractStatusPill = map[string]string{
	"pending":     "p-pending",
	"in_progress": "p-progress",
	"completed":   "p-completed",
	"cancelled":   "p-apprno",
}

func ListSubcontracts(w http.ResponseWriter, r *http.Request, user model.User) {
	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	status := strings.TrimSpace(qv.Get("status"))
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (sc.description LIKE ? OR COALESCE(sup.name,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if status != "" {
		where += ` AND sc.status = ?`
		args = append(args, status)
	}

	total := countRows(`SELECT COUNT(1) FROM active_subcontracts sc LEFT JOIN active_suppliers sup ON sup.id=sc.supplier_id`+where, args...)

	query := `SELECT ` + subcontractCols + `
		FROM active_subcontracts sc LEFT JOIN active_suppliers sup ON sup.id=sc.supplier_id` + where +
		` ORDER BY sc.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢外包失敗")
		return
	}
	defer rows.Close()

	var list []model.Subcontract
	for rows.Next() {
		s, err := scanSubcontract(rows)
		if err != nil {
			continue
		}
		list = append(list, s)
	}

	var subRows []pages.SubcontractRow
	for _, s := range list {
		subRows = append(subRows, pages.SubcontractRow{
			ID:           s.ID,
			WorkOrderID:  s.WorkOrderID,
			SupplierName: s.SupplierName,
			Description:  s.Description,
			CostAmount:   s.CostAmount,
			ChargeAmount: s.ChargeAmount,
			Status:       s.Status,
			StatusLabel:  subcontractStatusLabel[s.Status],
			StatusPill:   subcontractStatusPill[s.Status],
		})
	}

	pages.ListSubcontractsPage(subRows, total, page, size, q, status,
		supplierOptionsSel(0),
		user).Render(r.Context(), w)
}

// SearchSubcontractsAPI 外包即時搜尋 API（HTMX 用）。
func SearchSubcontractsAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where := ` WHERE 1=1`
	var args []interface{}
	if q != "" {
		where += ` AND (sc.description LIKE ? OR COALESCE(sup.name,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	rows, err := db.DB.Query(`SELECT `+subcontractCols+`
		FROM active_subcontracts sc LEFT JOIN active_suppliers sup ON sup.id=sc.supplier_id`+where+
		` ORDER BY sc.id DESC LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.SubcontractRow
	for rows.Next() {
		s, err := scanSubcontract(rows)
		if err != nil {
			continue
		}
		result = append(result, pages.SubcontractRow{
			ID:           s.ID,
			WorkOrderID:  s.WorkOrderID,
			SupplierName: s.SupplierName,
			Description:  s.Description,
			CostAmount:   s.CostAmount,
			ChargeAmount: s.ChargeAmount,
			Status:       s.Status,
			StatusLabel:  subcontractStatusLabel[s.Status],
			StatusPill:   subcontractStatusPill[s.Status],
		})
	}
	pages.SubcontractSearchRows(result).Render(r.Context(), w)
}

// CreateSubcontract 新增外包工項（SRS FR-SUB-01）。
func CreateSubcontract(w http.ResponseWriter, r *http.Request) {
	workOrderID := mustInt(r.FormValue("work_order_id"))
	if workOrderID == 0 {
		fail(w, "工單 ID 為必填")
		return
	}
	if countRows("SELECT COUNT(1) FROM active_work_orders WHERE id = ?", workOrderID) == 0 {
		fail(w, "工單不存在")
		return
	}
	description := strings.TrimSpace(r.FormValue("description"))
	if description == "" {
		fail(w, "加工內容為必填")
		return
	}

	tx, err := db.BeginImmediate()
	if err != nil {
		fail(w, "新增外包失敗")
		return
	}
	defer tx.Rollback()
	supplierID := mustInt(r.FormValue("supplier_id"))
	costAmount := atofDefault(r.FormValue("cost_amount"), 0)
	status := subcontractStatusArg(r.FormValue("status"))
	res, err := tx.Exec(`INSERT INTO subcontracts
		(work_order_id, supplier_id, description, cost_amount, charge_amount, status, expected_date, notes)
		VALUES (?,?,?,?,?,?,?,?)`,
		workOrderID, supplierIDArg(r.FormValue("supplier_id")), description,
		costAmount, atofDefault(r.FormValue("charge_amount"), 0),
		status,
		nullStr(r.FormValue("expected_date")), nullStr(r.FormValue("notes")))
	if err != nil {
		fail(w, "新增外包失敗")
		return
	}
	subcontractID, err := res.LastInsertId()
	if err != nil {
		fail(w, "新增外包失敗")
		return
	}
	if supplierID > 0 && costAmount > 0 && status != "cancelled" {
		if err := createAPPayable(tx, "subcontract", subcontractID, supplierID, costAmount, today()); err != nil {
			log.Printf("建立外包應付帳款失敗: %v", err)
			fail(w, "建立外包應付帳款失敗")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, "新增外包失敗")
		return
	}
	redirectMsg(w, r, "/subcontracts", "外包工項已新增")
}

// EditSubcontractForm 編輯外包表單（SRS FR-SUB-02）。
func EditSubcontractForm(w http.ResponseWriter, r *http.Request, user model.User) {
	id := mustInt(r.PathValue("id"))
	var s model.Subcontract
	var createdAt string
	err := db.DB.QueryRow(`SELECT `+subcontractCols+` FROM active_subcontracts sc
		LEFT JOIN active_suppliers sup ON sup.id=sc.supplier_id WHERE sc.id = ?`, id).
		Scan(&s.ID, &s.WorkOrderID, &s.SupplierID, &s.SupplierName,
			&s.Description, &s.CostAmount, &s.ChargeAmount, &s.Status,
			&s.ExpectedDate, &s.CompletedDate, &s.Notes, &createdAt)
	s.CreatedAt = parseFlexTime(createdAt)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body := `
    <div class="card">
      <h2>編輯外包 #` + strconv.FormatInt(s.ID, 10) + `</h2>
      <form class="frm" method="POST" action="/subcontracts/` + strconv.FormatInt(s.ID, 10) + `">
        <label>工單 ID<input type="number" name="work_order_id" value="` + strconv.FormatInt(s.WorkOrderID, 10) + `" required></label>
        <label>協力廠商
          <select name="supplier_id">` + supplierOptionsSel(s.SupplierID) + `</select>
        </label>
        <label class="full">加工內容<input name="description" value="` + htmlEsc(s.Description) + `" required></label>
        <label>成本金額<input type="number" step="0.01" name="cost_amount" value="` + money(s.CostAmount) + `"></label>
        <label>收費金額<input type="number" step="0.01" name="charge_amount" value="` + money(s.ChargeAmount) + `"></label>
        <label>預計完成日<input type="date" name="expected_date" value="` + htmlEsc(s.ExpectedDate) + `"></label>
        <label>狀態
          <select name="status">
            <option value="pending"` + selIf(s.Status == "pending") + `>待處理</option>
            <option value="in_progress"` + selIf(s.Status == "in_progress") + `>進行中</option>
            <option value="completed"` + selIf(s.Status == "completed") + `>已完成</option>
            <option value="cancelled"` + selIf(s.Status == "cancelled") + `>已取消</option>
          </select>
        </label>
        <label class="full">備註<textarea name="notes">` + htmlEsc(s.Notes) + `</textarea></label>
        <div class="full"><button class="btn btn-p" type="submit">儲存</button> <a class="btn btn-s" href="/subcontracts">取消</a></div>
      </form>
    </div>`
	renderPage(w, "編輯外包", "/subcontracts", user.Username, user.Role, body)
}

// UpdateSubcontract 更新外包工項。
func UpdateSubcontract(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	workOrderID := mustInt(r.FormValue("work_order_id"))
	description := strings.TrimSpace(r.FormValue("description"))
	if workOrderID == 0 || description == "" {
		fail(w, "工單 ID 與加工內容為必填")
		return
	}
	status := subcontractStatusArg(r.FormValue("status"))
	completed := nullStr(r.FormValue("completed_date"))
	if status == "completed" && completed == "" {
		completed = today()
	}
	supplierID := mustInt(r.FormValue("supplier_id"))
	costAmount := atofDefault(r.FormValue("cost_amount"), 0)
	tx, err := db.BeginImmediate()
	if err != nil {
		fail(w, "更新外包失敗")
		return
	}
	defer tx.Rollback()
	var payable model.APPayable
	err = tx.QueryRow(`SELECT supplier_id, amount FROM ap_payables
		WHERE source_type='subcontract' AND source_id=?`, id).Scan(&payable.SupplierID, &payable.Amount)
	hasPayable := err == nil
	if err != nil && err != sql.ErrNoRows {
		fail(w, "讀取外包應付帳款失敗")
		return
	}
	if hasPayable && (supplierID != payable.SupplierID || roundAPMoney(costAmount) != roundAPMoney(payable.Amount)) {
		fail(w, "外包已建立應付帳款，供應商與成本不可修改")
		return
	}
	res, err := tx.Exec(`UPDATE subcontracts SET work_order_id=?, supplier_id=?,
		description=?, cost_amount=?, charge_amount=?, status=?, expected_date=?,
		completed_date=?, notes=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		workOrderID, supplierIDArg(r.FormValue("supplier_id")), description,
		costAmount, atofDefault(r.FormValue("charge_amount"), 0),
		status, nullStr(r.FormValue("expected_date")), completed, nullStr(r.FormValue("notes")), id)
	if err != nil {
		fail(w, "更新外包失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	if !hasPayable && supplierID > 0 && costAmount > 0 && status != "cancelled" {
		if err := createAPPayable(tx, "subcontract", id, supplierID, costAmount, today()); err != nil {
			log.Printf("建立外包應付帳款失敗: %v", err)
			fail(w, "建立外包應付帳款失敗")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		fail(w, "更新外包失敗")
		return
	}
	redirectMsg(w, r, "/subcontracts", "外包已更新")
}

// DeleteSubcontract 刪除外包工項。
func DeleteSubcontract(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	res, err := db.DB.Exec("UPDATE subcontracts SET deleted_at = datetime('now') WHERE id = ? AND deleted_at IS NULL", id)
	if err != nil {
		fail(w, "刪除失敗")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.NotFound(w, r)
		return
	}
	redirectMsg(w, r, "/subcontracts", "外包已刪除")
}

func supplierIDArg(v string) interface{} {
	n := mustInt(v)
	if n <= 0 {
		return nil
	}
	return n
}

func subcontractStatusArg(v string) string {
	switch v {
	case "pending", "in_progress", "completed", "cancelled":
		return v
	default:
		return "pending"
	}
}
