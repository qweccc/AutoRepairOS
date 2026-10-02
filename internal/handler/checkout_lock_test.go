package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// 這組測試涵蓋審查時發現的三個結帳缺陷：
//   P0-2 重複結帳 → 重複扣料、重複累計營收
//   P0-3 已結帳可回退狀態 → 庫存與營收沒有回滾
//   P0-4 已結帳仍可增刪項目 → 金額與已入帳營收分歧
//
// 全部用「先建立工單 → 一次結帳 → 嘗試二次操作」的形狀，
// 因為 bug 的本质是「第二次操作不該成功」。

// setupOrder 建立一張含零件項目的工單，回傳 woID 與零件 ID。
func setupOrder(t *testing.T, srv *httptest.Server, c *http.Client, plate string) (int64, int64) {
	t.Helper()

	doExec(t, `INSERT INTO customers (name, phone) VALUES (?, ?)`,
		"測試客戶-"+plate, "09"+plate)
	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone=?", &custID, "09"+plate)

	doExec(t, `INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?,?,?)`,
		custID, plate, "Toyota Vitz")
	var vehID int64
	doScan(t, "SELECT id FROM vehicles WHERE plate_number=?", &vehID, plate)

	doExec(t, `INSERT INTO work_orders (vehicle_id, customer_id, status, current_mileage, total_amount)
		VALUES (?, ?, 'pending', 5000, 0)`, vehID, custID)
	var woID int64
	doScan(t, "SELECT id FROM work_orders ORDER BY id DESC LIMIT 1", &woID)

	var invID int64
	doScan(t, "SELECT id FROM inventory_items WHERE part_number='0W20'", &invID)

	postForm(t, c, srv.URL+"/work-orders/"+strconv.FormatInt(woID, 10)+"/items", url.Values{
		"item_type":         {"part"},
		"inventory_item_id": {strconv.FormatInt(invID, 10)},
		"description":       {"機油"},
		"quantity":          {"2"},
		"unit_price":        {"980"},
	})

	return woID, invID
}

func postForm(t *testing.T, c *http.Client, u string, v url.Values) *http.Response {
	t.Helper()
	resp, err := c.PostForm(u, v)
	if err != nil {
		t.Fatalf("POST %s: %v", u, err)
	}
	resp.Body.Close()
	return resp
}

func stock(t *testing.T, invID int64) int {
	t.Helper()
	var n int
	doScan(t, "SELECT stock_quantity FROM inventory_items WHERE id=?", &n, invID)
	return n
}

// TestDoubleCheckoutIsIdempotent 重複結帳不得重複扣料或重複計營收。
func TestDoubleCheckoutIsIdempotent(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()
	c := authClient(t, srv)

	woID, invID := setupOrder(t, srv, c, "DBL-01")

	before := stock(t, invID)
	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone=?", &custID, "09DBL-01")
	var spent0 float64
	doScan(t, "SELECT COALESCE(total_spent,0) FROM customers WHERE id=?", &spent0, custID)

	// 第一次結帳：應該成功且扣料
	r1 := postForm(t, c, srv.URL+"/work-orders/"+strconv.FormatInt(woID, 10)+"/status",
		url.Values{"status": {"paid"}})
	if r1.StatusCode != http.StatusFound {
		t.Fatalf("first paid: got %d, want 302", r1.StatusCode)
	}
	afterFirst := stock(t, invID)
	if afterFirst != before-2 {
		t.Errorf("首次結帳扣料錯誤：%d → %d（應減 2）", before, afterFirst)
	}
	var spent1 float64
	doScan(t, "SELECT COALESCE(total_spent,0) FROM customers WHERE id=?", &spent1, custID)

	// 第二次結帳同一張單：不應再扣料，也不應再計營收
	r2 := postForm(t, c, srv.URL+"/work-orders/"+strconv.FormatInt(woID, 10)+"/status",
		url.Values{"status": {"paid"}})
	if r2.StatusCode != http.StatusFound {
		t.Fatalf("second paid should be idempotent 302, got %d", r2.StatusCode)
	}
	if got := stock(t, invID); got != afterFirst {
		t.Errorf("重複結帳又扣了一次庫存：%d → %d", afterFirst, got)
	}
	var spent2 float64
	doScan(t, "SELECT COALESCE(total_spent,0) FROM customers WHERE id=?", &spent2, custID)
	if spent2 != spent1 {
		t.Errorf("重複結帳又累計一次消費：%f → %f", spent1, spent2)
	}
}

// TestPaidOrderCannotRevert 已結帳工單不可回退狀態。
func TestPaidOrderCannotRevert(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()
	c := authClient(t, srv)

	woID, invID := setupOrder(t, srv, c, "BKW-01")
	postForm(t, c, srv.URL+"/work-orders/"+strconv.FormatInt(woID, 10)+"/status",
		url.Values{"status": {"paid"}})

	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone=?", &custID, "09BKW-01")
	paidStock := stock(t, invID)
	var paidSpent float64
	doScan(t, "SELECT COALESCE(total_spent,0) FROM customers WHERE id=?", &paidSpent, custID)

	r := postForm(t, c, srv.URL+"/work-orders/"+strconv.FormatInt(woID, 10)+"/status",
		url.Values{"status": {"in_progress"}})
	if r.StatusCode == http.StatusFound {
		t.Fatal("已結帳工單被允許回退為進行中")
	}

	var status string
	doScan(t, "SELECT status FROM work_orders WHERE id=?", &status, woID)
	if status != "paid" {
		t.Errorf("工單狀態被改成 %q，應維持 paid", status)
	}
	if got := stock(t, invID); got != paidStock {
		t.Errorf("回退後庫存被改動：%d → %d", paidStock, got)
	}
	var spent float64
	doScan(t, "SELECT COALESCE(total_spent,0) FROM customers WHERE id=?", &spent, custID)
	if spent != paidSpent {
		t.Errorf("回退後消費被改動：%f → %f", paidSpent, spent)
	}
}

// TestPaidOrderItemsLocked 已結帳工單不可再加或刪項目。
func TestPaidOrderItemsLocked(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()
	c := authClient(t, srv)

	woID, invID := setupOrder(t, srv, c, "ITM-01")
	base := "/work-orders/" + strconv.FormatInt(woID, 10)

	postForm(t, c, srv.URL+base+"/status", url.Values{"status": {"paid"}})

	// 加項目：應被拒
	add := postForm(t, c, srv.URL+base+"/items", url.Values{
		"item_type": {"labor"}, "description": {"事後加項"},
		"quantity": {"1"}, "unit_price": {"5000"},
	})
	if add.StatusCode == http.StatusFound {
		t.Error("已結帳工單仍可加入項目")
	}

	// 刪項目：應被拒，且庫存不可回補
	paidStock := stock(t, invID)
	var itemID int64
	doScan(t, "SELECT id FROM work_order_items WHERE work_order_id=?", &itemID, woID)

	del := postForm(t, c, srv.URL+base+"/items/"+strconv.FormatInt(itemID, 10)+"/delete", url.Values{})
	if del.StatusCode == http.StatusFound {
		t.Error("已結帳工單仍可刪除項目")
	}
	if got := stock(t, invID); got != paidStock {
		t.Errorf("刪除已結帳項目後庫存被回補：%d → %d", paidStock, got)
	}

	// 金額不應被改動
	var total float64
	doScan(t, "SELECT total_amount FROM work_orders WHERE id=?", &total, woID)
	if total != 1960 {
		t.Errorf("已結帳工單金額被改動：%f（應為 1960 = 2 x 980）", total)
	}
}
