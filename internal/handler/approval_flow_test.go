package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// 這批測試涵蓋「追加維修授權」的完整流程。修復前：main.go 有客戶端的
// 開啟／同意／拒絕三個路由，但沒有任何地方建立授權請求（CreateApprovalRequest
// 全 repo 只出現在一行註解裡），而同意時「加入工單項目」還是 TODO——所以
// 手冊描述的流程從 UI 走不完，而且即使走到同意，工單也不會多出項目。

// TestApprovalFlowAddsItemsAndRecalculatesTotal 同意後項目要進工單，且總金額要重算。
//
// 重算這一步特別重要：少了它，項目進得了明細卻不算進 total_amount，帳務上
// 等於漏收。AddWorkOrderItem 也有同樣的呼叫，兩條路徑的口徑必須一致。
func TestApprovalFlowAddsItemsAndRecalculatesTotal(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1,
		`[{"description":"煞車油管老化更換","quantity":2,"unit_price":500,"item_type":"part"}]`)
	if err != nil {
		t.Fatalf("建立授權請求失敗: %v", err)
	}

	w := httptest.NewRecorder()
	ApproveRequestHandler(w, httptest.NewRequest("POST", "/approval/approve/"+req.Token, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("同意授權期望 200，得到 %d", w.Code)
	}

	var n int
	var subtotal float64
	if err := d.QueryRow(`SELECT COUNT(*), COALESCE(SUM(subtotal),0) FROM work_order_items
		WHERE work_order_id = 100`).Scan(&n, &subtotal); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("同意後工單項目數應為 1，實際 %d", n)
	}
	if subtotal != 1000 {
		t.Errorf("項目小計應為 2×500=1000，實際 %v", subtotal)
	}

	var total float64
	if err := d.QueryRow(`SELECT COALESCE(total_amount,0) FROM work_orders WHERE id = 100`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 1000 {
		t.Errorf("工單總金額應重算為 1000，實際 %v（項目進了明細卻沒算進總額＝漏收）", total)
	}

	var status, approval string
	if err := d.QueryRow(`SELECT status FROM approval_requests WHERE token = ?`, req.Token).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT approval_status FROM work_orders WHERE id = 100`).Scan(&approval); err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		t.Errorf("授權狀態應為 approved，實際 %q", status)
	}
	if approval != "approved" {
		t.Errorf("工單 approval_status 應為 approved，實際 %q", approval)
	}
}

// TestApprovalApproveIsIdempotent 重複同意不可重複加項目。
//
// 客戶可能重複點連結（或瀏覽器重送 POST）。若每次都插入，客戶同意一次卻被
// 收兩次錢——這是最不能出的錯。
func TestApprovalApproveIsIdempotent(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1,
		`[{"description":"更換油管","quantity":1,"unit_price":800,"item_type":"part"}]`)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i <= 3; i++ {
		w := httptest.NewRecorder()
		ApproveRequestHandler(w, httptest.NewRequest("POST", "/approval/approve/"+req.Token, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 次同意期望 200，得到 %d", i, w.Code)
		}
	}

	var n int
	var total float64
	if err := d.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE work_order_id = 100`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT COALESCE(total_amount,0) FROM work_orders WHERE id = 100`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("重複同意後項目數仍應為 1，實際 %d（客戶同意一次卻被收多次錢）", n)
	}
	if total != 800 {
		t.Errorf("重複同意後總額仍應為 800，實際 %v", total)
	}
}

// TestRejectedApprovalAddsNothing 拒絕不可加入任何項目或變更金額。
func TestRejectedApprovalAddsNothing(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1,
		`[{"description":"引擎大修","quantity":1,"unit_price":30000,"item_type":"labor"}]`)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	RejectRequestHandler(w, httptest.NewRequest("POST", "/approval/reject/"+req.Token, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("拒絕授權期望 200，得到 %d", w.Code)
	}

	var n int
	var total float64
	if err := d.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE work_order_id = 100`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(`SELECT COALESCE(total_amount,0) FROM work_orders WHERE id = 100`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("客戶拒絕後不該有任何項目，實際 %d 筆", n)
	}
	if total != 0 {
		t.Errorf("客戶拒絕後總額不該變動，實際 %v", total)
	}
}

// TestApproveRejectsLegacyFreeTextSummary 自由文字的 item_summary 要 fail-closed。
//
// 舊資料（或任何繞過 UI 直接寫入的列）可能是自由文字。這種情況不能「反正先
// 核准、項目之後再說」——那會產生「客戶以為同意了某個金額、工單卻沒有對應
// 項目」的錢務不一致。寧可拒絕並留下紀錄，讓車廠人工處理。
func TestApproveRejectsLegacyFreeTextSummary(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1, "換機油") // 舊格式：自由文字
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	ApproveRequestHandler(w, httptest.NewRequest("POST", "/approval/approve/"+req.Token, nil))

	var status string
	if err := d.QueryRow(`SELECT status FROM approval_requests WHERE token = ?`, req.Token).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "approved" {
		t.Error("無法解析的追加項目不可被核准——會造成「已核准但工單沒有項目」的不一致")
	}
	if status != "pending" {
		t.Errorf("解析失敗時狀態應維持 pending（保留人工處理空間），實際 %q", status)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE work_order_id = 100`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("解析失敗時不該寫入任何項目，實際 %d 筆", n)
	}
}

// TestParseApprovalItemsValidation 驗證規則集中在 parseApprovalItems。
func TestParseApprovalItemsValidation(t *testing.T) {
	bad := []struct {
		name string
		raw  string
	}{
		{"空字串", ``},
		{"只有空白", `   `},
		{"空陣列", `[]`},
		{"不是 JSON", `換機油`},
		{"缺品名", `[{"description":"","quantity":1,"unit_price":100,"item_type":"part"}]`},
		{"只有空白的品名", `[{"description":"   ","quantity":1,"unit_price":100,"item_type":"part"}]`},
		{"數量為 0", `[{"description":"x","quantity":0,"unit_price":100,"item_type":"part"}]`},
		{"數量為負", `[{"description":"x","quantity":-1,"unit_price":100,"item_type":"part"}]`},
		{"單價為負", `[{"description":"x","quantity":1,"unit_price":-1,"item_type":"part"}]`},
		{"類別無效", `[{"description":"x","quantity":1,"unit_price":0,"item_type":"bogus"}]`},
		{"類別空白", `[{"description":"x","quantity":1,"unit_price":0,"item_type":""}]`},
	}
	for _, c := range bad {
		if _, err := parseApprovalItems(c.raw); err == nil {
			t.Errorf("%s（%q）應該被拒絕", c.name, c.raw)
		}
	}

	good := []string{
		`[{"description":"更換油管","quantity":1,"unit_price":500,"item_type":"part"}]`,
		`[{"description":"工資","quantity":2,"unit_price":0,"item_type":"labor"}]`,
		`[{"description":"外包烤漆","quantity":1,"unit_price":3000,"item_type":"subcontract"}]`,
		`[{"description":"A","quantity":1,"unit_price":1,"item_type":"part"},{"description":"B","quantity":2,"unit_price":2,"item_type":"labor"}]`,
	}
	for _, raw := range good {
		if _, err := parseApprovalItems(raw); err != nil {
			t.Errorf("合法項目 %q 不該被拒絕: %v", raw, err)
		}
	}
}

// TestCreateApprovalFromWorkOrderBlocksPaidWorkOrder 已結帳的工單不可再追加。
//
// 已結帳工單是財務憑證；追加項目會讓已開立的收據與實際金額不符。
func TestCreateApprovalFromWorkOrderBlocksPaidWorkOrder(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	if _, err := d.Exec(`UPDATE work_orders SET status='paid' WHERE id = 100`); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"description": {"追加項目"},
		"quantity":    {"1"},
		"unit_price":  {"500"},
		"item_type":   {"part"},
	}
	r := httptest.NewRequest("POST", "/work-orders/100/approval-request", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", "100")
	w := httptest.NewRecorder()

	CreateApprovalFromWorkOrder(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("已結帳工單應被導回（302），得到 %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("已結帳工單被擋時應帶 err= 參數（紅色訊息），實際 Location=%q", loc)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM approval_requests WHERE work_order_id = 100`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("已結帳工單不該建立授權請求，實際 %d 筆", n)
	}
}

// TestCreateApprovalFromWorkOrderRequiresLineBinding 未綁定 LINE 的客戶無法收到連結。
//
// 這裡要的是「明確的錯誤訊息」而不是靜默失敗：櫃檯必須知道要改用電話聯絡，
// 而不是以為連結已經送出去了。
func TestCreateApprovalFromWorkOrderRequiresLineBinding(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	// 工單 101 屬於客戶 2（未綁定 LINE）
	form := url.Values{
		"description": {"追加項目"},
		"quantity":    {"1"},
		"unit_price":  {"500"},
		"item_type":   {"part"},
	}
	r := httptest.NewRequest("POST", "/work-orders/101/approval-request", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", "101")
	w := httptest.NewRecorder()

	CreateApprovalFromWorkOrder(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("未綁定 LINE 應被導回（302），得到 %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Errorf("未綁定 LINE 時應帶 err= 參數，實際 Location=%q", loc)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM approval_requests WHERE work_order_id = 101`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("無法送達的授權不該建立（會讓櫃檯以為通知已送出），實際 %d 筆", n)
	}
}

// TestCreateApprovalFromWorkOrderRejectsBadInput 表單驗證與解析端共用同一套規則。
func TestCreateApprovalFromWorkOrderRejectsBadInput(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	cases := []struct {
		name       string
		desc, qty  string
		unit, kind string
	}{
		{"缺品名", "", "1", "500", "part"},
		{"數量為 0", "x", "0", "500", "part"},
		{"數量為負", "x", "-2", "500", "part"},
		{"單價為負", "x", "1", "-5", "part"},
		{"類別無效", "x", "1", "500", "bogus"},
	}
	for _, c := range cases {
		form := url.Values{
			"description": {c.desc}, "quantity": {c.qty},
			"unit_price": {c.unit}, "item_type": {c.kind},
		}
		r := httptest.NewRequest("POST", "/work-orders/100/approval-request", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetPathValue("id", "100")
		w := httptest.NewRecorder()

		CreateApprovalFromWorkOrder(w, r)

		if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
			t.Errorf("%s：應被拒絕並帶 err= 參數，實際 code=%d Location=%q", c.name, w.Code, loc)
		}
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM approval_requests WHERE work_order_id = 100`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("不合法的輸入不該建立任何授權，實際 %d 筆", n)
	}
}
