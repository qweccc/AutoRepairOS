package handler

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

func RecordAPPayment(w http.ResponseWriter, r *http.Request) {
	payableID := mustInt(r.PathValue("id"))
	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		formError(w, r, "登記付款", "/ap", "付款金額格式錯誤")
		return
	}
	paidAt := strings.TrimSpace(r.FormValue("paid_at"))
	if paidAt == "" {
		paidAt = today()
	}
	tx, err := db.BeginImmediate()
	if err != nil {
		fail(w, "登記付款失敗")
		return
	}
	defer tx.Rollback()
	if err := applyAPPayment(tx, payableID, amount, paidAt); err != nil {
		if errors.Is(err, errAPPaymentRejected) {
			fail(w, err.Error())
		} else {
			log.Printf("登記應付付款失敗: %v", err)
			fail(w, "登記付款失敗")
		}
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("提交應付付款失敗: %v", err)
		fail(w, "登記付款失敗")
		return
	}
	redirectMsg(w, r, "/ap", "付款已登記")
}

func VoidAPPayable(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	res, err := db.DB.Exec(`UPDATE ap_payables SET status='void'
		WHERE id=? AND status IN ('open','partial') AND paid_amount=0`, id)
	if err != nil {
		fail(w, "沖銷應付帳款失敗")
		return
	}
	changed, _ := res.RowsAffected()
	if changed == 0 {
		fail(w, "僅可沖銷尚未付款的應付帳款")
		return
	}
	redirectMsg(w, r, "/ap", "應付帳款已沖銷")
}

func apStatementPeriod(r *http.Request) (string, string, error) {
	now := time.Now()
	defaultFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	defaultTo := defaultFrom.AddDate(0, 1, -1)
	from, to := strings.TrimSpace(r.URL.Query().Get("from")), strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" {
		from = defaultFrom.Format("2006-01-02")
	}
	if to == "" {
		to = defaultTo.Format("2006-01-02")
	}
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return "", "", errors.New("起始日期格式須為 YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil || end.Before(start) {
		return "", "", errors.New("結束日期無效或早於起始日期")
	}
	return start.Format("2006-01-02"), end.Format("2006-01-02"), nil
}

func APStatement(w http.ResponseWriter, r *http.Request, user model.User) {
	supplierID := mustInt(r.URL.Query().Get("supplier_id"))
	from, to, err := apStatementPeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	suppliers, err := loadAPSuppliers()
	if err != nil {
		http.Error(w, "查詢供應商失敗", http.StatusInternalServerError)
		return
	}
	statementRows, err := loadAPRows(supplierID, "all", from, to, true)
	if err != nil {
		http.Error(w, "查詢供應商對帳單失敗", http.StatusInternalServerError)
		return
	}
	name := ""
	for _, supplier := range suppliers {
		if supplier.ID == supplierID {
			name = supplier.Name
			break
		}
	}
	if supplierID <= 0 || name == "" {
		pages.APStatementPage(nil, "", from, to, 0, 0, 0, convertAPSuppliers(suppliers),
			supplierID, "",
			user).Render(r.Context(), w)
		return
	}
	var amount, paid, balance float64
	rows := make([]pages.APPayableRow, 0, len(statementRows))
	for _, row := range statementRows {
		amount += row.Amount
		paid += row.PaidAmount
		balance += row.Balance
		rows = append(rows, pages.APPayableRow{
			ID: row.ID, SourceLabel: row.SourceLabel, SupplierName: row.SupplierName,
			Amount: row.Amount, PaidAmount: row.PaidAmount, Balance: row.Balance,
			DueDate: row.DueDate, CreatedAt: row.CreatedAt, PaidAt: row.PaidAt,
			Status: row.Status, StatusLabel: row.StatusLabel, StatusPill: row.StatusPill,
		})
	}
	pages.APStatementPage(rows, name, from, to, amount, paid, balance,
		convertAPSuppliers(suppliers), supplierID, apStatementCSVURL(supplierID, from, to),
		user).Render(r.Context(), w)
}

func convertAPSuppliers(suppliers []apSupplierOption) []pages.APSupplierOption {
	out := make([]pages.APSupplierOption, 0, len(suppliers))
	for _, supplier := range suppliers {
		out = append(out, pages.APSupplierOption{ID: supplier.ID, Name: supplier.Name})
	}
	return out
}

func apStatementCSVURL(supplierID int64, from, to string) string {
	values := url.Values{}
	values.Set("supplier_id", strconv.FormatInt(supplierID, 10))
	values.Set("from", from)
	values.Set("to", to)
	return "/ap/statement.csv?" + values.Encode()
}

func APStatementCSV(w http.ResponseWriter, r *http.Request) {
	supplierID := mustInt(r.URL.Query().Get("supplier_id"))
	if supplierID <= 0 {
		http.Error(w, "請選擇供應商", http.StatusBadRequest)
		return
	}
	from, to, err := apStatementPeriod(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var supplierName string
	if err := db.DB.QueryRow(`SELECT name FROM suppliers WHERE id=?`, supplierID).Scan(&supplierName); err != nil {
		http.NotFound(w, r)
		return
	}
	rows, err := loadAPRows(supplierID, "all", from, to, true)
	if err != nil {
		http.Error(w, "匯出對帳單失敗", http.StatusInternalServerError)
		return
	}
	var output bytes.Buffer
	output.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(&output)
	if err := writer.Write([]string{"供應商", "期間起", "期間迄", "成立日", "來源單據", "應付金額", "已付金額", "餘額", "到期日", "狀態"}); err != nil {
		http.Error(w, "產生 CSV 失敗", http.StatusInternalServerError)
		return
	}
	for _, row := range rows {
		if err := writer.Write([]string{
			apCSVText(supplierName), from, to, apCSVText(row.CreatedAt), apCSVText(row.SourceLabel),
			strconv.FormatFloat(row.Amount, 'f', 2, 64), strconv.FormatFloat(row.PaidAmount, 'f', 2, 64),
			strconv.FormatFloat(row.Balance, 'f', 2, 64), apCSVText(row.DueDate), apCSVText(row.StatusLabel),
		}); err != nil {
			http.Error(w, "產生 CSV 失敗", http.StatusInternalServerError)
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		http.Error(w, "產生 CSV 失敗", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ap_statement_%d_%s_%s.csv"`, supplierID, from, to))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}

func apCSVText(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
