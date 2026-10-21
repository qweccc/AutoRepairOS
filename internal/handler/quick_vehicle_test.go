package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/session"
)

// 這批測試涵蓋「快速建立客戶與車輛」遇到重複車牌時的行為。
//
// 修復前：QuickVehicle 偵測到同車牌時直接回傳既有車輛並 ok:true，前端只看到
// 「車輛已帶入」——櫃檯不會發現自己可能打錯車牌，或正要把工單（含之後的收款）
// 開到別台車、別的車主身上。車牌是選車的唯一依據，選錯的代價很高。

func quickVehicleTestDB(t *testing.T) string {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	for _, s := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'owner', name TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1, created_at TEXT)`,
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id INTEGER NOT NULL,
			csrf_token TEXT NOT NULL DEFAULT '', created_at TEXT)`,
		`CREATE TABLE customers (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, phone TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id INTEGER,
			plate_number TEXT, brand_model TEXT, current_mileage INTEGER, deleted_at TEXT)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		// 既有資料：ABC-1234 屬於「王小明 / 0911222333」
		`INSERT INTO customers (id, name, phone) VALUES (1, '王小明', '0911222333')`,
		`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (1, 1, 'ABC-1234', 'Toyota')`,
	} {
		if _, err := db.DB.Exec(s); err != nil {
			t.Fatalf("setup failed: %v\n%s", err, s)
		}
	}
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active)
		VALUES (1, 'owner1', 'x', 'owner', '老闆', 1)`); err != nil {
		t.Fatal(err)
	}
	token, err := session.Start(1)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func postQuickVehicle(t *testing.T, token string, form url.Values) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/quick-vehicle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	QuickVehicle(rec, req)

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("回應不是 JSON: %v / %s", err, rec.Body.String())
	}
	return rec.Code, body
}

// 車牌已存在時必須明確標示 duplicate 並帶出車主，前端才能跳確認。
func TestQuickVehicleReportsDuplicateWithOwner(t *testing.T) {
	token := quickVehicleTestDB(t)

	code, body := postQuickVehicle(t, token, url.Values{
		"plate_number": {"ABC-1234"}, "brand_model": {"Toyota"},
		"customer_name": {"王小明"}, "customer_phone": {"0911222333"},
	})
	if code != http.StatusOK {
		t.Fatalf("應回 200，得到 %d", code)
	}
	if body["duplicate"] != true {
		t.Errorf("同車牌必須標示 duplicate:true（否則前端不會跳確認），實際 %v", body["duplicate"])
	}
	if body["owner_name"] != "王小明" {
		t.Errorf("應帶出現有車主姓名，實際 %v", body["owner_name"])
	}
	if body["owner_phone"] != "0911222333" {
		t.Errorf("應帶出現有車主電話，實際 %v", body["owner_phone"])
	}
	if id, ok := body["vehicle_id"].(float64); !ok || int64(id) != 1 {
		t.Errorf("應回既有車輛 id=1，實際 %v", body["vehicle_id"])
	}

	// 不可重複建立
	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM vehicles`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("同車牌不該再建一台車，實際 %d 台", n)
	}
}

// 車牌不存在時的行為不受影響（回歸），且不該誤標 duplicate。
func TestQuickVehicleCreatesNewWhenPlateFree(t *testing.T) {
	token := quickVehicleTestDB(t)

	code, body := postQuickVehicle(t, token, url.Values{
		"plate_number": {"NEW-9999"}, "brand_model": {"Honda"},
		"customer_name": {"陳小美"}, "customer_phone": {"0955666777"},
	})
	if code != http.StatusOK {
		t.Fatalf("應回 200，得到 %d", code)
	}
	if body["duplicate"] == true {
		t.Error("新車牌不該標示 duplicate")
	}
	if body["ok"] != true {
		t.Errorf("應成功建立，實際 %v", body)
	}

	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM vehicles`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("應建立第二台車，實際 %d 台", n)
	}

	// 新客戶也要建立
	var custCount int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM customers`).Scan(&custCount); err != nil {
		t.Fatal(err)
	}
	if custCount != 2 {
		t.Errorf("應建立第二個客戶，實際 %d 個", custCount)
	}
}

// 大小寫不同視為同一台車（車牌比對是 UPPER）。
func TestQuickVehicleDuplicateIsCaseInsensitive(t *testing.T) {
	token := quickVehicleTestDB(t)

	code, body := postQuickVehicle(t, token, url.Values{
		"plate_number": {"abc-1234"}, "brand_model": {"Toyota"},
		"customer_name": {"王小明"}, "customer_phone": {"0911222333"},
	})
	if code != http.StatusOK {
		t.Fatalf("應回 200，得到 %d", code)
	}
	if body["duplicate"] != true {
		t.Errorf("小寫車牌也該被視為重複（比對用 UPPER），實際 %v", body["duplicate"])
	}
}
