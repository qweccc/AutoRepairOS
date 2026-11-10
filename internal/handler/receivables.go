package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/service"
	"autorepairos/web/pages"
)

// 收款沖銷（應收帳款收款紀錄）。
//
// 對應舊系統 CAR35 的「應收帳款沖銷作業」（Customer Pay，90,762 筆）——
// 這是舊系統金額最大的缺口：現金 3.48 億、刷卡 4,417 萬、支票 714 萬。
// 在這一頁出現之前，新系統結帳只能把工單狀態改成 paid，
// 看不出「收了多少、用什麼方式收、有沒有折讓、票什麼時候到期」。
//
// 為什麼收款是獨立實體而不是 work_orders 上的欄位：
//   1. 一張工單可以分次收款（舊資料 90,762 筆對 87,625 張工單）
//   2. 一次收款可以沖銷多張工單（月結客戶）
//   3. 收款單有獨立單號，需單獨查詢與列印
// 因此 work_order_id 可空 —— 手動收款不必綁特定工單。

// paymentMismatch checks if payment total doesn't match the sum of payment methods.
func paymentMismatch(sum, payTotal float64) bool {
	return sum > 0 && payTotal > 0 && (payTotal < sum-0.005 || payTotal > sum+0.005)
}

// NewReceivableForm 新增收款表單。
func NewReceivableForm(w http.ResponseWriter, r *http.Request, user model.User) {
	if !canSeeMoney(user) {
		http.Error(w, "權限不足", http.StatusForbidden)
		return
	}
	payDate := today()
	rec := receivable{VoucherNo: nextVoucherNo(payDate), PayDate: payDate}
	if workOrderID := mustInt(r.URL.Query().Get("work_order")); workOrderID > 0 {
		rec.WorkOrderID = workOrderID
		err := db.DB.QueryRow(`SELECT wo.customer_id, COALESCE(c.name,'')
			FROM active_work_orders wo JOIN active_customers c ON c.id = wo.customer_id
			WHERE wo.id = ?`, workOrderID).Scan(&rec.CustomerID, &rec.CustomerName)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "載入工單收款資料失敗", http.StatusInternalServerError)
			return
		}
	}
	renderReceivableForm(w, r, user, rec, "", "")
}

// receivableFormBody 收款表單（新增與編輯共用）。
func renderReceivableForm(w http.ResponseWriter, r *http.Request, user model.User, rec receivable, action, errMsg string) {
	sel := func(v float64) string {
		if v == 0 {
			return ""
		}
		return money(v)
	}
	pages.ReceivableFormPage(pages.ReceivableFormData{
		Title: "收款紀錄", Action: action,
		VoucherNo: rec.VoucherNo, PayDate: normDate(rec.PayDate),
		CustomerName: rec.CustomerName, CustomerID: strconv.FormatInt(rec.CustomerID, 10),
		WorkOrderID: intOrEmpty(rec.WorkOrderID), Cash: sel(rec.Cash), Card: sel(rec.Card),
		CardNo: rec.CardNo, CheckAmount: sel(rec.CheckAmount), CheckNo: rec.CheckNo,
		CheckDate: normDate(rec.CheckDate), Bank: rec.Bank, TransferAmount: sel(rec.TransferAmount),
		TransferReference: rec.TransferReference, DetailTotal: sel(rec.DetailTotal),
		Discount: sel(rec.Discount), Tax: sel(rec.Tax), PayTotal: sel(rec.PayTotal),
		Notes: rec.Notes, Error: errMsg, User: user,
	}).Render(r.Context(), w)
}

// CreateReceivable 建立收款紀錄。
func CreateReceivable(w http.ResponseWriter, r *http.Request) {
	// 表單送的是可讀的 customer_q（姓名/電話/車牌），沒帶 customer_id 時
	// 用既有的 resolveCustomerID 找一次，避免櫃檯必須先查 id。
	// 為了重用，把 customer_q 複製成該函式認得的 customer_query。
	r.Form = func() url.Values {
		r.ParseForm()
		v := r.Form
		if v.Get("customer_query") == "" && v.Get("customer_q") != "" {
			v.Set("customer_query", v.Get("customer_q"))
		}
		return v
	}()
	customerID, msg := resolveCustomerID(r)
	if customerID <= 0 {
		redirectMsg(w, r, "/receivables/new", "找不到客戶："+msg)
		return
	}

	payDate := strings.TrimSpace(r.FormValue("pay_date"))
	if payDate == "" {
		payDate = today()
	}

	cash := atofDefault(r.FormValue("cash"), 0)
	card := atofDefault(r.FormValue("card"), 0)
	checkAmt := atofDefault(r.FormValue("check_amount"), 0)
	transferAmt := atofDefault(r.FormValue("transfer_amount"), 0)
	payTotal := atofDefault(r.FormValue("pay_total"), 0)
	// 合計留空時自動加總，避免櫃檯填了方式卻忘了填合計。
	if payTotal == 0 {
		payTotal = cash + card + checkAmt + transferAmt
	}
	if cash < 0 || card < 0 || checkAmt < 0 || transferAmt < 0 || payTotal < 0 {
		redirectMsg(w, r, "/receivables/new", "收款金額不可為負數")
		return
	}
	if cash+card+checkAmt+transferAmt == 0 && payTotal == 0 {
		redirectMsg(w, r, "/receivables/new", "請至少填一種付款方式或收款合計")
		return
	}
	sum := cash + card + checkAmt + transferAmt
	if paymentMismatch(sum, payTotal) {
		redirectMsg(w, r, "/receivables/new", "收款合計與各付款方式加總不符")
		return
	}
	if sum > 0 && payTotal < 0 {
		redirectMsg(w, r, "/receivables/new", "收款合計不得為負數")
		return
	}

	voucher := strings.TrimSpace(r.FormValue("voucher_no"))
	if voucher == "" {
		redirectMsg(w, r, "/receivables/new", "收款單號不可空白，請重新載入表單")
		return
	}

	// 關聯工單（可空）。收款與必要的結帳由 service 在同一交易內處理。
	woID := mustInt(r.FormValue("work_order_id"))

	// created_by：用自己的身分寫入（與 inventory 調庫同樣做法）
	actorID := int64(0)
	if u, uerr := currentUser(r); uerr == nil {
		actorID = u.ID
	}

	detailTotal := atofDefault(r.FormValue("detail_total"), 0)
	realTotal := atofDefault(r.FormValue("real_total"), 0)
	discount := atofDefault(r.FormValue("discount"), 0)
	tax := atofDefault(r.FormValue("tax"), 0)
	cardNo := strings.TrimSpace(r.FormValue("card_no"))
	checkNo := strings.TrimSpace(r.FormValue("check_no"))
	checkDate := normDate(r.FormValue("check_date"))
	bank := strings.TrimSpace(r.FormValue("bank"))
	notes := strings.TrimSpace(r.FormValue("notes"))

	err := service.New(db.DB).CreateReceipt(r.Context(), service.ReceiptInput{
		VoucherNo: voucher, CustomerID: customerID, WorkOrderID: woID, PayDate: payDate,
		DetailTotal: detailTotal, RealTotal: realTotal, Discount: discount, Tax: tax,
		PayTotal: payTotal, Cash: cash, Card: card, CheckAmount: checkAmt,
		TransferAmount: transferAmt, TransferReference: strings.TrimSpace(r.FormValue("transfer_reference")),
		CardNo: cardNo, CheckNo: checkNo, CheckDate: checkDate, Bank: bank,
		Notes: notes, CreatedBy: actorID,
	})
	if err != nil {
		redirectMsg(w, r, "/receivables/new", "新增收款失敗："+err.Error())
		return
	}

	redirectMsg(w, r, "/receivables", "收款已記錄："+voucher+"（"+money(payTotal)+"）")
}

// EditReceivableForm 編輯收款表單。
func EditReceivableForm(w http.ResponseWriter, r *http.Request, user model.User) {
	if !canSeeMoney(user) {
		http.Error(w, "權限不足", http.StatusForbidden)
		return
	}
	id := mustInt(r.PathValue("id"))

	var rec receivable
	err := db.DB.QueryRow(`SELECT `+receivableCols+` FROM receivables r
		JOIN active_customers c ON c.id = r.customer_id
		LEFT JOIN active_vehicles v ON v.id = r.vehicle_id
		WHERE r.id = ?`, id).Scan(
		&rec.ID, &rec.VoucherNo, &rec.CustomerID, &rec.CustomerName,
		&rec.VehicleID, &rec.PlateNumber, &rec.WorkOrderID,
		&rec.PayDate, &rec.DetailTotal, &rec.RealTotal,
		&rec.Discount, &rec.Tax, &rec.PayTotal,
		&rec.Cash, &rec.Card, &rec.CheckAmount, &rec.TransferAmount, &rec.TransferReference,
		&rec.CardNo, &rec.CheckNo, &rec.CheckDate,
		&rec.Bank, &rec.Notes, &rec.Voided, &rec.Source)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	action := "/receivables/" + strconv.FormatInt(id, 10)
	renderReceivableForm(w, r, user, rec, action, "")
}

// UpdateReceivable 更新收款紀錄。
func UpdateReceivable(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))

	payDate := strings.TrimSpace(r.FormValue("pay_date"))
	if payDate == "" {
		payDate = today()
	}

	cash := atofDefault(r.FormValue("cash"), 0)
	card := atofDefault(r.FormValue("card"), 0)
	checkAmt := atofDefault(r.FormValue("check_amount"), 0)
	transferAmt := atofDefault(r.FormValue("transfer_amount"), 0)
	payTotal := atofDefault(r.FormValue("pay_total"), 0)
	if payTotal == 0 {
		payTotal = cash + card + checkAmt + transferAmt
	}
	editPath := "/receivables/" + strconv.FormatInt(id, 10) + "/edit"
	if cash < 0 || card < 0 || checkAmt < 0 || transferAmt < 0 || payTotal < 0 {
		formError(w, r, "編輯收款紀錄", editPath, "收款金額不可為負數")
		return
	}
	if cash+card+checkAmt+transferAmt == 0 && payTotal == 0 {
		formError(w, r, "編輯收款紀錄", editPath, "請至少填一種付款方式或收款合計")
		return
	}
	sum := cash + card + checkAmt + transferAmt
	if paymentMismatch(sum, payTotal) {
		formError(w, r, "編輯收款紀錄", editPath, "收款合計與各付款方式加總不符")
		return
	}
	if sum > 0 && payTotal < 0 {
		formError(w, r, "編輯收款紀錄", editPath, "收款合計不得為負數")
		return
	}

	voucher := strings.TrimSpace(r.FormValue("voucher_no"))
	if voucher == "" {
		formError(w, r, "編輯收款紀錄", editPath, "收款單號不能為空")
		return
	}

	actorID := int64(0)
	if user, userErr := currentUser(r); userErr == nil {
		actorID = user.ID
	}
	err := service.New(db.DB).UpdateReceipt(r.Context(), id, service.ReceiptInput{
		VoucherNo: voucher, PayDate: payDate,
		DetailTotal: atofDefault(r.FormValue("detail_total"), 0),
		RealTotal:   atofDefault(r.FormValue("real_total"), 0),
		Discount:    atofDefault(r.FormValue("discount"), 0),
		Tax:         atofDefault(r.FormValue("tax"), 0),
		PayTotal:    payTotal, Cash: cash, Card: card, CheckAmount: checkAmt,
		TransferAmount: transferAmt, TransferReference: nullStr(r.FormValue("transfer_reference")),
		CardNo: nullStr(r.FormValue("card_no")), CheckNo: nullStr(r.FormValue("check_no")),
		CheckDate: normDate(r.FormValue("check_date")), Bank: nullStr(r.FormValue("bank")),
		Notes: nullStr(r.FormValue("notes")), CreatedBy: actorID,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrReceiptNotFound):
			http.NotFound(w, r)
		case errors.Is(err, service.ErrReceiptVoided):
			fail(w, "已作廢的收款不能編輯")
		default:
			fail(w, "更新收款失敗："+err.Error())
		}
		return
	}

	redirectMsg(w, r, "/receivables", "收款已更新："+voucher)
}

// VoidReceivable 作廢收款（軟刪除）。
//
// 為什麼不真刪：帳務紀錄需要留痕。舊系統也是用 Delete Flag 標記（9 筆），
// 真刪會讓對帳金額對不上、也無法追查是誰改的。
func VoidReceivable(w http.ResponseWriter, r *http.Request) {
	id := mustInt(r.PathValue("id"))
	if id <= 0 {
		http.NotFound(w, r)
		return
	}
	err := service.New(db.DB).VoidReceipt(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrReceiptNotFound), errors.Is(err, service.ErrReceiptVoided):
			redirectMsg(w, r, "/receivables", "該筆不存在或已作廢")
		default:
			fail(w, "作廢失敗："+err.Error())
		}
		return
	}
	redirectMsg(w, r, "/receivables", "已作廢該筆收款（紀錄保留，可用「含作廢」查回）")
}

// nextVoucherNo 產生收款單號：RCV-YYYYMMDD-NNN。
//
// 舊系統的單號格式很雜（'.0003001'、'+9/00001'），新單號另立格式，
// 讓「舊帳」與「新收」一眼可辨。
func nextVoucherNo(payDate string) string {
	day := strings.ReplaceAll(normDate(payDate), "-", "")
	if day == "" {
		day = strings.ReplaceAll(today(), "-", "")
	}
	prefix := "RCV-" + day + "-"
	// 使用 voucher_sequences 序號表原子遞增，避免 COUNT(*) 在併發時撞號
	var n int
	tx, err := db.DB.Begin()
	if err != nil {
		// fallback：舊邏輯
		db.DB.QueryRow(`SELECT COUNT(1) FROM receivables WHERE voucher_no LIKE ?`,
			prefix+"%").Scan(&n)
		return prefix + pad3(n+1)
	}
	defer tx.Rollback()
	_, _ = tx.Exec(`INSERT INTO voucher_sequences (prefix, last_seq) VALUES (?, 0) ON CONFLICT(prefix) DO NOTHING`, prefix)
	tx.QueryRow(`SELECT last_seq FROM voucher_sequences WHERE prefix = ?`, prefix).Scan(&n)
	n++
	_, _ = tx.Exec(`UPDATE voucher_sequences SET last_seq = ? WHERE prefix = ?`, n, prefix)
	if err := tx.Commit(); err != nil {
		db.DB.QueryRow(`SELECT COUNT(1) FROM receivables WHERE voucher_no LIKE ?`,
			prefix+"%").Scan(&n)
		return prefix + pad3(n+1)
	}
	return prefix + pad3(n)
}

func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

// moneyIf 金額為 0 時顯示破折號，避免一整排 0 蓋掉真正的數字。
func moneyIf(f float64) string {
	if f == 0 {
		return `<span style="color:#475569">—</span>`
	}
	return money(f)
}

func intOrEmpty(n int64) string {
	if n <= 0 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}
