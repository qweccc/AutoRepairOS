package handler

import (
	"net/http"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// FR-FIN-02：把工單直接改成「已結帳」時，必須已有足以覆蓋應收的收款紀錄
// （含賒帳）。不足要擋下並指向收款流程；補足（即使全額賒帳）才放行。
func TestCheckoutRequiresReceiptRecord(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id,name,phone) VALUES (1,'收款測試','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id,customer_id,plate_number,brand_model) VALUES (1,1,'TEST-RCV','測試車')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount,current_mileage,is_insurance,deductible_amount,discount_amount,is_warranty,claim_amount,claim_date,claim_note,quoted_total_amount,quoted_discount_amount)
		VALUES (1,1,1,'completed',100,1000,0,0,0,0,0,'','','',0)`); err != nil {
		t.Fatal(err)
	}

	w := postWorkOrderStatus(t, 1, "paid")
	if w.Code == http.StatusFound {
		t.Fatalf("沒有收款紀錄卻能直接結帳（code=%d）", w.Code)
	}
	if !strings.Contains(w.Body.String(), "尚未足以結帳") {
		t.Fatalf("錯誤訊息沒有引導先開收款單：%s", w.Body.String())
	}

	// 用「賒帳」把應收掛上（外交/月結情境），就應該可以結帳。
	if _, err := db.DB.Exec(`INSERT INTO receivables (voucher_no,customer_id,work_order_id,pay_date,pay_total,credit_amount,voided,source)
		VALUES ('RCV-TEST-1',1,1,'2026-11-18',100,100,0,'manual')`); err != nil {
		t.Fatal(err)
	}
	w = postWorkOrderStatus(t, 1, "paid")
	if w.Code != http.StatusFound {
		t.Fatalf("有足額收款（賒帳）仍無法結帳 code=%d body=%s", w.Code, w.Body.String())
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM work_orders WHERE id=1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "paid" {
		t.Fatalf("結帳後狀態=%s", status)
	}
}
