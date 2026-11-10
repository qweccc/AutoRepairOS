package handler

import (
	"net/http"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/pages"
)

const receivableCols = `r.id, r.voucher_no, r.customer_id, COALESCE(c.name,''),
	COALESCE(r.vehicle_id,0), COALESCE(v.plate_number,''),
	COALESCE(r.work_order_id,0),
	COALESCE(r.pay_date,''), r.detail_total, r.real_total,
	r.discount, r.tax, r.pay_total,
	r.cash, r.card, r.check_amount, r.transfer_amount,
	COALESCE(r.transfer_reference,''),
	COALESCE(r.card_no,''), COALESCE(r.check_no,''), COALESCE(r.check_date,''),
	COALESCE(r.bank,''), COALESCE(r.notes,''), r.voided, r.source`

const receivableFrom = ` FROM receivables r
	JOIN active_customers c ON c.id = r.customer_id
	LEFT JOIN active_vehicles v ON v.id = r.vehicle_id`

type receivable struct {
	ID                int64
	VoucherNo         string
	CustomerID        int64
	CustomerName      string
	VehicleID         int64
	PlateNumber       string
	WorkOrderID       int64
	PayDate           string
	DetailTotal       float64
	RealTotal         float64
	Discount          float64
	Tax               float64
	PayTotal          float64
	Cash              float64
	Card              float64
	CheckAmount       float64
	TransferAmount    float64
	TransferReference string
	CardNo            string
	CheckNo           string
	CheckDate         string
	Bank              string
	Notes             string
	Voided            int
	Source            string
}

func scanReceivable(rows interface{ Scan(...interface{}) error }) (receivable, error) {
	var r receivable
	err := rows.Scan(&r.ID, &r.VoucherNo, &r.CustomerID, &r.CustomerName,
		&r.VehicleID, &r.PlateNumber, &r.WorkOrderID,
		&r.PayDate, &r.DetailTotal, &r.RealTotal,
		&r.Discount, &r.Tax, &r.PayTotal,
		&r.Cash, &r.Card, &r.CheckAmount, &r.TransferAmount, &r.TransferReference,
		&r.CardNo, &r.CheckNo, &r.CheckDate,
		&r.Bank, &r.Notes, &r.Voided, &r.Source)
	return r, err
}

// payMethod 判斷這筆收款的主要付款方式，用於列表標籤。
//
// 同一筆可同時使用多種收款方式，因此這裡是顯示用的主要分類。
// 因此這裡是「顯示用」的分類，不是互斥的真實欄位。
func payMethod(r receivable) (string, string) {
	hasCash := r.Cash > 0
	hasCard := r.Card > 0
	hasCheck := r.CheckAmount > 0
	hasTransfer := r.TransferAmount > 0
	switch {
	case hasCash && !hasCard && !hasCheck && !hasTransfer:
		return "現金", "p-completed"
	case hasCard && !hasCash && !hasCheck && !hasTransfer:
		return "信用卡", "p-apprpend"
	case hasCheck && !hasCash && !hasCard && !hasTransfer:
		return "票據", "p-waitparts"
	case hasTransfer && !hasCash && !hasCard && !hasCheck:
		return "轉帳", "p-waitparts"
	case !hasCash && !hasCard && !hasCheck && !hasTransfer:
		return "未收款", "p-apprno"
	default:
		return "混合", "p-apprpend"
	}
}

// ListReceivables 收款列表 + 新增表單。
func ListReceivables(w http.ResponseWriter, r *http.Request, user model.User) {
	// 財務數字屬店內機密，與報表一致：技師不可看。
	if !canSeeMoney(user) {
		http.Error(w, "權限不足：收款紀錄僅限櫃檯人員與系統管理員使用", http.StatusForbidden)
		return
	}

	qv := r.URL.Query()
	q := strings.TrimSpace(qv.Get("q"))
	method := strings.TrimSpace(qv.Get("method"))
	from := strings.TrimSpace(qv.Get("from"))
	to := strings.TrimSpace(qv.Get("to"))
	showVoided := qv.Get("voided") == "1"
	unpaidOnly := qv.Get("unpaid") == "1"
	page := pageNo(r)
	size := pageSize(r)

	where := ` WHERE 1=1`
	var args []interface{}
	if unpaidOnly {
		// 待收款：工单已结帐但收款合计不足
		where += ` AND r.work_order_id IN (
			SELECT wo.id FROM active_work_orders wo
			WHERE wo.status = 'completed'
			AND COALESCE((SELECT SUM(r2.pay_total) FROM receivables r2 WHERE r2.work_order_id = wo.id AND r2.voided = 0), 0) < wo.total_amount
		)`
	}
	// 作廢的預設隱藏（帳務清單不該被作廢單干擾），但要能查。
	if !showVoided {
		where += ` AND r.voided = 0`
	}
	if q != "" {
		where += ` AND (r.voucher_no LIKE ? OR c.name LIKE ? OR c.phone LIKE ?
			OR COALESCE(v.plate_number,'') LIKE ? OR COALESCE(r.check_no,'') LIKE ?
			OR COALESCE(r.transfer_reference,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like, like, like)
	}
	// 付款方式篩選。舊資料同一筆可並存多種金額，所以用「> 0」判斷而非等於。
	switch method {
	case "cash":
		where += ` AND r.cash > 0`
	case "card":
		where += ` AND r.card > 0`
	case "check":
		where += ` AND r.check_amount > 0`
	case "transfer":
		where += ` AND r.transfer_amount > 0`
	}
	// 日期區間。舊資料是 '100/03/11'（民國），新資料是 ISO，
	// 先用 normDateSQL 正規化再比對，兩種格式都能篩。
	if from != "" {
		where += ` AND ` + normDateSQL("r.pay_date") + ` >= date(?)`
		args = append(args, from)
	}
	if to != "" {
		where += ` AND ` + normDateSQL("r.pay_date") + ` <= date(?)`
		args = append(args, to)
	}

	total := countRows(`SELECT COUNT(1)`+receivableFrom+where, args...)

	// 合計（不受分頁影響）：這是這一頁最主要的用途 —— 知道收了多少錢。
	var sumCash, sumCard, sumCheck, sumTransfer, sumPay float64
	db.DB.QueryRow(`SELECT COALESCE(SUM(r.cash),0), COALESCE(SUM(r.card),0),
		COALESCE(SUM(r.check_amount),0), COALESCE(SUM(r.transfer_amount),0),
		COALESCE(SUM(r.pay_total),0)`+
		receivableFrom+where, args...).Scan(&sumCash, &sumCard, &sumCheck, &sumTransfer, &sumPay)

	// 計算每張工單的已收金額（用於部分收款追蹤）
	woReceived := make(map[int64]float64)
	woTotal := make(map[int64]float64)
	woRows, _ := db.DB.Query(`SELECT r.work_order_id, COALESCE(r.pay_total,0), COALESCE(wo.total_amount,0)
		FROM receivables r
		JOIN active_work_orders wo ON wo.id = r.work_order_id
		WHERE r.voided = 0 AND r.work_order_id > 0`)
	if woRows != nil {
		for woRows.Next() {
			var woID int64
			var received, total float64
			if woRows.Scan(&woID, &received, &total) == nil {
				woReceived[woID] += received
				woTotal[woID] = total
			}
		}
		woRows.Close()
	}

	query := `SELECT ` + receivableCols + receivableFrom + where +
		` ORDER BY ` + normDateSQL("r.pay_date") + ` DESC, r.id DESC LIMIT ? OFFSET ?`
	pageArgs := append(append([]interface{}{}, args...), size, (page-1)*size)

	rows, err := db.DB.Query(query, pageArgs...)
	if err != nil {
		fail(w, "查詢收款紀錄失敗")
		return
	}
	defer rows.Close()

	var recRows []pages.ReceivableRow
	for rows.Next() {
		r2, err := scanReceivable(rows)
		if err != nil {
			continue
		}
		label, cls := payMethod(r2)
		if r2.Voided != 0 {
			label, cls = "已作廢", "p-apprno"
		}
		woCell := "-"
		receivedCell := "-"
		unpaidCell := "-"
		unpaidRed := false
		if r2.WorkOrderID > 0 {
			woCell = "#" + strconv.FormatInt(r2.WorkOrderID, 10)
			if total, ok := woTotal[r2.WorkOrderID]; ok {
				received := woReceived[r2.WorkOrderID]
				unpaid := total - received
				receivedCell = money(received)
				if unpaid > 0 {
					unpaidCell = money(unpaid)
					unpaidRed = true
				} else {
					unpaidCell = "0"
				}
			}
		}
		recRows = append(recRows, pages.ReceivableRow{
			ID:             r2.ID,
			VoucherNo:      r2.VoucherNo,
			PayDate:        normDate(r2.PayDate),
			CustomerName:   r2.CustomerName,
			PlateNumber:    r2.PlateNumber,
			MethodLabel:    label,
			MethodPill:     cls,
			Cash:           r2.Cash,
			Card:           r2.Card,
			CheckAmount:    r2.CheckAmount,
			TransferAmount: r2.TransferAmount,
			PayTotal:       r2.PayTotal,
			WorkOrderID:    r2.WorkOrderID,
			WoCell:         woCell,
			ReceivedCell:   receivedCell,
			UnpaidCell:     unpaidCell,
			UnpaidRed:      unpaidRed,
			IsLegacy:       r2.Source == "legacy",
		})
	}

	pages.ListReceivablesPage(recRows, total, page, size, q, method, from, to,
		showVoided, unpaidOnly, sumPay, sumCash, sumCard, sumCheck, sumTransfer,
		user).Render(r.Context(), w)
}

// SearchReceivablesAPI 收款即時搜尋 API（HTMX 用）。
func SearchReceivablesAPI(w http.ResponseWriter, r *http.Request, user model.User) {
	// 財務數字屬店內機密：搜尋 API 與列表頁一致，技師不可看。
	if !canSeeMoney(user) {
		http.Error(w, "", http.StatusForbidden)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	where := ` WHERE r.voided = 0`
	var args []interface{}
	if q != "" {
		where += ` AND (r.voucher_no LIKE ? OR c.name LIKE ? OR c.phone LIKE ?
			OR COALESCE(v.plate_number,'') LIKE ? OR COALESCE(r.check_no,'') LIKE ?
			OR COALESCE(r.transfer_reference,'') LIKE ?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, like, like, like)
	}
	rows, err := db.DB.Query(`SELECT `+receivableCols+receivableFrom+where+
		` ORDER BY `+normDateSQL("r.pay_date")+` DESC, r.id DESC LIMIT 50`, args...)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var result []pages.ReceivableRow
	for rows.Next() {
		r2, err := scanReceivable(rows)
		if err != nil {
			continue
		}
		label, cls := payMethod(r2)
		if r2.Voided != 0 {
			label, cls = "已作廢", "p-apprno"
		}
		result = append(result, pages.ReceivableRow{
			ID:             r2.ID,
			VoucherNo:      r2.VoucherNo,
			PayDate:        normDate(r2.PayDate),
			CustomerName:   r2.CustomerName,
			PlateNumber:    r2.PlateNumber,
			MethodLabel:    label,
			MethodPill:     cls,
			Cash:           r2.Cash,
			Card:           r2.Card,
			CheckAmount:    r2.CheckAmount,
			TransferAmount: r2.TransferAmount,
			PayTotal:       r2.PayTotal,
			WorkOrderID:    r2.WorkOrderID,
			WoCell:         woCellText(r2.WorkOrderID),
			ReceivedCell:   "-",
			UnpaidCell:     "-",
			IsLegacy:       r2.Source == "legacy",
		})
	}
	pages.ReceivableSearchRows(result).Render(r.Context(), w)
}

func woCellText(woID int64) string {
	if woID > 0 {
		return "#" + strconv.FormatInt(woID, 10)
	}
	return "-"
}

// normDateSQL 產生「日期欄位正規化為 YYYY-MM-DD」的 SQL 片段。
//
// 舊系統的日期是民國年 '100/03/11'，新資料是 ISO '2026-10-07'。
// SQLite 的 date() 不吃斜線格式（date('100/03/11') 回 NULL），
// 直接比對會讓所有舊資料被篩掉，所以要先轉換。
// 與 normDate() 的規則保持一致。
func normDateSQL(col string) string {
	// 民國年 → 西元：斜線格式且首段 <= 200 者視為民國年，+1911。
	// 用 CASE 在 SQL 裡做，避免為了排序把整表撈出來。
	return `date(CASE
		WHEN ` + col + ` LIKE '%/%/%' AND CAST(substr(` + col + `,1,instr(` + col + `,'/')-1) AS INTEGER) BETWEEN 1 AND 200
		THEN (CAST(substr(` + col + `,1,instr(` + col + `,'/')-1) AS INTEGER) + 1911) || '-' ||
			substr(` + col + `, instr(` + col + `,'/')+1, instr(substr(` + col + `, instr(` + col + `,'/')+1), '/') - 1) || '-' ||
			substr(` + col + `, instr(substr(` + col + `, instr(` + col + `,'/')+1), '/') + instr(` + col + `,'/') + 1)
		WHEN ` + col + ` LIKE '%/%/%'
		THEN replace(` + col + `, '/', '-')
		ELSE ` + col + `
	END)`
}
