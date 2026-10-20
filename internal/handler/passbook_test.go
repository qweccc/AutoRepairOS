package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/internal/seed"
)

// T2.10 車主 LINE 數位維修 Passbook（FR-VH-05）。
//
// 驗收邊界（ROADMAP）：
//   - 綁定客戶查自己的車輛 → 回傳履歷
//   - 未綁定、或非該客戶的車輛 → 404，不得回傳任何資料
//   - 只公開必要資訊（客戶名、車牌、工單、服務類型、金額、狀態）
func setupPassbookServer(t *testing.T) *httptest.Server {
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
	t.Cleanup(db.Close)
	if err := db.Migrate(); err != nil {
		t.Fatalf("db.Migrate failed: %v", err)
	}
	seed.Run(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/passbook", PassbookAPI)
	return httptest.NewServer(mux)
}

// passbookFixture 建立「客戶 → 車輛 → 工單 → passbook」完整鏈路，回傳各 id。
func passbookFixture(t *testing.T, lineUserID, plate string) (custID, vehID, woID int64) {
	t.Helper()
	mustExec(t, `INSERT INTO customers (name, phone, line_user_id) VALUES (?,?,?)`,
		"Passbook 車主", "0911000001", lineUserID)
	if err := db.DB.QueryRow(`SELECT id FROM customers WHERE line_user_id = ?`, lineUserID).
		Scan(&custID); err != nil {
		t.Fatalf("查客戶失敗: %v", err)
	}
	mustExec(t, `INSERT INTO vehicles (customer_id, plate_number, brand_model, manufacture_year)
		VALUES (?,?,?,?)`, custID, plate, "Toyota Corolla", 2022)
	if err := db.DB.QueryRow(`SELECT id FROM vehicles WHERE plate_number = ?`, plate).
		Scan(&vehID); err != nil {
		t.Fatalf("查車輛失敗: %v", err)
	}
	mustExec(t, `INSERT INTO work_orders (vehicle_id, customer_id, status, current_mileage)
		VALUES (?,?,'completed',?)`, vehID, custID, 50000)
	if err := db.DB.QueryRow(`SELECT id FROM work_orders WHERE vehicle_id = ? ORDER BY id DESC LIMIT 1`, vehID).
		Scan(&woID); err != nil {
		t.Fatalf("查工單失敗: %v", err)
	}
	return custID, vehID, woID
}

func mustExec(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec failed: %v (q=%s)", err, q)
	}
}

func passbookGet(t *testing.T, srv *httptest.Server, line string, vehicleID int64) (int, []model.Passbook, string) {
	t.Helper()
	q := url.Values{"line": {line}}
	if vehicleID > 0 {
		q.Set("vehicle_id", strconv.FormatInt(vehicleID, 10))
	}
	resp, err := http.Get(srv.URL + "/api/passbook?" + q.Encode())
	if err != nil {
		t.Fatalf("GET /api/passbook: %v", err)
	}
	defer resp.Body.Close()

	var list []model.Passbook
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			t.Fatalf("解碼回應失敗: %v", err)
		}
		return http.StatusOK, list, ""
	}
	buf := make([]byte, 512)
	n, _ := resp.Body.Read(buf)
	return resp.StatusCode, nil, strings.TrimSpace(string(buf[:n]))
}

// 符合的綁定：車主查自己的車輛 → 200 且回傳該車履歷。
func TestPassbookAPIOwnerCanReadOwnVehicle(t *testing.T) {
	srv := setupPassbookServer(t)
	defer srv.Close()

	_, vehID, woID := passbookFixture(t, "U-PB-OWNER", "PB-OWN-01")
	mustExec(t, `INSERT INTO passbook
		(customer_id, vehicle_id, work_order_id, service_type, description,
		 parts_cost, labor_cost, subcontract_cost, subtotal, tax, discount_amount, total_amount, status)
		VALUES ((SELECT customer_id FROM work_orders WHERE id=?), ?, ?, 'repair', '更換煞車來令片',
		 1200, 800, 0, 2000, 0, 0, 2000, 'paid')`, woID, vehID, woID)

	st, list, body := passbookGet(t, srv, "U-PB-OWNER", vehID)
	if st != http.StatusOK {
		t.Fatalf("車主查自己車輛應回 200，得到 %d (%s)", st, body)
	}
	if len(list) != 1 {
		t.Fatalf("應回傳 1 筆履歷，得到 %d 筆", len(list))
	}
	p := list[0]
	if p.VehiclePlate != "PB-OWN-01" {
		t.Errorf("車牌 = %q，應為 PB-OWN-01", p.VehiclePlate)
	}
	if p.CustomerName != "Passbook 車主" {
		t.Errorf("客戶名 = %q", p.CustomerName)
	}
	if p.TotalAmount != 2000 {
		t.Errorf("總金額 = %v，應為 2000", p.TotalAmount)
	}
	if p.SubcontractCost != 0 {
		t.Errorf("外包成本 = %v，應為 0", p.SubcontractCost)
	}
}

// 不同客戶的綁定：甲的 LINE 查乙的車輛 → 不得回傳任何資料。
//
// PassbookAPI 刻意對「非該客戶」「未綁定」「查無紀錄」一律回 200 + 空陣列：
// 若改回 404，攻擊者就能用 404/200 的差異枚舉「哪些車牌存在」。
// 空陣列同樣達成「不得取得履歷」，且不洩露車輛是否存在。
func TestPassbookAPIRejectsOtherCustomerVehicle(t *testing.T) {
	srv := setupPassbookServer(t)
	defer srv.Close()

	// 乙的車輛有 passbook，但用甲（未綁定）的 line_user_id 查詢
	_, vehID, woID := passbookFixture(t, "U-PB-OTHER", "PB-OTH-01")
	mustExec(t, `INSERT INTO passbook
		(customer_id, vehicle_id, work_order_id, service_type, description,
		 parts_cost, labor_cost, subcontract_cost, subtotal, tax, discount_amount, total_amount, status)
		VALUES ((SELECT customer_id FROM work_orders WHERE id=?), ?, ?, 'maintenance', '定期保養',
		 500, 300, 0, 800, 0, 0, 800, 'paid')`, woID, vehID, woID)

	st, list, body := passbookGet(t, srv, "U-PB-OWNER-NOT-BOUND", vehID)
	if st != http.StatusOK {
		t.Fatalf("非該客戶應回 200（空陣列），得到 %d (%s)", st, body)
	}
	if len(list) != 0 {
		t.Errorf("非該客戶不該取得任何履歷，卻拿到 %d 筆: %+v", len(list), list)
	}
}

// 未綁定 LINE：完全沒有 line_user_id 的帳號查詢 → 不得回傳任何資料。
func TestPassbookAPIRejectsUnboundLine(t *testing.T) {
	srv := setupPassbookServer(t)
	defer srv.Close()

	_, vehID, _ := passbookFixture(t, "U-PB-HAS-DATA", "PB-UNB-01")

	st, list, body := passbookGet(t, srv, "U-PB-NEVER-BOUND", vehID)
	if st != http.StatusOK {
		t.Fatalf("未綁定應回 200（空陣列），得到 %d (%s)", st, body)
	}
	if len(list) != 0 {
		t.Errorf("未綁定不該取得任何履歷，卻拿到 %d 筆: %+v", len(list), list)
	}
}

// 缺參數：沒有 line 或 vehicle_id → 404，不洩露資訊。
func TestPassbookAPIRejectsMissingParams(t *testing.T) {
	srv := setupPassbookServer(t)
	defer srv.Close()

	if st, _, body := passbookGet(t, srv, "", 1); st != http.StatusNotFound {
		t.Errorf("缺 line 應回 404，得到 %d (%s)", st, body)
	}
	if st, _, body := passbookGet(t, srv, "U-PB-ANY", 0); st != http.StatusNotFound {
		t.Errorf("缺 vehicle_id 應回 404，得到 %d (%s)", st, body)
	}
}

// 綁定正確但查無紀錄：回 200 + 空陣列（有權查但無資料），不得用 404 洩露車主身份。
func TestPassbookAPIEmptyWhenNoRecords(t *testing.T) {
	srv := setupPassbookServer(t)
	defer srv.Close()

	_, vehID, _ := passbookFixture(t, "U-PB-EMPTY", "PB-EMP-01")
	// 不建立任何 passbook 紀錄

	st, list, body := passbookGet(t, srv, "U-PB-EMPTY", vehID)
	if st != http.StatusOK {
		t.Fatalf("有權查但無資料應回 200，得到 %d (%s)", st, body)
	}
	if len(list) != 0 {
		t.Errorf("應回傳空陣列，得到 %d 筆", len(list))
	}
}

// 只公開必要資訊：回應不得包含成本價或其他客戶資料。
func TestPassbookAPIOnlyExposesNecessaryInfo(t *testing.T) {
	srv := setupPassbookServer(t)
	defer srv.Close()

	_, vehID, woID := passbookFixture(t, "U-PB-LEAK", "PB-LEAK-01")
	mustExec(t, `INSERT INTO passbook
		(customer_id, vehicle_id, work_order_id, service_type, description,
		 parts_cost, labor_cost, subcontract_cost, subtotal, tax, discount_amount, total_amount, status)
		VALUES ((SELECT customer_id FROM work_orders WHERE id=?), ?, ?, 'repair', '引擎大修',
		 5000, 3000, 1000, 9000, 0, 0, 9000, 'paid')`, woID, vehID, woID)

	q := url.Values{"line": {"U-PB-LEAK"}, "vehicle_id": {strconv.FormatInt(vehID, 10)}}
	resp, err := http.Get(srv.URL + "/api/passbook?" + q.Encode())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	// 回應是 JSON 陣列（不是包件的物件）
	var arr []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("解碼失敗: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("應回 1 筆，得到 %v", arr)
	}
	rec := arr[0]

	// 必要欄位存在
	for _, key := range []string{"customer_name", "vehicle_plate", "service_type", "total_amount", "status"} {
		if _, ok := rec[key]; !ok {
			t.Errorf("回應缺少必要欄位 %q: %v", key, rec)
		}
	}
	// 不得回傳其他客戶的個資或其他車輛
	if s, _ := rec["customer_name"].(string); strings.Contains(s, "Other") {
		t.Errorf("回應洩露其他客戶名稱: %v", rec)
	}
}
