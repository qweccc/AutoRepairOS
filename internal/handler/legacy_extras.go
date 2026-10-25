package handler

import (
	"net/http"
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
		(SELECT COUNT(1) FROM active_maintenance_template_items i WHERE i.template_id = t.id)
		FROM active_maintenance_templates t`+where+` ORDER BY t.code`, args...)
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

	var listRows []pages.MaintenanceTemplateListRow
	for _, t := range list {
		listRows = append(listRows, pages.MaintenanceTemplateListRow{
			ID: t.ID, Code: t.Code, Name: t.Name, ItemCount: t.ItemCount,
		})
	}

	// 指定 ?id= 時顯示該套餐的項目明細
	var detail *pages.MaintenanceTemplateDetail
	if id := mustInt(r.URL.Query().Get("id")); id > 0 {
		detail = templateDetailData(id)
	}
	pages.ListMaintenanceTemplatesPage(pages.MaintenanceTemplateListData{
		Rows: listRows, Query: q, Detail: detail, User: user, Flash: r.URL.Query().Get("msg"),
	}).Render(r.Context(), w)
}

func templateDetailData(id int64) *pages.MaintenanceTemplateDetail {
	var name, code string
	if err := db.DB.QueryRow("SELECT code, name FROM active_maintenance_templates WHERE id = ?", id).
		Scan(&code, &name); err != nil {
		return nil
	}
	rows, err := db.DB.Query(`SELECT COALESCE(i.part_number,''),
		COALESCE(inv.name, i.description, ''), i.quantity, i.unit_price
		FROM active_maintenance_template_items i
		LEFT JOIN active_inventory_items inv ON inv.part_number = i.part_number
		WHERE i.template_id = ? ORDER BY i.sequence_no, i.id`, id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	detail := &pages.MaintenanceTemplateDetail{Code: code, Name: name}
	for rows.Next() {
		var part, desc string
		var qty, price float64
		if rows.Scan(&part, &desc, &qty, &price) != nil {
			continue
		}
		detail.Items = append(detail.Items, pages.MaintenanceTemplateEditItem{
			PartNumber: part, Description: desc, Quantity: money(qty), Price: money(price),
		})
	}
	return detail
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

	pages.ListExpensesPage(expRows, total, page, size, q, sumIn, sumOut, user).Render(r.Context(), w)
}

// SearchExpensesAPI 支出即時搜尋 API（HTMX 用）。
func SearchExpensesAPI(w http.ResponseWriter, r *http.Request, user model.User) {
	// 財務數字屬店內機密：搜尋 API 與列表頁一致，技師不可看。
	if !canSeeMoney(user) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
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
