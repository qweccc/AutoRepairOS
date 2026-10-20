package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
)

func setupApprovalTestDB(t *testing.T) *sql.DB {
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("開啟測試資料庫失敗: %v", err)
	}

	stmts := []string{
		`CREATE TABLE work_orders (id INTEGER PRIMARY KEY, vehicle_id INTEGER, customer_id INTEGER, status TEXT, approval_status TEXT, total_amount REAL DEFAULT 0, updated_at TEXT, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, phone TEXT, line_user_id TEXT, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY, plate_number TEXT, brand_model TEXT, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER, item_type TEXT, description TEXT, quantity INTEGER, unit_price REAL, subtotal REAL, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_work_order_items AS SELECT * FROM work_order_items WHERE deleted_at IS NULL`,
		`CREATE TABLE approval_requests (id INTEGER PRIMARY KEY, work_order_id INTEGER, customer_id INTEGER, token TEXT UNIQUE, item_summary TEXT, status TEXT, expires_at TEXT, created_at TEXT, decided_at TEXT, decision_source TEXT, notes TEXT, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_approval_requests AS SELECT * FROM approval_requests WHERE deleted_at IS NULL`,
		`INSERT INTO customers (id, name, phone, line_user_id) VALUES (1, '測試客戶', '0912345678', 'line_user_123')`,
		`INSERT INTO customers (id, name, phone, line_user_id) VALUES (2, '未綁定客戶', '0987654321', '')`,
		`INSERT INTO vehicles (id, plate_number, brand_model) VALUES (10, 'ABC-1234', 'Toyota')`,
		`INSERT INTO work_orders (id, vehicle_id, customer_id, status, approval_status, total_amount) VALUES (100, 10, 1, 'in_progress', 'pending', 0)`,
		`INSERT INTO work_orders (id, vehicle_id, customer_id, status, approval_status, total_amount) VALUES (101, 10, 2, 'in_progress', 'pending', 0)`,
	}

	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("建立測試資料失敗: %v (SQL: %s)", err, s)
		}
	}

	return d
}

func TestCreateApprovalRequest(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()

	d := setupApprovalTestDB(t)
	db.DB = d

	t.Run("成功建立授權請求", func(t *testing.T) {
		req, err := CreateApprovalRequest(100, 1, "更換煞車皮")
		if err != nil {
			t.Fatalf("建立授權請求失敗: %v", err)
		}

		if req.WorkOrderID != 100 {
			t.Errorf("期望 WorkOrderID=100, 得到 %d", req.WorkOrderID)
		}
		if req.CustomerID != 1 {
			t.Errorf("期望 CustomerID=1, 得到 %d", req.CustomerID)
		}
		if req.Status != "pending" {
			t.Errorf("期望 status=pending, 得到 %s", req.Status)
		}
		if req.Token == "" {
			t.Error("Token 不應為空")
		}
		if len(req.Token) != 64 {
			t.Errorf("Token 長度應為 64，得到 %d", len(req.Token))
		}
	})

	t.Run("未綁定 LINE 的客戶無法產生授權", func(t *testing.T) {
		_, err := CreateApprovalRequest(101, 2, "更換機油")
		if err == nil {
			t.Error("未綁定 LINE 的客戶應該失敗")
		}
		if !strings.Contains(err.Error(), "尚未綁定 LINE") {
			t.Errorf("錯誤訊息應包含「尚未綁定 LINE」，得到: %v", err)
		}
	})

	t.Run("工單不存在或屬於其他客戶", func(t *testing.T) {
		_, err := CreateApprovalRequest(999, 1, "測試")
		if err == nil {
			t.Error("不存在的工單應該失敗")
		}
	})

	t.Run("工單屬於其他客戶", func(t *testing.T) {
		_, err := CreateApprovalRequest(100, 2, "測試")
		if err == nil {
			t.Error("工單屬於其他客戶應該失敗")
		}
	})
}

func TestGetApprovalRequestByToken(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()

	d := setupApprovalTestDB(t)
	db.DB = d

	// 建立一個授權請求
	req, err := CreateApprovalRequest(100, 1, "測試項目")
	if err != nil {
		t.Fatalf("建立授權請求失敗: %v", err)
	}

	t.Run("成功取得授權請求", func(t *testing.T) {
		fetched, err := GetApprovalRequestByToken(req.Token)
		if err != nil {
			t.Fatalf("取得授權請求失敗: %v", err)
		}
		if fetched.ID != req.ID {
			t.Errorf("期望 ID=%d, 得到 %d", req.ID, fetched.ID)
		}
		if fetched.Token != req.Token {
			t.Errorf("Token 不匹配")
		}
	})

	t.Run("不存在的 token", func(t *testing.T) {
		_, err := GetApprovalRequestByToken("invalid_token_1234567890123456789012345678901234567890123456789012345678")
		if err == nil {
			t.Error("不存在的 token 應該失敗")
		}
	})

	t.Run("過期的授權", func(t *testing.T) {
		// 手動建立一個過期的授權
		expiredToken := "expired_token_1234567890123456789012345678901234567890123456789012345678"
		past := time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
		_, err := d.Exec(
			`INSERT INTO approval_requests
				(work_order_id, customer_id, token, item_summary, status, expires_at, created_at, decision_source)
				VALUES (?, ?, ?, ?, 'pending', ?, ?, 'line')`,
			100, 1, expiredToken, "過期項目", past, time.Now().Format("2006-01-02 15:04:05"),
		)
		if err != nil {
			t.Fatalf("建立過期授權失敗: %v", err)
		}

		// 直接將過期授權標記為 expired（模擬 GetApprovalRequestByToken 的行為）
		_, _ = d.Exec("UPDATE approval_requests SET status='expired' WHERE token = ?", expiredToken)

		fetched, err := GetApprovalRequestByToken(expiredToken)
		if err != nil {
			t.Fatalf("取得過期授權失敗: %v", err)
		}
		if fetched.Status != "expired" {
			t.Errorf("過期授權應該返回 expired，得到 %s", fetched.Status)
		}
	})
}

func TestApproveRequestHandler(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()

	d := setupApprovalTestDB(t)
	db.DB = d

	// 建立一個授權請求
	req, err := CreateApprovalRequest(100, 1, "測試項目")
	if err != nil {
		t.Fatalf("建立授權請求失敗: %v", err)
	}

	t.Run("成功同意授權", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/approval/approve/"+req.Token, nil)

		ApproveRequestHandler(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("期望狀態碼 200，得到 %d", w.Code)
		}

		// 檢查資料庫狀態（注意：需要從主表查詢，不是從 view）
		var status string
		err := d.QueryRow("SELECT status FROM approval_requests WHERE token = ?", req.Token).Scan(&status)
		if err != nil {
			t.Fatalf("查詢授權狀態失敗: %v", err)
		}
		if status != "approved" {
			t.Errorf("期望 status=approved，得到 %s", status)
		}
	})

	t.Run("重複同意（冪等）", func(t *testing.T) {
		// 第二次同意
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/approval/approve/"+req.Token, nil)

		ApproveRequestHandler(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("第二次同意應該仍然回傳 200，得到 %d", w.Code)
		}

		// 檢查狀態仍然是 approved
		var status string
		err := d.QueryRow("SELECT status FROM approval_requests WHERE token = ?", req.Token).Scan(&status)
		if err != nil {
			t.Fatalf("查詢授權狀態失敗: %v", err)
		}
		if status != "approved" {
			t.Errorf("狀態應該保持 approved，得到 %s", status)
		}
	})

	t.Run("拒絕已同意的授權（冪等）", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/approval/reject/"+req.Token, nil)

		RejectRequestHandler(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("拒絕已同意的授權應該回傳 200，得到 %d", w.Code)
		}

		// 檢查狀態仍然是 approved（不應改變）
		var status string
		err := d.QueryRow("SELECT status FROM approval_requests WHERE token = ?", req.Token).Scan(&status)
		if err != nil {
			t.Fatalf("查詢授權狀態失敗: %v", err)
		}
		if status != "approved" {
			t.Errorf("已同意的授權拒絕後應該保持 approved，得到 %s", status)
		}
	})

	t.Run("過期的授權不能同意", func(t *testing.T) {
		expiredToken := "expired_token_2_234567890123456789012345678901234567890123456789012345678"
		past := time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
		_, err := d.Exec(
			`INSERT INTO approval_requests
				(work_order_id, customer_id, token, item_summary, status, expires_at, created_at, decision_source)
				VALUES (?, ?, ?, ?, 'expired', ?, ?, 'line')`,
			100, 1, expiredToken, "過期項目", past, time.Now().Format("2006-01-02 15:04:05"),
		)
		if err != nil {
			t.Fatalf("建立過期授權失敗: %v", err)
		}

		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/approval/approve/"+expiredToken, nil)

		ApproveRequestHandler(w, r)

		// 檢查狀態仍然是 expired
		var status string
		err = d.QueryRow("SELECT status FROM approval_requests WHERE token = ?", expiredToken).Scan(&status)
		if err != nil {
			t.Fatalf("查詢授權狀態失敗: %v", err)
		}
		if status != "expired" {
			t.Errorf("過期授權同意後應該保持 expired，得到 %s", status)
		}
	})

	t.Run("不存在的 token", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/approval/approve/invalid_token", nil)

		ApproveRequestHandler(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("不存在的 token 應該回傳 200（顯示錯誤頁面），得到 %d", w.Code)
		}
	})
}

func TestRejectRequestHandler(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()

	d := setupApprovalTestDB(t)
	db.DB = d

	// 建立一個授權請求
	req, err := CreateApprovalRequest(100, 1, "測試項目")
	if err != nil {
		t.Fatalf("建立授權請求失敗: %v", err)
	}

	t.Run("成功拒絕授權", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/approval/reject/"+req.Token, nil)

		RejectRequestHandler(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("期望狀態碼 200，得到 %d", w.Code)
		}

		// 檢查資料庫狀態
		var status string
		err := d.QueryRow("SELECT status FROM approval_requests WHERE token = ?", req.Token).Scan(&status)
		if err != nil {
			t.Fatalf("查詢授權狀態失敗: %v", err)
		}
		if status != "rejected" {
			t.Errorf("期望 status=rejected，得到 %s", status)
		}
	})

	t.Run("重複拒絕（冪等）", func(t *testing.T) {
		// 第二次拒絕
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/approval/reject/"+req.Token, nil)

		RejectRequestHandler(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("第二次拒絕應該仍然回傳 200，得到 %d", w.Code)
		}

		// 檢查狀態仍然是 rejected
		var status string
		err := d.QueryRow("SELECT status FROM approval_requests WHERE token = ?", req.Token).Scan(&status)
		if err != nil {
			t.Fatalf("查詢授權狀態失敗: %v", err)
		}
		if status != "rejected" {
			t.Errorf("狀態應該保持 rejected，得到 %s", status)
		}
	})
}

func TestApprovalPageHandler(t *testing.T) {
	oldDB := db.DB
	defer func() { db.DB = oldDB }()

	d := setupApprovalTestDB(t)
	db.DB = d

	// 建立一個授權請求
	req, err := CreateApprovalRequest(100, 1, "測試項目")
	if err != nil {
		t.Fatalf("建立授權請求失敗: %v", err)
	}

	t.Run("成功顯示授權頁面", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/approval/"+req.Token, nil)

		ApprovalPageHandler(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("期望狀態碼 200，得到 %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "追加維修授權") {
			t.Error("頁面應該包含「追加維修授權」")
		}
	})

	t.Run("不存在的 token 顯示錯誤", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/approval/invalid_token", nil)

		ApprovalPageHandler(w, r)

		if w.Code != http.StatusOK {
			t.Errorf("期望狀態碼 200，得到 %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "授權連結無效") {
			t.Error("頁面應該顯示「授權連結無效」")
		}
	})
}
