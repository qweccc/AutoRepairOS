package handler

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

var errAPPaymentRejected = errors.New("付款金額超過應付餘額或帳款已結清")

func createAPPayable(tx db.TxExecutor, sourceType string, sourceID, supplierID int64, amount float64, issueDate string) error {
	if !isAPPayableSource(sourceType) {
		return fmt.Errorf("unsupported AP source type %q", sourceType)
	}
	if sourceID <= 0 || supplierID <= 0 {
		return errors.New("AP source and supplier IDs must be positive")
	}
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return errors.New("AP amount must be a finite non-negative value")
	}
	var terms string
	if err := tx.QueryRow(`SELECT COALESCE(payment_terms,'') FROM suppliers WHERE id=?`, supplierID).Scan(&terms); err != nil {
		return fmt.Errorf("查詢供應商付款條件: %w", err)
	}
	dueDate, err := apDueDate(terms, issueDate)
	if err != nil {
		return err
	}
	amount = roundAPMoney(amount)
	_, err = tx.Exec(`INSERT INTO ap_payables
		(source_type, source_id, supplier_id, amount, paid_amount, balance, due_date, status)
		VALUES (?,?,?,?,0,?,?,'open')
		ON CONFLICT(source_type, source_id) DO NOTHING`,
		sourceType, sourceID, supplierID, amount, amount, dueDate)
	return err
}

// isSettledStatus checks if an AP status is settled (paid or void).
func isSettledStatus(status string) bool {
	return status == "paid" || status == "void"
}

// isAPPayableSource checks if the source type creates AP payables.
func isAPPayableSource(sourceType string) bool {
	return sourceType == "purchase_receive" || sourceType == "subcontract"
}

func apDueDate(terms, issueDate string) (string, error) {
	issued, err := time.Parse("2006-01-02", issueDate)
	if err != nil {
		return "", fmt.Errorf("無效的應付成立日期 %q", issueDate)
	}
	switch terms {
	case "monthly":
		nextMonth := time.Date(issued.Year(), issued.Month()+1, 1, 0, 0, 0, 0, issued.Location())
		return nextMonth.AddDate(0, 0, -1).Format("2006-01-02"), nil
	case "check30":
		return issued.AddDate(0, 0, 30).Format("2006-01-02"), nil
	case "check60":
		return issued.AddDate(0, 0, 60).Format("2006-01-02"), nil
	default:
		return issued.Format("2006-01-02"), nil
	}
}

func roundAPMoney(amount float64) float64 {
	return math.Round(amount*100) / 100
}

func applyAPPayment(tx db.TxExecutor, payableID int64, amount float64, paidAt string) error {
	amount = roundAPMoney(amount)
	if payableID <= 0 || amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return errAPPaymentRejected
	}
	if _, err := time.Parse("2006-01-02", paidAt); err != nil {
		return errors.New("付款日期格式錯誤")
	}
	res, err := tx.Exec(`UPDATE ap_payables
		SET paid_amount=round(paid_amount+?,2),
			balance=round(amount-(paid_amount+?),2),
			status=CASE WHEN round(amount-(paid_amount+?),2)<=0 THEN 'paid' ELSE 'partial' END,
			paid_at=CASE WHEN round(amount-(paid_amount+?),2)<=0 THEN ? ELSE NULL END
		WHERE id=? AND status IN ('open','partial')
			AND round(balance,2)>=?`,
		amount, amount, amount, amount, paidAt, payableID, amount)
	if err != nil {
		return err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errAPPaymentRejected
	}
	if _, err := tx.Exec(`INSERT INTO ap_payments (payable_id, amount, paid_at) VALUES (?,?,?)`,
		payableID, amount, paidAt); err != nil {
		return err
	}
	return nil
}

func apAgeBucket(dueDate, createdAt, asOf string) (string, int, error) {
	date := strings.TrimSpace(dueDate)
	if date == "" {
		date = strings.TrimSpace(createdAt)
		if len(date) > 10 {
			date = date[:10]
		}
	}
	due, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", 0, fmt.Errorf("應付到期日無效 %q: %w", date, err)
	}
	base, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return "", 0, fmt.Errorf("帳齡基準日無效 %q: %w", asOf, err)
	}
	days := int(base.Sub(due).Hours() / 24)
	if days < 0 {
		days = 0
	}
	switch {
	case days <= 30:
		return "0–30", days, nil
	case days <= 60:
		return "31–60", days, nil
	case days <= 90:
		return "61–90", days, nil
	default:
		return "90+", days, nil
	}
}

type apPayableRow struct {
	ID           int64
	SourceLabel  string
	SupplierName string
	Amount       float64
	PaidAmount   float64
	Balance      float64
	DueDate      string
	CreatedAt    string
	PaidAt       string
	Status       string
	StatusLabel  string
	StatusPill   string
	Aging        string
	AgeDays      int
	Payments     []pages.APPaymentRow
}

type apSupplierOption struct {
	ID   int64
	Name string
}

type apBucketSummary struct {
	Name  string
	Count int
	Total float64
}

var apBuckets = []string{"0–30", "31–60", "61–90", "90+"}

func apStatusPresentation(status string) (string, string) {
	switch status {
	case "partial":
		return "部分付款", "p-progress"
	case "paid":
		return "已付清", "p-completed"
	case "void":
		return "已沖銷", "p-apprno"
	default:
		return "未付款", "p-pending"
	}
}

func apSourceLabel(sourceType string, sourceID int64) string {
	name := "外包"
	if sourceType == "purchase_receive" {
		name = "採購進貨"
	}
	return name + " #" + strconv.FormatInt(sourceID, 10)
}

func loadAPRows(supplierID int64, status string, from, to string, statement bool) ([]apPayableRow, error) {
	query := `SELECT p.id, p.source_type, p.source_id, COALESCE(s.name,''),
		p.amount, p.paid_amount, p.balance, COALESCE(p.due_date,''), p.created_at,
		COALESCE(p.paid_at,''), p.status
		FROM ap_payables p LEFT JOIN suppliers s ON s.id=p.supplier_id WHERE 1=1`
	args := make([]interface{}, 0, 4)
	if supplierID > 0 {
		query += ` AND p.supplier_id=?`
		args = append(args, supplierID)
	}
	if statement {
		query += ` AND date(p.created_at) BETWEEN ? AND ?`
		args = append(args, from, to)
	} else if status == "" {
		query += ` AND p.status IN ('open','partial')`
	} else if status != "all" {
		query += ` AND p.status=?`
		args = append(args, status)
	}
	query += ` ORDER BY p.created_at DESC, p.id DESC`
	if !statement {
		query += ` LIMIT 500`
	}
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]apPayableRow, 0)
	for rows.Next() {
		var row apPayableRow
		var sourceType string
		var sourceID int64
		if err := rows.Scan(&row.ID, &sourceType, &sourceID, &row.SupplierName,
			&row.Amount, &row.PaidAmount, &row.Balance, &row.DueDate, &row.CreatedAt,
			&row.PaidAt, &row.Status); err != nil {
			return nil, err
		}
		row.SourceLabel = apSourceLabel(sourceType, sourceID)
		row.StatusLabel, row.StatusPill = apStatusPresentation(row.Status)
		if !statement && !isSettledStatus(row.Status) {
			row.Aging, row.AgeDays, err = apAgeBucket(row.DueDate, row.CreatedAt, today())
			if err != nil {
				return nil, err
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := loadAPPaymentRows(out); err != nil {
		return nil, err
	}
	return out, nil
}

func loadAPPaymentRows(payables []apPayableRow) error {
	if len(payables) == 0 {
		return nil
	}
	placeholders := make([]string, len(payables))
	args := make([]interface{}, len(payables))
	byID := make(map[int64]int, len(payables))
	for i, payable := range payables {
		placeholders[i] = "?"
		args[i] = payable.ID
		byID[payable.ID] = i
	}
	rows, err := db.DB.Query(`SELECT payable_id, amount, paid_at FROM ap_payments
		WHERE payable_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY paid_at, id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var payableID int64
		var payment pages.APPaymentRow
		if err := rows.Scan(&payableID, &payment.Amount, &payment.PaidAt); err != nil {
			return err
		}
		i, ok := byID[payableID]
		if ok {
			payables[i].Payments = append(payables[i].Payments, payment)
		}
	}
	return rows.Err()
}

func loadAPSuppliers() ([]apSupplierOption, error) {
	rows, err := db.DB.Query(`SELECT id, name FROM suppliers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]apSupplierOption, 0)
	for rows.Next() {
		var supplier apSupplierOption
		if err := rows.Scan(&supplier.ID, &supplier.Name); err != nil {
			return nil, err
		}
		out = append(out, supplier)
	}
	return out, rows.Err()
}

func summarizeAPAging(supplierID int64, asOf string) ([]apBucketSummary, error) {
	summary := make([]apBucketSummary, len(apBuckets))
	index := make(map[string]int, len(apBuckets))
	for i, name := range apBuckets {
		summary[i].Name = name
		index[name] = i
	}
	query := `SELECT due_date, created_at, balance FROM ap_payables
		WHERE status IN ('open','partial')`
	args := []interface{}{}
	if supplierID > 0 {
		query += ` AND supplier_id=?`
		args = append(args, supplierID)
	}
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var dueDate, createdAt string
		var balance float64
		if err := rows.Scan(&dueDate, &createdAt, &balance); err != nil {
			return nil, err
		}
		bucket, _, err := apAgeBucket(dueDate, createdAt, asOf)
		if err != nil {
			return nil, err
		}
		i := index[bucket]
		summary[i].Count++
		summary[i].Total += balance
	}
	return summary, rows.Err()
}

func APHome(w http.ResponseWriter, r *http.Request, user model.User) {
	supplierID := mustInt(r.URL.Query().Get("supplier_id"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	switch status {
	case "", "all", "open", "partial", "paid", "void":
	default:
		status = ""
	}
	rows, err := loadAPRows(supplierID, status, "", "", false)
	if err != nil {
		http.Error(w, "查詢應付帳款失敗", http.StatusInternalServerError)
		return
	}
	suppliers, err := loadAPSuppliers()
	if err != nil {
		http.Error(w, "查詢供應商失敗", http.StatusInternalServerError)
		return
	}
	aging, err := summarizeAPAging(supplierID, today())
	if err != nil {
		http.Error(w, "計算帳齡失敗", http.StatusInternalServerError)
		return
	}
	pageRows := make([]pages.APPayableRow, 0, len(rows))
	for _, row := range rows {
		pageRows = append(pageRows, pages.APPayableRow{
			ID: row.ID, SourceLabel: row.SourceLabel, SupplierName: row.SupplierName,
			Amount: row.Amount, PaidAmount: row.PaidAmount, Balance: row.Balance,
			DueDate: row.DueDate, CreatedAt: row.CreatedAt, PaidAt: row.PaidAt,
			Status: row.Status, StatusLabel: row.StatusLabel, StatusPill: row.StatusPill,
			Aging: row.Aging, AgeDays: row.AgeDays, Payments: row.Payments,
		})
	}
	pageAging := make([]pages.APAgingSummary, 0, len(aging))
	for _, bucket := range aging {
		pageAging = append(pageAging, pages.APAgingSummary{Name: bucket.Name, Count: bucket.Count, Total: bucket.Total})
	}
	pageSuppliers := make([]pages.APSupplierOption, 0, len(suppliers))
	for _, supplier := range suppliers {
		pageSuppliers = append(pageSuppliers, pages.APSupplierOption{ID: supplier.ID, Name: supplier.Name})
	}
	pages.APHomePage(pageRows, pageAging, pageSuppliers, supplierID, status, today(),
		user).Render(r.Context(), w)
}
