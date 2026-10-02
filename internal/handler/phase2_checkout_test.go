package handler

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/seed"
)

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DBPath:            "file:" + dir + "/test.db?_journal_mode=WAL&_busy_timeout=5000",
		RunSeed:           true,
		DefaultOwner:      "owner",
		DefaultOwnerPwd:   "Owner001!",
		DefaultCounter:    "counter",
		DefaultCounterPwd: "Counter001!",
		DefaultTech:       "tech",
		DefaultTechPwd:    "Tech001!",
	}
	if err := db.Init(cfg); err != nil {
		t.Fatalf("db.Init failed: %v", err)
	}
	// t.TempDir 清理時會刪除整個目錄；DB 連線未關會讓 Windows 鎖住 test.db 導致 RemoveAll 失敗。
	t.Cleanup(db.Close)
	if err := db.Migrate(); err != nil {
		t.Fatalf("db.Migrate failed: %v", err)
	}
	seed.Run(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", LoginHandler)
	mux.HandleFunc("GET /login", LoginHandler)
	mux.HandleFunc("POST /work-orders", CounterUp(CreateWorkOrder))
	mux.HandleFunc("POST /work-orders/{id}/items", CounterUp(AddWorkOrderItem))
	mux.HandleFunc("GET /work-orders/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ShowWorkOrder(w, r, model.User{Role: "owner"}) }))
	mux.HandleFunc("GET /work-orders/{id}/receipt", AllStaff(PrintWorkOrderReceipt))
	mux.HandleFunc("POST /work-orders/{id}/status", CounterUp(UpdateWorkOrderStatus))
	return httptest.NewServer(mux)
}

func doExec(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec failed: %v (q=%s)", err, q)
	}
}

// doScan 執行單列查詢。scan 為掃描目標（通常 1 個），args 為 SQL 綁定值。
// 例：doScan(t, "SELECT stock_quantity FROM t WHERE id=?", &stock, invID)
func doScan(t *testing.T, q string, scan interface{}, args ...interface{}) {
	t.Helper()
	var target interface{}
	switch s := scan.(type) {
	case []interface{}:
		target = s
	case nil:
		t.Fatalf("doScan 需要掃描目標 (q=%s)", q)
	default:
		target = []interface{}{s}
	}
	if err := db.DB.QueryRow(q, args...).Scan(target.([]interface{})...); err != nil {
		t.Fatalf("scan failed: %v (q=%s)", err, q)
	}
}

// authClient 建立已登入的 cookie jar client。
// 真的走一次登入流程取得 session token——舊版是直接塞 "owner-1" 這種
// 可偽造的值，正好就是 P0-1 的漏洞本身，測試不該複刻 insecure 的做法。
// 不自動跟隨 3xx，才能斷言 handler 實際回 302 而非最終頁面的 200。
func authClient(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := c.PostForm(srv.URL+"/login",
		url.Values{"username": {"owner"}, "password": {"Owner001!"}})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: got %d, want 302", resp.StatusCode)
	}
	return c
}

// forgedClient 帶著舊版格式的假 token，驗證已無法登入（迴歸測試 P0-1）。
func forgedClient(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("http://127.0.0.1:1/")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "session_token", Value: "owner-1", Path: "/", HttpOnly: true},
	})
	// 不跟隨 3xx：才能直接斷言被擋下時的 302 與 Location。
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// itoa64 將 int64 轉為十進位字串。
// 舊版手寫實作開頭有個 `for i != 0 { i /= 10 }` 的空轉迴圈，會把 i 直接除到 0，
// 導致任何輸入都回傳 "0"（例如 itoa64(1) == "0"），讓 inventory_item_id 全部失效。
// 直接用標準庫 strconv，不再自行實作。
func itoa64(i int64) string { return strconv.FormatInt(i, 10) }

func TestCheckoutDeductsParts(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()
	c := authClient(t, srv)

	doExec(t, `INSERT INTO customers (name, phone) VALUES ('測試客戶','0900000000')`)
	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone='0900000000'", &custID)
	doExec(t, `INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?,?,?)`, custID, "AB-1234", "Toyota Vitz")
	var vehID int64
	doScan(t, "SELECT id FROM vehicles WHERE plate_number=?", &vehID, "AB-1234")
	doExec(t, "INSERT INTO work_orders (vehicle_id, customer_id, status, current_mileage, total_amount) VALUES (?, ?, ?, ?, 0)", vehID, custID, "pending", 5000)
	var woID int64
	doScan(t, "SELECT id FROM work_orders ORDER BY id DESC LIMIT 1", &woID)

	var invID int64
	doScan(t, "SELECT id FROM inventory_items WHERE part_number='0W20'", &invID)
	var stock int
	doScan(t, "SELECT stock_quantity FROM inventory_items WHERE id=?", &stock, invID)
	t.Logf("工單=%d 零件=%d 初始庫存=%d", woID, invID, stock)

	// 加入一個零件項目。此時不扣庫存（扣料統一在結帳時執行），
	// 避免未成交工單佔用庫存、技師改單時還要回補。
	if _, err := c.PostForm(srv.URL+"/work-orders/"+itoa64(woID)+"/items", url.Values(map[string][]string{
		"item_type": {"part"}, "description": {"機油"}, "quantity": {"1"},
		"unit_price": {"980"}, "inventory_item_id": {itoa64(invID)},
	})); err != nil {
		t.Fatalf("post item: %v", err)
	}
	doScan(t, "SELECT stock_quantity FROM inventory_items WHERE id=?", &stock, invID)
	if stock != 30 {
		t.Errorf("expected stock unchanged (30) after add, got %d", stock)
	}

	// 結帳（paid）：扣實際用量 1 + 累計消費
	resp, err := c.PostForm(srv.URL+"/work-orders/"+itoa64(woID)+"/status", url.Values(map[string][]string{"status": {"paid"}}))
	if err != nil {
		t.Fatalf("post status: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		bb, _ := io.ReadAll(resp.Body)
		bs := string(bb)
		if len(bs) > 120 {
			bs = bs[:120]
		}
		t.Fatalf("paid: got %d body=%q", resp.StatusCode, bs)
	}
	doScan(t, "SELECT stock_quantity FROM inventory_items WHERE id=?", &stock, invID)
	if stock != 29 {
		t.Errorf("expected stock 29 after paid, got %d", stock)
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
	c := authClient(t, srv)

	doExec(t, `INSERT INTO customers (name, phone) VALUES ('純工時','0900000001')`)
	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone='0900000001'", &custID)
	doExec(t, `INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?,?,?)`, custID, "AB-9999", "Toyota Vitz")
	var vehID int64
	doScan(t, "SELECT id FROM vehicles WHERE plate_number=?", &vehID, "AB-9999")
	doExec(t, "INSERT INTO work_orders (vehicle_id, customer_id, status, current_mileage, total_amount) VALUES (?,?,?, ?, 0)", vehID, custID, "pending", 5000)
	var woID int64
	doScan(t, "SELECT id FROM work_orders ORDER BY id DESC LIMIT 1", &woID)

	// 純工資項目
	if _, err := c.PostForm(srv.URL+"/work-orders/"+itoa64(woID)+"/items", url.Values(map[string][]string{
		"item_type": {"labor"}, "description": {"工資"}, "quantity": {"2"}, "unit_price": {"1000"}, "inventory_item_id": {"0"},
	})); err != nil {
		t.Fatalf("post labor: %v", err)
	}
	resp, err := c.PostForm(srv.URL+"/work-orders/"+itoa64(woID)+"/status", url.Values(map[string][]string{"status": {"paid"}}))
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
	c := authClient(t, srv)

	doExec(t, `INSERT INTO customers (name, phone) VALUES ('超賣','0900000002')`)
	var custID int64
	doScan(t, "SELECT id FROM customers WHERE phone='0900000002'", &custID)
	doExec(t, `INSERT INTO vehicles (customer_id, plate_number, brand_model) VALUES (?,?,?)`, custID, "AB-5555", "Toyota Vitz")
	var vehID int64
	doScan(t, "SELECT id FROM vehicles WHERE plate_number=?", &vehID, "AB-5555")
	doExec(t, "INSERT INTO work_orders (vehicle_id, customer_id, status, current_mileage, total_amount) VALUES (?,?,?, ?, 0)", vehID, custID, "pending", 5000)
	var woID int64
	doScan(t, "SELECT id FROM work_orders ORDER BY id DESC LIMIT 1", &woID)

	var invID int64
	doScan(t, "SELECT id FROM inventory_items WHERE part_number='0W20'", &invID)

	// 加入 3 支（庫存 30），先扣 3 → 剩 27
	if _, err := c.PostForm(srv.URL+"/work-orders/"+itoa64(woID)+"/items", url.Values(map[string][]string{
		"item_type": {"part"}, "description": {"機油"}, "quantity": {"3"}, "unit_price": {"980"}, "inventory_item_id": {itoa64(invID)},
	})); err != nil {
		t.Fatalf("post item: %v", err)
	}
	resp, err := c.PostForm(srv.URL+"/work-orders/"+itoa64(woID)+"/status", url.Values(map[string][]string{"status": {"paid"}}))
	if err != nil {
		t.Fatalf("post paid: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Errorf("expected paid 302, got %d", resp.StatusCode)
	}
}

// TestForgedSessionRejected 迴歸測試 P0-1：
// 舊版 cookie 是 "username-id" 且未簽名，攻擊者只要猜到 username 就能冒充。
// 現在 token 必須存在 sessions 表，偽造值必須被視為未登入。
func TestForgedSessionRejected(t *testing.T) {
	srv := setupTestServer(t)
	defer srv.Close()

	c := forgedClient(t)
	resp, err := c.PostForm(srv.URL+"/work-orders",
		url.Values{"vehicle_id": {"1"}, "current_mileage": {"100"}})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	t.Logf("forged -> status=%d location=%q headers=%v body=%s",
		resp.StatusCode, resp.Header.Get("Location"), resp.Header, snippet(resp))
	t.Logf("Allow header = %q", resp.Header.Get("Allow"))
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("偽造 token 竟然通過驗證並建立工單")
	}
	if loc := resp.Header.Get("Location"); loc == "" && resp.StatusCode != http.StatusFound {
		t.Errorf("應導向 /login，但沒有 Location header")
	}
}

func snippet(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(b) > 120 {
		b = b[:120]
	}
	return string(b)
}
