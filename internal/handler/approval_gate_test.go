package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// 這批測試涵蓋「未決授權阻擋完工」與「電話同意」兩條路徑。
//
// 為什麼需要電話同意：LINE 連結要靠客戶自己點。客戶電話上說「好，你修」
// 但沒點連結時，工單會永遠卡在待客戶同意，而車子已經在頂車機上了。

const goodItems = `[{"description":"煞車油管","quantity":2,"unit_price":500,"item_type":"part"}]`

// --- CheckApprovalForStatus ---

func TestCheckApprovalForStatusBlocksWhenPending(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	if _, err := CreateApprovalRequest(100, 1, goodItems); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"completed", "paid"} {
		if err := CheckApprovalForStatus(100, target); err == nil {
			t.Errorf("有未決授權時 %s 應該被擋下", target)
		}
	}
	// 非完工／結帳的狀態不受影響
	for _, target := range []string{"in_progress", "pending", "waiting_parts"} {
		if err := CheckApprovalForStatus(100, target); err != nil {
			t.Errorf("%s 不該被未決授權擋下: %v", target, err)
		}
	}
}

// 這是最重要的一條：沒有追加授權的工單必須能完工。
//
// 判準若寫成「工單的 approval_status == pending」就會擋死每一張工單——
// 因為 pending 是 work_orders 的預設值，絕大多數工單根本沒有追加項目。
func TestCheckApprovalForStatusAllowsWhenNoApproval(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	// setup 的兩張工單 approval_status 都是 'pending'（預設值）
	var st string
	if err := d.QueryRow(`SELECT approval_status FROM work_orders WHERE id = 100`).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != "pending" {
		t.Fatalf("前置條件：工單的 approval_status 應為 pending（預設值），實際 %q", st)
	}

	for _, target := range []string{"completed", "paid"} {
		if err := CheckApprovalForStatus(100, target); err != nil {
			t.Errorf("沒有任何追加授權時 %s 不該被擋（approval_status 的預設值就是 pending，"+
				"照它擋會讓每張工單都無法完工）: %v", target, err)
		}
	}
}

func TestCheckApprovalForStatusAllowsWhenAlreadyDecided(t *testing.T) {
	for _, tc := range []struct{ name, status string }{
		{"已同意", "approved"}, {"已拒絕", "rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oldDB := db.DB
			defer func() { db.DB = oldDB }()
			d := setupApprovalTestDB(t)
			db.DB = d

			req, err := CreateApprovalRequest(100, 1, goodItems)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Exec(`UPDATE approval_requests SET status=? WHERE id=?`, tc.status, req.ID); err != nil {
				t.Fatal(err)
			}
			if err := CheckApprovalForStatus(100, "completed"); err != nil {
				t.Errorf("%s 的授權不該擋完工: %v", tc.name, err)
			}
		})
	}
}

// 過期的授權不該繼續擋完工——客戶不會再回覆了，而工單不能永遠卡著。
func TestCheckApprovalForStatusIgnoresExpired(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1, goodItems)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE approval_requests SET expires_at='2020-01-01 00:00:00' WHERE id=?`, req.ID); err != nil {
		t.Fatal(err)
	}

	if err := CheckApprovalForStatus(100, "completed"); err != nil {
		t.Errorf("已過期的授權不該擋完工（客戶不會再回覆）: %v", err)
	}
}

// 別的工單的授權不該擋到這張工單。
func TestCheckApprovalForStatusScopedToWorkOrder(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	if _, err := CreateApprovalRequest(100, 1, goodItems); err != nil {
		t.Fatal(err)
	}
	if err := CheckApprovalForStatus(101, "completed"); err != nil {
		t.Errorf("工單 101 不該被工單 100 的授權擋下: %v", err)
	}
}

// --- ApproveApprovalByPhone ---

func postPhoneApprove(t *testing.T, woID, reqID int64, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/work-orders/x/approval-requests/y/approve-phone",
		strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", itoa(woID))
	r.SetPathValue("req_id", itoa(reqID))
	w := httptest.NewRecorder()
	ApproveApprovalByPhone(w, r)
	return w
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestApproveByPhoneAddsItemsAndRecordsSource(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1, goodItems)
	if err != nil {
		t.Fatal(err)
	}

	w := postPhoneApprove(t, 100, req.ID, url.Values{"note": {"與車主陳先生通話確認"}})
	if w.Code != http.StatusFound {
		t.Fatalf("電話同意應導回（302），得到 %d", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "msg=") {
		t.Errorf("成功時應帶 msg= 參數，實際 %q", loc)
	}

	var status, source, notes string
	if err := d.QueryRow(`SELECT status, decision_source, COALESCE(notes,'')
		FROM approval_requests WHERE id=?`, req.ID).Scan(&status, &source, &notes); err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		t.Errorf("授權狀態應為 approved，實際 %q", status)
	}
	// 來源必須是 phone：事後追查「這個同意是怎麼來的」時，
	// 電話同意與客戶自己點連結是可信度不同的兩件事。
	if source != "phone" {
		t.Errorf("decision_source 應為 phone（客戶自己點連結是 web），實際 %q", source)
	}
	if !strings.Contains(notes, "與車主陳先生通話確認") {
		t.Errorf("備註應保留在紀錄裡，實際 %q", notes)
	}

	var n int
	var subtotal, total float64
	if err := d.QueryRow(`SELECT COUNT(*), COALESCE(SUM(subtotal),0) FROM work_order_items
		WHERE work_order_id=100`).Scan(&n, &subtotal); err != nil {
		t.Fatal(err)
	}
	if n != 1 || subtotal != 1000 {
		t.Errorf("電話同意後應加入 1 筆小計 1000 的項目，實際 %d 筆 / %v", n, subtotal)
	}
	if err := d.QueryRow(`SELECT COALESCE(total_amount,0) FROM work_orders WHERE id=100`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 1000 {
		t.Errorf("工單總額應重算為 1000，實際 %v（漏收）", total)
	}
}

func TestApproveByPhoneRequiresNote(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1, goodItems)
	if err != nil {
		t.Fatal(err)
	}
	w := postPhoneApprove(t, 100, req.ID, url.Values{"note": {"   "}})

	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("沒填備註時應被擋下並帶 err=，實際 %q", loc)
	}
	var status string
	if err := d.QueryRow(`SELECT status FROM approval_requests WHERE id=?`, req.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Errorf("被擋下時狀態不該改變，實際 %q", status)
	}
}

// 不可以拿別張工單的授權 id 來核准自己的工單。
func TestApproveByPhoneRejectsWrongWorkOrder(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	// 授權屬於工單 100，卻從工單 101 送出
	req, err := CreateApprovalRequest(100, 1, goodItems)
	if err != nil {
		t.Fatal(err)
	}
	w := postPhoneApprove(t, 101, req.ID, url.Values{"note": {"x"}})

	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("工單不符時應被擋下並帶 err=，實際 %q", loc)
	}
	var status string
	if err := d.QueryRow(`SELECT status FROM approval_requests WHERE id=?`, req.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Errorf("狀態不該被改動，實際 %q", status)
	}
}

// 客戶已經自己點過連結後，櫃檯不能再記一次電話同意（否則項目會被加兩次）。
func TestApproveByPhoneRejectsAlreadyDecided(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1, goodItems)
	if err != nil {
		t.Fatal(err)
	}
	// 客戶先自己同意了
	if _, err := d.Exec(`UPDATE approval_requests SET status='approved', decision_source='web' WHERE id=?`, req.ID); err != nil {
		t.Fatal(err)
	}

	w := postPhoneApprove(t, 100, req.ID, url.Values{"note": {"電話也說好"}})
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("已決定的授權應被擋下並帶 err=，實際 %q", loc)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE work_order_id=100`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("重複記錄同意不該加入項目（會被收兩次錢），實際 %d 筆", n)
	}
}

func TestApproveByPhoneRejectsExpired(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1, goodItems)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE approval_requests SET expires_at='2020-01-01 00:00:00' WHERE id=?`, req.ID); err != nil {
		t.Fatal(err)
	}

	w := postPhoneApprove(t, 100, req.ID, url.Values{"note": {"x"}})
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
		t.Errorf("過期授權應被擋下並帶 err=，實際 %q", loc)
	}
}

// --- CancelApprovalRequest ---

func TestCancelApprovalRequest(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	req, err := CreateApprovalRequest(100, 1, goodItems)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest("POST", "/work-orders/x/approval-requests/y/cancel",
		strings.NewReader(url.Values{"note": {"客戶決定不修"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetPathValue("id", "100")
	r.SetPathValue("req_id", itoa(req.ID))
	w := httptest.NewRecorder()
	CancelApprovalRequest(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("取消應導回（302），得到 %d", w.Code)
	}
	var status, source string
	if err := d.QueryRow(`SELECT status, decision_source FROM approval_requests WHERE id=?`, req.ID).
		Scan(&status, &source); err != nil {
		t.Fatal(err)
	}
	if status != "rejected" {
		t.Errorf("取消後狀態應為 rejected，實際 %q", status)
	}
	if source != "counter" {
		t.Errorf("decision_source 應為 counter，實際 %q", source)
	}

	// 取消後不該再擋完工
	if err := CheckApprovalForStatus(100, "completed"); err != nil {
		t.Errorf("取消後不該擋完工: %v", err)
	}
	// 也不該加入任何項目
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM work_order_items WHERE work_order_id=100`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("取消不該加入項目，實際 %d 筆", n)
	}
}

// --- pendingApprovalRows ---

// item_summary 壞掉的列仍必須出現在頁面上：它同樣會擋住完工，
// 若因為解析不了就不顯示，櫃檯會看到提醒卻找不到任何列可取消。
func TestPendingApprovalRowsIncludesMalformed(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	ok, err := CreateApprovalRequest(100, 1, goodItems)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := CreateApprovalRequest(100, 1, "換機油") // 舊格式自由文字
	if err != nil {
		t.Fatal(err)
	}

	rows := pendingApprovalRows(100)
	if len(rows) != 2 {
		t.Fatalf("應回 2 筆未決授權，實際 %d（壞掉的那筆若被略過，櫃檯就無法取消它）", len(rows))
	}
	byID := map[int64]string{}
	for _, r := range rows {
		byID[r.ID] = r.Description
	}
	if byID[ok.ID] != "煞車油管" {
		t.Errorf("正常列的品名應被解析出來，實際 %q", byID[ok.ID])
	}
	if !strings.Contains(byID[bad.ID], "異常") {
		t.Errorf("壞掉的列應標示資料異常，實際 %q", byID[bad.ID])
	}

	// 已決定的不該出現在未決清單
	if _, err := d.Exec(`UPDATE approval_requests SET status='rejected' WHERE id=?`, ok.ID); err != nil {
		t.Fatal(err)
	}
	if rows := pendingApprovalRows(100); len(rows) != 1 || rows[0].ID != bad.ID {
		t.Errorf("已決定的授權不該出現在未決清單，實際 %d 筆", len(rows))
	}
}
