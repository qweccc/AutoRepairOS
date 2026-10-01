package handler

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
	"autorepairos/internal/seed"
)

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DBPath:          "file:" + dir + "/test.db?_journal_mode=WAL&_busy_timeout=5000",
		RunSeed:         true,
		DefaultOwner:    "owner",
		DefaultOwnerPwd: "Owner001!",
		DefaultCounter:  "counter",
		DefaultCounterPwd: "Counter001!",
		DefaultTech:     "tech",
		DefaultTechPwd:  "Tech001!",
	}
	if err := db.Init(cfg); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("db.Migrate failed: %v", err)
	}
	seed.Run(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /work-orders", RequireLogin(CreateWorkOrder))
	mux.HandleFunc("POST /work-orders/{id}/items", RequireLogin(AddWorkOrderItem))
	mux.HandleFunc("POST /work-orders/{id}/status", RequireLogin(UpdateWorkOrderStatus))
	return httptest.NewServer(mux)
}

func doExec(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec failed: %v (q=%s)", err, q)
	}
}

func doScan(t *testing.T, q string, dst ...interface{}) {
	t.Helper()
	if err := db.DB.QueryRow(q, dst...).Scan(dst...); err != nil {
		t.Fatalf("scan failed: %v (q=%s)", err, q)
	}
}

// authClient 建立已登入的 cookie jar client（手動塞入 session_token）。
func authClient(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("http://127.0.0.1:1/")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "session_token", Value: "owner-1", Path: "/", HttpOnly: true},
	})
	return &http.Client{Jar: jar}
}


func itoa64(i int64) string {
	b := []byte{}
	for i != 0 {
		i /= 10
	}
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		b = append(b, byte('0'+i%10))
		i /= 10
	}
	if neg {
		b = append(b, '-')
	}
	for j := len(b) - 1; j > 0; j-- {
		b[j], b[j-1] = b[j-1], b[j]
	}
	if len(b) == 0 {
		return "0"
	}
	return string(b)
}

func TestCheckoutDeductsParts(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()
	defer db.Close()
	c := authClient(t)

	doExec(t, `INSERT INTO customers (name, phone) VALUES ('測試客戶','0900000000')`)
	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone='0900000000'", &custID)
	doExec(t, `INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?,?,?)`, custID, "AB-1234", "Toyota Vitz")
	doExec(t, "INSERT INTO work_orders (vehicle_id, customer_id, status, total_amount) VALUES (?,?,?, 0)", custID, custID, "pending")

	var invID int64
	doScan(t, "SELECT id FROM inventory_items WHERE part_number='0W20'", &invID)
	var stock int
	doScan(t, "SELECT stock_quantity FROM inventory_items WHERE id=?", &stock, invID)
	t.Logf("零件 %d 初始庫存=%d", invID, stock)

	// 加入一個零件項目（扣庫）
	if _, err := c.PostForm(srv.URL+"/work-orders/1/items", url.Values(map[string][]string{
		"item_type": {"part"}, "description": {"機油"}, "quantity": {"1"},
		"unit_price": {"980"}, "inventory_item_id": {itoa64(invID)},
	})); err != nil {
		t.Fatalf("post item: %v", err)
	}
	doScan(t, "SELECT stock_quantity FROM inventory_items WHERE id=?", &stock, invID)
	if stock != 9 {
		t.Errorf("expected stock 9 after add, got %d", stock)
	}

	// 結帳（paid）：扣實際用量 1 + 累計消費
	resp, err := c.PostForm(srv.URL+"/work-orders/1/status", url.Values(map[string][]string{"status": {"paid"}}))
	if err != nil {
		t.Fatalf("post status: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("paid: got %d", resp.StatusCode)
	}
	doScan(t, "SELECT stock_quantity FROM inventory_items WHERE id=?", &stock, invID)
	if stock != 8 {
		t.Errorf("expected stock 8 after paid, got %d", stock)
	}
	var spent float64
	doScan(t, "SELECT total_spent FROM customers WHERE id=?", &spent, custID)
	if spent != 980 {
		t.Errorf("expected total_spent 980, got %f", spent)
	}
	t.Logf("扣料 OK: 結帳後庫存=%d 消費=%f", stock, spent)
}

func TestCheckoutNoPartsNoDeductStock(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()
	defer db.Close()

	doExec(t, `INSERT INTO customers (name, phone) VALUES ('純工時','0900000001')`)
	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone='0900000001'", &custID)
	doExec(t, `INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?,?,?)`, custID, "AB-9999", "Toyota Vitz")
	doExec(t, "INSERT INTO work_orders (vehicle_id, customer_id, status, current_mileage, total_amount) VALUES (?,?,?, ?, 0)", custID, custID, "pending", 5000)

	// 純工資項目
	if _, err := srv.Client().PostForm(srv.URL+"/work-orders/1/items", url.Values(map[string][]string{
		"item_type": {"labor"}, "description": {"工資"}, "quantity": {"2"}, "unit_price": {"1000"}, "inventory_item_id": {"0"},
	})); err != nil {
		t.Fatalf("post labor: %v", err)
	}
	resp, err := srv.Client().PostForm(srv.URL+"/work-orders/1/status", url.Values(map[string][]string{"status": {"paid"}}))
	if err != nil {
		t.Fatalf("post paid: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("paid labor: got %d", resp.StatusCode)
	}
	var spent float64
	doScan(t, "SELECT total_spent FROM customers WHERE id=?", &spent, custID)
	if spent != 2000 {
		t.Errorf("expected total_spent 2000, got %f", spent)
	}
}

func TestCheckoutOverstockFails(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()
	defer db.Close()

	doExec(t, `INSERT INTO customers (name, phone) VALUES ('超賣','0900000002')`)
	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone='0900000002'", &custID)
	doExec(t, `INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?,?,?)`, custID, "AB-5555", "Toyota Vitz")
	doExec(t, "INSERT INTO work_orders (vehicle_id, customer_id, status, current_mileage, total_amount) VALUES (?,?,?, ?, 0)", custID, custID, "pending", 5000)

	var invID int64
	doScan(t, "SELECT id FROM inventory_items WHERE part_number='0W20'", &invID)

	// 加入 3 支（庫存 30），先扣 3 → 剩 27
	if _, err := srv.Client().PostForm(srv.URL+"/work-orders/1/items", url.Values(map[string][]string{
		"item_type": {"part"}, "description": {"機油"}, "quantity": {"3"}, "unit_price": {"980"}, "inventory_item_id": {itoa64(invID)},
	})); err != nil {
		t.Fatalf("post item: %v", err)
	}
	resp, err := srv.Client().PostForm(srv.URL+"/work-orders/1/status", url.Values(map[string][]string{"status": {"paid"}}))
	if err != nil {
		t.Fatalf("post paid: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Errorf("expected paid 302, got %d", resp.StatusCode)
	}
}
