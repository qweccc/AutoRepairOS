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
	if sum > 0 && payTotal > 0 && (payTotal < sum-0.005 || payTotal > sum+0.005) {
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
	if cash < 0 || card < 0 || checkAmt < 0 || transferAmt < 0 || payTotal < 0 {
		fail(w, "收款金額不可為負數")
		return
	}
	if cash+card+checkAmt+transferAmt == 0 && payTotal == 0 {
		fail(w, "請至少填一種付款方式或收款合計")
		return
	}
	sum := cash + card + checkAmt + transferAmt
	if sum > 0 && payTotal > 0 && (payTotal < sum-0.005 || payTotal > sum+0.005) {
		fail(w, "收款合計與各付款方式加總不符")
		return
	}
	if sum > 0 && payTotal < 0 {
		fail(w, "收款合計不得為負數")
		return
	}

	voucher := strings.TrimSpace(r.FormValue("voucher_no"))
	if voucher == "" {
		fail(w, "收款單號不能為空")
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
	var n int
	db.DB.QueryRow(`SELECT COUNT(1) FROM receivables WHERE voucher_no LIKE ?`,
		prefix+"%").Scan(&n)
	return prefix + pad3(n+1)
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
