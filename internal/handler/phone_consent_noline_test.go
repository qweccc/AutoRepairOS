package handler

import (
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// 這則測試是 code review 時發現的缺口所加的回歸測試。
//
// 缺口：CreateApprovalRequest 原本對「沒綁定 LINE」的客戶直接回錯誤
// （「客戶尚未綁定 LINE，無法產生授權連結」）。但沒綁 LINE 的客戶多半是
// 臨時進廠、從沒加過 LINE 的客人——而「客戶電話同意」正是為他們設計的：
// 既然推不了 LINE，就只能打電話。結果是：沒綁 LINE 就沒有授權紀錄，
// 也就無處可按「客戶電話同意」，電話同意形同不存在。
//
// 這則測試刻意把**整條路徑**走完（建立 → 擋完工 → 電話同意 → 項目入帳
// → 重算總額 → 不再擋完工），而不是只驗「建立函式沒回錯」——
// 一個功能常有多個獨立破口，驗收要看整條路徑能否走完。
func TestPhoneConsentWorksForCustomerWithoutLINE(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	const woID, custID = 101, 2 // setup 中「未綁定客戶」的工單

	if customerHasLINE(custID) {
		t.Fatal("前置條件錯誤：客戶 2 應該是未綁定 LINE")
	}

	// 1. 建立授權（過去這裡就失敗了）
	req, err := CreateApprovalRequest(woID, custID, goodItems)
	if err != nil {
		t.Fatalf("未綁 LINE 的客戶應能建立授權，否則電話同意無路可走: %v", err)
	}

	// 2. 建立後應擋住完工與結帳
	for _, target := range []string{"completed", "paid"} {
		if err := CheckApprovalForStatus(woID, target); err == nil {
			t.Errorf("有未決授權時 %s 應被擋下", target)
		}
	}

	// 3. 走電話同意（備註必填）
	rec := postPhoneApprove(t, woID, req.ID, url.Values{
		"note": {"與車主陳先生通話確認，同意更換煞車油管兩條"},
	})
	if rec.Code < 300 || rec.Code >= 400 {
		t.Fatalf("電話同意應為重導（3xx），實際 %d: %s", rec.Code, rec.Body.String())
	}

	// 4. 項目必須真的進了工單明細，且金額正確
	var desc, itemType string
	var qty int
	var unitPrice, subtotal float64
	err = d.QueryRow(`SELECT description, item_type, quantity, unit_price, subtotal
		FROM work_order_items WHERE work_order_id = ?`, woID).
		Scan(&desc, &itemType, &qty, &unitPrice, &subtotal)
	if err != nil {
		t.Fatalf("電話同意後應有工單項目（否則等於客戶同意了卻沒修）: %v", err)
	}
	if desc != "煞車油管" || itemType != "part" || qty != 2 || unitPrice != 500 {
		t.Errorf("項目內容不符：desc=%q type=%q qty=%d unit=%v", desc, itemType, qty, unitPrice)
	}
	if subtotal != 1000 {
		t.Errorf("小計應為 2×500=1000，實際 %v", subtotal)
	}

	// 5. 總額必須重算（漏了會讓項目進明細卻不算進 total_amount＝帳務漏收）
	var total float64
	if err := d.QueryRow(`SELECT total_amount FROM work_orders WHERE id = ?`, woID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 1000 {
		t.Errorf("工單總額應重算為 1000，實際 %v（漏算＝帳務漏收）", total)
	}

	// 6. 授權紀錄要留下「這個同意是電話來的」與備註
	var status, source, notes string
	if err := d.QueryRow(`SELECT status, decision_source, COALESCE(notes,'')
		FROM approval_requests WHERE id = ?`, req.ID).Scan(&status, &source, &notes); err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		t.Errorf("授權狀態應為 approved，實際 %q", status)
	}
	if source != "phone" {
		t.Errorf("decision_source 應為 phone（事後要分得出電話同意與客戶自己點連結），實際 %q", source)
	}
	if !strings.Contains(notes, "陳先生") {
		t.Errorf("備註應被保留（電話同意的證據），實際 %q", notes)
	}

	// 7. 不再擋完工
	if err := CheckApprovalForStatus(woID, "completed"); err != nil {
		t.Errorf("電話同意後不該再擋完工: %v", err)
	}

	// 8. 客戶端不該收到通知（沒綁 LINE 送不出去），
	//    但授權本身仍存在——這正是電話同意的依據。
	var pending int
	if err := d.QueryRow(`SELECT COUNT(*) FROM approval_requests
		WHERE work_order_id = ? AND status = 'pending'`, woID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Errorf("同意後不該還有 pending 的授權，實際 %d 筆", pending)
	}
}

// 反向確認：沒綁 LINE 的客戶建立授權後，若未取得同意仍必須擋住完工。
// 否則上面的放寬就變成「沒綁 LINE 的客戶可以繞過完工關卡」。
func TestNoLINECustomerStillBlockedWithoutConsent(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()
	d := setupApprovalTestDB(t)
	db.DB = d

	if _, err := CreateApprovalRequest(101, 2, goodItems); err != nil {
		t.Fatalf("未綁 LINE 的客戶應能建立授權: %v", err)
	}

	if err := CheckApprovalForStatus(101, "completed"); err == nil {
		t.Error("尚未取得同意的授權仍必須擋住完工——放寬建立條件不等於放寬關卡")
	}
}
