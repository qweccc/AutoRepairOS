package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// setupSupplierTestDB 建一個只有 suppliers 表的臨時 DB，隔離測試。
func setupSupplierTestDB(t *testing.T) {
	t.Helper()
	var err error
	db.DB, err = openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`CREATE TABLE IF NOT EXISTS suppliers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		phone TEXT,
		contact_person TEXT,
		address TEXT,
		notes TEXT,
		vendor_type TEXT,
		payment_terms TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		deleted_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`CREATE VIEW active_suppliers AS
		SELECT * FROM suppliers WHERE deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
}

func postQuickCreate(t *testing.T, name string) map[string]interface{} {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest(http.MethodPost, "/api/suppliers/quick-create", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	QuickCreateSupplier(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestQuickCreateSupplier 涵蓋：新建、同名重複回既有、相似名稱提醒、空名拒絕。
func TestQuickCreateSupplier(t *testing.T) {
	setupSupplierTestDB(t)

	// 1) 新建
	r1 := postQuickCreate(t, "鋒勝汽車材料行")
	if r1["ok"] != true || r1["existing"] != false {
		t.Fatalf("新建失敗：%v", r1)
	}
	id1 := r1["id"].(float64)

	// 2) 同名重複 → 回既有那筆（existing=true），不再建
	r2 := postQuickCreate(t, "鋒勝汽車材料行")
	if r2["existing"] != true || r2["id"].(float64) != id1 {
		t.Fatalf("同名應回既有：%v", r2)
	}

	// 3) 相似名稱（包含關係）→ 仍建立但帶 similar 提醒
	r3 := postQuickCreate(t, "鋒勝")
	if r3["existing"] != false {
		t.Fatalf("相似名應為新建：%v", r3)
	}
	if r3["similar"] != "鋒勝汽車材料行" {
		t.Fatalf("應提醒相似名稱，got %v", r3["similar"])
	}

	// 4) 空名 → 400
	body, _ := json.Marshal(map[string]string{"name": "  "})
	req := httptest.NewRequest(http.MethodPost, "/api/suppliers/quick-create", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	QuickCreateSupplier(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空名應 400，got %d", rec.Code)
	}

	// 5) 只建了 2 家（鋒勝汽車材料行、鋒勝），確認沒有重複建檔
	var count int
	db.DB.QueryRow(`SELECT COUNT(1) FROM suppliers`).Scan(&count)
	if count != 2 {
		t.Fatalf("供應商數應為 2，got %d", count)
	}
}

// TestQuickCreateSupplierMethodNotAllowed GET 應回 405。
func TestQuickCreateSupplierMethodNotAllowed(t *testing.T) {
	setupSupplierTestDB(t)
	req := httptest.NewRequest(http.MethodGet, "/api/suppliers/quick-create", nil)
	rec := httptest.NewRecorder()
	QuickCreateSupplier(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET 應 405，got %d", rec.Code)
	}
}
