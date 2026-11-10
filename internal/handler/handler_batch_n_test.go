package handler

// Batch N：approvals.go / suppliers.go / subcontracts.go Coverage。
// 只新增此測試檔，不修改 production 或其他測試檔。
// 復用既有 harness：newMigratedTestDB、postReq、mustExec、itoa64。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ============================================================
// 共用 seed / assert helpers（batchN 前綴，避免與平行工作者衝突）
// ============================================================

func batchNCount(t *testing.T, query string, args ...interface{}) int {
	t.Helper()
	var n int
	if err := db.DB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func batchNSeedApprovalBase(t *testing.T) {
	t.Helper()
	mustExec(t, `INSERT INTO customers (id, name, phone, line_user_id) VALUES (1,'王小明','0912345678','line_u1')`)
	mustExec(t, `INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (1,1,'ABC-1234','TOYOTA')`)
	mustExec(t, `INSERT INTO work_orders (id, customer_id, vehicle_id, status, current_mileage, total_amount) VALUES (100,1,1,'in_progress',50000,0)`)
}

func batchNApprovalForm() url.Values {
	return url.Values{
		"description": {"追加工資"},
		"quantity":    {"1"},
		"unit_price":  {"500"},
		"item_type":   {"labor"},
	}
}

func batchNPostApprovalFromWO(t *testing.T, id string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := postReq(t, "/work-orders/"+id+"/approval-request", form)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	CreateApprovalFromWorkOrder(w, r)
	return w
}

func batchNInsertApproval(t *testing.T, token, status, expiresAt string, woID, custID int64, decidedAt, notes interface{}) {
	t.Helper()
	mustExec(t, `INSERT INTO approval_requests
		(work_order_id, customer_id, token, item_summary, status, expires_at, created_at, decided_at, decision_source, notes)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		woID, custID, token,
		`[{"description":"工資","quantity":1,"unit_price":500,"item_type":"labor"}]`,
		status, expiresAt, time.Now().Format("2006-01-02 15:04:05"), decidedAt, "line", notes)
}

// ============================================================
// approvals.go
// ============================================================

func TestBatchNApprovalFromWorkOrder(t *testing.T) {
	t.Run("LINE 客戶建立授權並推播", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		w := batchNPostApprovalFromWO(t, "100", batchNApprovalForm())
		if w.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if loc := w.Header().Get("Location"); !strings.Contains(loc, "msg=") {
			t.Fatalf("Location=%q", loc)
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM approval_requests WHERE work_order_id=100`); n != 1 {
			t.Fatalf("approval_requests=%d want 1", n)
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM notifications WHERE work_order_id=100 AND kind='approval_request'`); n != 1 {
			t.Fatalf("notifications=%d want 1", n)
		}
	})

	t.Run("未綁定 LINE 仍建立授權但不推播", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		mustExec(t, `UPDATE customers SET line_user_id=NULL WHERE id=1`)
		w := batchNPostApprovalFromWO(t, "100", batchNApprovalForm())
		if w.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		loc, _ := url.QueryUnescape(w.Header().Get("Location"))
		if !strings.Contains(loc, "未綁定 LINE") {
			t.Fatalf("Location=%q", loc)
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM approval_requests WHERE work_order_id=100`); n != 1 {
			t.Fatalf("approval_requests=%d want 1", n)
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM notifications WHERE work_order_id=100`); n != 0 {
			t.Fatalf("notifications=%d want 0", n)
		}
	})

	t.Run("已結帳工單不可追加", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		mustExec(t, `UPDATE work_orders SET status='paid' WHERE id=100`)
		w := batchNPostApprovalFromWO(t, "100", batchNApprovalForm())
		if w.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
			t.Fatalf("Location=%q", loc)
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM approval_requests`); n != 0 {
			t.Fatalf("approval_requests=%d want 0", n)
		}
	})

	t.Run("工單不存在回 404", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		w := batchNPostApprovalFromWO(t, "999", batchNApprovalForm())
		if w.Code != http.StatusNotFound {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("表單驗證失敗回錯誤訊息", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		form := batchNApprovalForm()
		form.Set("description", "")
		w := batchNPostApprovalFromWO(t, "100", form)
		if w.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
			t.Fatalf("Location=%q", loc)
		}
	})

	t.Run("建立授權寫入失敗回錯誤訊息", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		mustExec(t, `DROP TABLE approval_requests`)
		w := batchNPostApprovalFromWO(t, "100", batchNApprovalForm())
		if w.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if loc := w.Header().Get("Location"); !strings.Contains(loc, "err=") {
			t.Fatalf("Location=%q", loc)
		}
	})

	t.Run("通知進佇列失敗仍以成功訊息回導", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		mustExec(t, `DROP TABLE notifications`)
		w := batchNPostApprovalFromWO(t, "100", batchNApprovalForm())
		if w.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		loc, _ := url.QueryUnescape(w.Header().Get("Location"))
		if !strings.Contains(loc, "msg=") || !strings.Contains(loc, "通知客戶失敗") {
			t.Fatalf("Location=%q", loc)
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM approval_requests WHERE work_order_id=100`); n != 1 {
			t.Fatalf("approval_requests=%d want 1", n)
		}
	})
}

func TestBatchNApprovalRequestErrors(t *testing.T) {
	t.Run("客戶不存在", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		_, err := CreateApprovalRequest(100, 9999, "x")
		if err == nil || !strings.Contains(err.Error(), "客戶不存在") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("客戶查詢失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		mustExec(t, `DROP VIEW active_customers`)
		_, err := CreateApprovalRequest(100, 1, "x")
		if err == nil || !strings.Contains(err.Error(), "查詢客戶失敗") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("工單不存在或不屬於該客戶", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		_, err := CreateApprovalRequest(9999, 1, "x")
		if err == nil || !strings.Contains(err.Error(), "工單不存在或不屬於該客戶") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("工單查詢失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		mustExec(t, `DROP VIEW active_work_orders`)
		_, err := CreateApprovalRequest(100, 1, "x")
		if err == nil || !strings.Contains(err.Error(), "查詢工單失敗") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("寫入授權失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		mustExec(t, `DROP TABLE approval_requests`)
		_, err := CreateApprovalRequest(100, 1, "x")
		if err == nil || !strings.Contains(err.Error(), "建立授權請求失敗") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("成功建立", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		req, err := CreateApprovalRequest(100, 1, "x")
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if req.ID <= 0 || req.Token == "" || req.Status != "pending" || req.WorkOrderID != 100 || req.CustomerID != 1 {
			t.Fatalf("req=%+v", req)
		}
	})
}

func TestBatchNApprovalTokenBranches(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")

	t.Run("token 不存在", func(t *testing.T) {
		newMigratedTestDB(t)
		_, err := GetApprovalRequestByToken("batch-n-no-such-token")
		if err == nil || !strings.Contains(err.Error(), "授權連結無效") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("查詢失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `DROP TABLE approval_requests`)
		if _, err := GetApprovalRequestByToken("batch-n-x"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("notes 與 decided_at 有效值", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		batchNInsertApproval(t, "batch-n-notes", "approved", future, 100, 1, "2026-01-01 10:00:00", "客戶同意")
		got, err := GetApprovalRequestByToken("batch-n-notes")
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if got.DecidedAt != "2026-01-01 10:00:00" || got.Notes != "客戶同意" {
			t.Fatalf("decided=%q notes=%q", got.DecidedAt, got.Notes)
		}
	})

	t.Run("過期時間格式錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		batchNInsertApproval(t, "batch-n-badtime", "pending", "not-a-time", 100, 1, nil, nil)
		if _, err := GetApprovalRequestByToken("batch-n-badtime"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("自動標記過期", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		past := time.Now().Add(-48 * time.Hour).Format("2006-01-02 15:04:05")
		batchNInsertApproval(t, "batch-n-expired", "pending", past, 100, 1, nil, nil)
		got, err := GetApprovalRequestByToken("batch-n-expired")
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if got.Status != "expired" {
			t.Fatalf("status=%q", got.Status)
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM approval_requests WHERE token='batch-n-expired' AND status='expired'`); n != 1 {
			t.Fatalf("db status not expired: %d", n)
		}
	})

	t.Run("合法待決授權", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedApprovalBase(t)
		batchNInsertApproval(t, "batch-n-pending", "pending", future, 100, 1, nil, nil)
		got, err := GetApprovalRequestByToken("batch-n-pending")
		if err != nil {
			t.Fatalf("err=%v", err)
		}
		if got.Status != "pending" || got.ID <= 0 {
			t.Fatalf("got=%+v", got)
		}
	})
}

func TestBatchNApprovalPageHandler(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	cases := []struct {
		name      string
		token     string
		status    string
		expiresAt string
		woID      int64
		custID    int64
		drop      string
		seedItem  bool
		wantCode  int
		wantBody  string
		wantItem  string
	}{
		{"空白 token", "", "", "", 0, 0, "", false, 400, "無效的授權連結", ""},
		{"無效 token", "batch-n-bad", "", "", 0, 0, "", false, 200, "授權連結無效", ""},
		{"已同意", "batch-n-approved", "approved", future, 100, 1, "", false, 200, "已同意", ""},
		{"已拒絕", "batch-n-rejected", "rejected", future, 100, 1, "", false, 200, "已拒絕", ""},
		{"已過期", "batch-n-expired", "expired", future, 100, 1, "", false, 200, "已過期", ""},
		{"狀態異常", "batch-n-weird", "bogus", future, 100, 1, "", false, 200, "授權狀態異常", ""},
		{"工單載入失敗", "batch-n-nowo", "pending", future, 9999, 1, "", false, 200, "工單資訊載入失敗", ""},
		{"客戶載入失敗", "batch-n-nocust", "pending", future, 100, 9999, "", false, 200, "客戶資訊載入失敗", ""},
		{"項目載入失敗仍渲染", "batch-n-noitems", "pending", future, 100, 1, "work_order_items", false, 200, "追加維修授權", ""},
		{"待決頁面附工單項目", "batch-n-pending", "pending", future, 100, 1, "", true, 200, "追加維修授權", "工資"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newMigratedTestDB(t)
			batchNSeedApprovalBase(t)
			if c.expiresAt != "" {
				batchNInsertApproval(t, c.token, c.status, c.expiresAt, c.woID, c.custID, nil, nil)
			}
			if c.seedItem {
				mustExec(t, `INSERT INTO work_order_items (work_order_id, item_type, description, quantity, unit_price, subtotal) VALUES (100,'labor','工資',1,500,500)`)
			}
			if c.drop != "" {
				mustExec(t, `DROP TABLE `+c.drop)
			}
			w := httptest.NewRecorder()
			ApprovalPageHandler(w, httptest.NewRequest("GET", "/approval/"+c.token, nil))
			if w.Code != c.wantCode {
				t.Fatalf("code=%d want %d body=%s", w.Code, c.wantCode, w.Body.String())
			}
			body := w.Body.String()
			if !strings.Contains(body, c.wantBody) {
				t.Fatalf("body should contain %q: %s", c.wantBody, body)
			}
			if c.wantItem != "" && !strings.Contains(body, c.wantItem) {
				t.Fatalf("body should contain item %q: %s", c.wantItem, body)
			}
		})
	}
}

func TestBatchNCustomerHasLINEMissing(t *testing.T) {
	newMigratedTestDB(t)
	batchNSeedApprovalBase(t)
	if !customerHasLINE(1) {
		t.Fatal("customer 1 should have LINE")
	}
	if customerHasLINE(9999) {
		t.Fatal("missing customer should not have LINE")
	}
}

// ============================================================
// suppliers.go
// ============================================================

func batchNSeedSuppliers(t *testing.T) {
	t.Helper()
	mustExec(t, `INSERT INTO suppliers (id, name, phone, contact_person, address, notes, vendor_type, payment_terms)
		VALUES (1,'材料商甲','0912','聯絡人A','地址A','備註A','material','check30')`)
	mustExec(t, `INSERT INTO suppliers (id, name, vendor_type, payment_terms) VALUES (2,'協力廠乙','vendor','monthly')`)
	mustExec(t, `INSERT INTO suppliers (id, name, vendor_type, created_at) VALUES (3,'時間缺失商','material',NULL)`)
	mustExec(t, `INSERT INTO suppliers (id, name, vendor_type, deleted_at) VALUES (4,'已停用商','material','2026-01-01 00:00:00')`)
}

func TestBatchNSupplierListSearch(t *testing.T) {
	owner := model.User{ID: 1, Role: "owner"}

	t.Run("列表含資料並跳過掃描失敗列", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSuppliers(t)
		w := httptest.NewRecorder()
		ListSuppliers(w, httptest.NewRequest("GET", "/suppliers", nil), owner)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		for _, want := range []string{"材料商甲", "協力廠乙"} {
			if !strings.Contains(body, want) {
				t.Fatalf("body should contain %q: %s", want, body)
			}
		}
		for _, notWant := range []string{"時間缺失商", "已停用商"} {
			if strings.Contains(body, notWant) {
				t.Fatalf("body should not contain %q: %s", notWant, body)
			}
		}
	})

	t.Run("列表 q 篩選", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSuppliers(t)
		w := httptest.NewRecorder()
		ListSuppliers(w, httptest.NewRequest("GET", "/suppliers?q=材料", nil), owner)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "材料商甲") || strings.Contains(body, "協力廠乙") {
			t.Fatalf("q filter body=%s", body)
		}
	})

	t.Run("列表 type 篩選", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSuppliers(t)
		w := httptest.NewRecorder()
		ListSuppliers(w, httptest.NewRequest("GET", "/suppliers?type=vendor", nil), owner)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "協力廠乙") || strings.Contains(body, "材料商甲") {
			t.Fatalf("type filter body=%s", body)
		}
	})

	t.Run("搜尋 q", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSuppliers(t)
		w := httptest.NewRecorder()
		SearchSuppliersAPI(w, httptest.NewRequest("GET", "/suppliers/search?q=材料", nil))
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "材料商甲") || strings.Contains(body, "協力廠乙") {
			t.Fatalf("search q body=%s", body)
		}
	})

	t.Run("搜尋 type", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSuppliers(t)
		w := httptest.NewRecorder()
		SearchSuppliersAPI(w, httptest.NewRequest("GET", "/suppliers/search?type=material", nil))
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "材料商甲") || strings.Contains(body, "協力廠乙") {
			t.Fatalf("search type body=%s", body)
		}
	})

	t.Run("列表查詢失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSuppliers(t)
		mustExec(t, `DROP VIEW active_suppliers`)
		w := httptest.NewRecorder()
		ListSuppliers(w, httptest.NewRequest("GET", "/suppliers", nil), owner)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("搜尋查詢失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSuppliers(t)
		mustExec(t, `DROP VIEW active_suppliers`)
		w := httptest.NewRecorder()
		SearchSuppliersAPI(w, httptest.NewRequest("GET", "/suppliers/search?q=x", nil))
		if w.Code != 500 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestBatchNSupplierCreateUpdate(t *testing.T) {
	owner := model.User{ID: 1, Role: "owner"}

	t.Run("新增缺名稱", func(t *testing.T) {
		newMigratedTestDB(t)
		w := httptest.NewRecorder()
		CreateSupplier(w, postReq(t, "/suppliers", url.Values{"name": {""}}))
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("新增成功", func(t *testing.T) {
		newMigratedTestDB(t)
		w := httptest.NewRecorder()
		CreateSupplier(w, postReq(t, "/suppliers", url.Values{"name": {"新廠商"}, "vendor_type": {"vendor"}, "payment_terms": {"check30"}}))
		if w.Code != 302 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM suppliers WHERE name='新廠商' AND vendor_type='vendor'`); n != 1 {
			t.Fatalf("suppliers=%d want 1", n)
		}
	})

	t.Run("新增寫入失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `DROP TABLE suppliers`)
		w := httptest.NewRecorder()
		CreateSupplier(w, postReq(t, "/suppliers", url.Values{"name": {"新廠商"}}))
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("編輯表單 200", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'廠商甲')`)
		req := httptest.NewRequest("GET", "/suppliers/1/edit", nil)
		req.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		EditSupplierForm(w, req, owner)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("編輯表單 404", func(t *testing.T) {
		newMigratedTestDB(t)
		req := httptest.NewRequest("GET", "/suppliers/999/edit", nil)
		req.SetPathValue("id", "999")
		w := httptest.NewRecorder()
		EditSupplierForm(w, req, owner)
		if w.Code != 404 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("更新成功", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'廠商甲')`)
		req := postReq(t, "/suppliers/1/update", url.Values{"name": {"廠商乙"}, "vendor_type": {"vendor"}})
		req.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		UpdateSupplier(w, req)
		if w.Code != 302 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		var name string
		if err := db.DB.QueryRow(`SELECT name FROM suppliers WHERE id=1`).Scan(&name); err != nil || name != "廠商乙" {
			t.Fatalf("name=%q err=%v", name, err)
		}
	})

	t.Run("更新寫入失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `DROP TABLE suppliers`)
		req := postReq(t, "/suppliers/1/update", url.Values{"name": {"廠商乙"}})
		req.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		UpdateSupplier(w, req)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func batchNQuickJSON(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/suppliers/quick-create", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	QuickCreateSupplier(w, req)
	return w
}

func batchNQuickHX(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := postReq(t, "/api/suppliers/quick-create", form)
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	QuickCreateSupplier(w, req)
	return w
}

func TestBatchNQuickCreateSupplierPaths(t *testing.T) {
	t.Run("JSON 格式錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		if w := batchNQuickJSON(t, "not json"); w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("JSON 空名稱", func(t *testing.T) {
		newMigratedTestDB(t)
		if w := batchNQuickJSON(t, `{"name":"  "}`); w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("JSON 新增", func(t *testing.T) {
		newMigratedTestDB(t)
		w := batchNQuickJSON(t, `{"name":"JSON新廠"}`)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		var out map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("json: %v body=%s", err, w.Body.String())
		}
		if out["ok"] != true || out["existing"] != false {
			t.Fatalf("out=%v", out)
		}
	})

	t.Run("JSON 既有", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'既有廠')`)
		w := batchNQuickJSON(t, `{"name":"既有廠"}`)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		var out map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("json: %v body=%s", err, w.Body.String())
		}
		if out["ok"] != true || out["existing"] != true {
			t.Fatalf("out=%v", out)
		}
	})

	t.Run("JSON 相似名稱", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'鋒勝汽車材料行')`)
		w := batchNQuickJSON(t, `{"name":"鋒勝"}`)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		var out map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("json: %v body=%s", err, w.Body.String())
		}
		if out["similar"] != "鋒勝汽車材料行" {
			t.Fatalf("out=%v", out)
		}
	})

	t.Run("JSON 寫入失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `DROP TABLE suppliers`)
		if w := batchNQuickJSON(t, `{"name":"新廠"}`); w.Code != 500 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("HX 空名稱", func(t *testing.T) {
		newMigratedTestDB(t)
		w := batchNQuickHX(t, url.Values{"name": {"  "}})
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "supplierQuickCreateResult") {
			t.Fatalf("fragment body=%s", w.Body.String())
		}
	})

	t.Run("HX 既有供應商", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'既有廠')`)
		w := batchNQuickHX(t, url.Values{"name": {"既有廠"}})
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "既有廠") {
			t.Fatalf("fragment body=%s", w.Body.String())
		}
	})

	t.Run("HX 新增供應商", func(t *testing.T) {
		newMigratedTestDB(t)
		w := batchNQuickHX(t, url.Values{"name": {"HX新廠"}})
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "HX新廠") {
			t.Fatalf("fragment body=%s", w.Body.String())
		}
	})

	t.Run("HX 相似名稱提醒", func(t *testing.T) {
		newMigratedTestDB(t)
		mustExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'鋒勝汽車材料行')`)
		w := batchNQuickHX(t, url.Values{"name": {"鋒勝"}})
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "類似名稱") {
			t.Fatalf("fragment body=%s", w.Body.String())
		}
	})
}

// ============================================================
// subcontracts.go
// ============================================================

func batchNSeedSubcontractBase(t *testing.T) {
	t.Helper()
	mustExec(t, `INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912345678')`)
	mustExec(t, `INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (1,1,'ABC-1234','TOYOTA')`)
	mustExec(t, `INSERT INTO work_orders (id, customer_id, vehicle_id, status, current_mileage, total_amount) VALUES (100,1,1,'in_progress',50000,0)`)
	mustExec(t, `INSERT INTO suppliers (id, name, payment_terms) VALUES (1,'協力廠商甲','check30')`)
}

func batchNInsertSubcontract(t *testing.T, woID, supplierID int64, cost float64, status string) int64 {
	t.Helper()
	var sup interface{}
	if supplierID > 0 {
		sup = supplierID
	}
	res, err := db.DB.Exec(`INSERT INTO subcontracts
		(work_order_id, supplier_id, description, cost_amount, charge_amount, status)
		VALUES (?,?,?,?,?,?)`, woID, sup, "外包加工", cost, cost+300, status)
	if err != nil {
		t.Fatalf("insert subcontract: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func batchNInsertAPPayable(t *testing.T, sourceID, supplierID int64, amount float64) {
	t.Helper()
	mustExec(t, `INSERT INTO ap_payables
		(source_type, source_id, supplier_id, amount, paid_amount, balance, due_date, status)
		VALUES ('subcontract',?,?,?,0,?,?,'open')`, sourceID, supplierID, amount, amount, "2999-01-01")
}

func TestBatchNSubcontractCreate(t *testing.T) {
	t.Run("缺少工單 ID", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		w := httptest.NewRecorder()
		CreateSubcontract(w, postReq(t, "/subcontracts", url.Values{"description": {"外包"}}))
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("工單不存在", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		w := httptest.NewRecorder()
		CreateSubcontract(w, postReq(t, "/subcontracts", url.Values{"work_order_id": {"9999"}, "description": {"外包"}}))
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("缺少加工內容", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		w := httptest.NewRecorder()
		CreateSubcontract(w, postReq(t, "/subcontracts", url.Values{"work_order_id": {"100"}, "description": {""}}))
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("成功建立並產生應付帳款", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		form := url.Values{
			"work_order_id": {"100"}, "supplier_id": {"1"}, "description": {"外包加工"},
			"cost_amount": {"500"}, "charge_amount": {"800"}, "status": {"pending"},
		}
		w := httptest.NewRecorder()
		CreateSubcontract(w, postReq(t, "/subcontracts", form))
		if w.Code != 302 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM subcontracts WHERE work_order_id=100`); n != 1 {
			t.Fatalf("subcontracts=%d want 1", n)
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM ap_payables WHERE source_type='subcontract' AND amount=500`); n != 1 {
			t.Fatalf("ap_payables=%d want 1", n)
		}
	})

	t.Run("成功建立不產生應付帳款", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		form := url.Values{
			"work_order_id": {"100"}, "supplier_id": {"0"}, "description": {"外包加工"},
			"cost_amount": {"0"}, "status": {"pending"},
		}
		w := httptest.NewRecorder()
		CreateSubcontract(w, postReq(t, "/subcontracts", form))
		if w.Code != 302 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM ap_payables`); n != 0 {
			t.Fatalf("ap_payables=%d want 0", n)
		}
	})

	t.Run("寫入失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		mustExec(t, `DROP TABLE subcontracts`)
		form := url.Values{"work_order_id": {"100"}, "description": {"外包"}}
		w := httptest.NewRecorder()
		CreateSubcontract(w, postReq(t, "/subcontracts", form))
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("建立應付帳款失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		mustExec(t, `DROP TABLE ap_payables`)
		form := url.Values{"work_order_id": {"100"}, "supplier_id": {"1"}, "description": {"外包"}, "cost_amount": {"500"}}
		w := httptest.NewRecorder()
		CreateSubcontract(w, postReq(t, "/subcontracts", form))
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("提交失敗（deferred FK）", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		// defer_foreign_keys 會被 database/sql 回收連線時清空（countRows 先跑過），
		// 改用 schema 層的 DEFERRABLE INITIALLY DEFERRED，才能讓 COMMIT 真的失敗。
		mustExec(t, `PRAGMA foreign_keys=ON`)
		mustExec(t, `DROP VIEW IF EXISTS active_subcontracts`)
		mustExec(t, `ALTER TABLE subcontracts RENAME TO subcontracts_batch_n_orig`)
		mustExec(t, `CREATE TABLE subcontracts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			work_order_id INTEGER NOT NULL,
			supplier_id INTEGER,
			description TEXT NOT NULL,
			cost_amount REAL DEFAULT 0,
			charge_amount REAL DEFAULT 0,
			status TEXT DEFAULT 'pending',
			expected_date DATE,
			completed_date DATE,
			notes TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			deleted_at TEXT,
			FOREIGN KEY (work_order_id) REFERENCES work_orders(id) DEFERRABLE INITIALLY DEFERRED,
			FOREIGN KEY (supplier_id) REFERENCES suppliers(id) DEFERRABLE INITIALLY DEFERRED
		)`)
		mustExec(t, `CREATE VIEW active_subcontracts AS SELECT * FROM subcontracts WHERE deleted_at IS NULL`)
		form := url.Values{"work_order_id": {"100"}, "supplier_id": {"9999"}, "description": {"外包"}, "cost_amount": {"0"}}
		w := httptest.NewRecorder()
		CreateSubcontract(w, postReq(t, "/subcontracts", form))
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM subcontracts`); n != 0 {
			t.Fatalf("subcontracts=%d want 0", n)
		}
	})
}

func TestBatchNSubcontractUpdate(t *testing.T) {
	t.Run("缺少必填", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 1, 500, "pending")
		req := postReq(t, "/subcontracts/"+itoa64(sid)+"/update", url.Values{"description": {""}})
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("讀取應付帳款失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 1, 500, "pending")
		mustExec(t, `DROP TABLE ap_payables`)
		req := postReq(t, "/subcontracts/"+itoa64(sid)+"/update", url.Values{"work_order_id": {"100"}, "description": {"外包"}})
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("已成立應付帳款不可改供應商或成本", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 1, 500, "pending")
		batchNInsertAPPayable(t, sid, 1, 500)
		req := postReq(t, "/subcontracts/"+itoa64(sid)+"/update", url.Values{
			"work_order_id": {"100"}, "supplier_id": {"1"}, "description": {"外包"}, "cost_amount": {"600"},
		})
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("更新寫入失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 1, 500, "pending")
		mustExec(t, `DROP TABLE subcontracts`)
		req := postReq(t, "/subcontracts/"+itoa64(sid)+"/update", url.Values{"work_order_id": {"100"}, "description": {"外包"}})
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("找不到外包工項", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		req := postReq(t, "/subcontracts/999/update", url.Values{"work_order_id": {"100"}, "description": {"外包"}})
		req.SetPathValue("id", "999")
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 404 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("更新時補建應付帳款", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 0, 0, "pending")
		req := postReq(t, "/subcontracts/"+itoa64(sid)+"/update", url.Values{
			"work_order_id": {"100"}, "supplier_id": {"1"}, "description": {"外包"}, "cost_amount": {"600"},
		})
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 302 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM ap_payables WHERE source_type='subcontract' AND source_id=? AND amount=600`, sid); n != 1 {
			t.Fatalf("ap_payables=%d want 1", n)
		}
	})

	t.Run("補建應付帳款失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 0, 0, "pending")
		mustExec(t, `CREATE TRIGGER batch_n_ap_abort BEFORE INSERT ON ap_payables BEGIN SELECT RAISE(ABORT,'batch_n_ap_abort'); END`)
		req := postReq(t, "/subcontracts/"+itoa64(sid)+"/update", url.Values{
			"work_order_id": {"100"}, "supplier_id": {"1"}, "description": {"外包"}, "cost_amount": {"600"},
		})
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("提交失敗（deferred FK）", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 0, 0, "pending")
		mustExec(t, `PRAGMA foreign_keys=ON`)
		mustExec(t, `PRAGMA defer_foreign_keys=ON`)
		req := postReq(t, "/subcontracts/"+itoa64(sid)+"/update", url.Values{
			"work_order_id": {"100"}, "supplier_id": {"9999"}, "description": {"外包"}, "cost_amount": {"0"},
		})
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM subcontracts WHERE id=? AND supplier_id IS NULL`, sid); n != 1 {
			t.Fatalf("supplier_id should stay NULL: %d", n)
		}
	})

	t.Run("成功更新並補完成日期", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 1, 500, "pending")
		batchNInsertAPPayable(t, sid, 1, 500)
		req := postReq(t, "/subcontracts/"+itoa64(sid)+"/update", url.Values{
			"work_order_id": {"100"}, "supplier_id": {"1"}, "description": {"修改外包"},
			"cost_amount": {"500"}, "status": {"completed"},
		})
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		UpdateSubcontract(w, req)
		if w.Code != 302 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		var desc, completed string
		if err := db.DB.QueryRow(`SELECT description, completed_date FROM subcontracts WHERE id=?`, sid).Scan(&desc, &completed); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if desc != "修改外包" {
			t.Fatalf("desc=%q", desc)
		}
		if !strings.HasPrefix(completed, today()) {
			t.Fatalf("completed=%q want prefix %q", completed, today())
		}
	})
}

func TestBatchNSubcontractDelete(t *testing.T) {
	t.Run("找不到", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		req := httptest.NewRequest("POST", "/subcontracts/999/delete", nil)
		req.SetPathValue("id", "999")
		w := httptest.NewRecorder()
		DeleteSubcontract(w, req)
		if w.Code != 404 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("刪除寫入失敗", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		mustExec(t, `DROP TABLE subcontracts`)
		req := httptest.NewRequest("POST", "/subcontracts/1/delete", nil)
		req.SetPathValue("id", "1")
		w := httptest.NewRecorder()
		DeleteSubcontract(w, req)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("成功刪除", func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		sid := batchNInsertSubcontract(t, 100, 1, 500, "pending")
		req := httptest.NewRequest("POST", "/subcontracts/"+itoa64(sid)+"/delete", nil)
		req.SetPathValue("id", itoa64(sid))
		w := httptest.NewRecorder()
		DeleteSubcontract(w, req)
		if w.Code != 302 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		if n := batchNCount(t, `SELECT COUNT(*) FROM subcontracts WHERE id=? AND deleted_at IS NOT NULL`, sid); n != 1 {
			t.Fatalf("deleted=%d want 1", n)
		}
	})
}

func TestBatchNSubcontractScanAndFilters(t *testing.T) {
	owner := model.User{ID: 1, Role: "owner"}
	seed := func(t *testing.T) {
		newMigratedTestDB(t)
		batchNSeedSubcontractBase(t)
		batchNInsertSubcontract(t, 100, 1, 500, "pending")
		mustExec(t, `INSERT INTO subcontracts (work_order_id, supplier_id, description, cost_amount, charge_amount, status, created_at)
			VALUES (100,1,'時間缺失外包',100,200,'pending',NULL)`)
		mustExec(t, `INSERT INTO subcontracts (work_order_id, supplier_id, description, cost_amount, charge_amount, status)
			VALUES (100,1,'已完成外包',100,200,'completed')`)
	}

	t.Run("列表 q 與 status 篩選並跳過掃描失敗列", func(t *testing.T) {
		seed(t)
		w := httptest.NewRecorder()
		ListSubcontracts(w, httptest.NewRequest("GET", "/subcontracts?q=外包&status=pending", nil), owner)
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "外包加工") {
			t.Fatalf("body should contain 外包加工: %s", body)
		}
		if strings.Contains(body, "時間缺失外包") {
			t.Fatalf("body should skip scan-error row: %s", body)
		}
		if strings.Contains(body, "已完成外包") {
			t.Fatalf("status filter should exclude completed: %s", body)
		}
	})

	t.Run("搜尋 q 並跳過掃描失敗列", func(t *testing.T) {
		seed(t)
		w := httptest.NewRecorder()
		SearchSubcontractsAPI(w, httptest.NewRequest("GET", "/subcontracts/search?q=外包", nil))
		if w.Code != 200 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "外包加工") {
			t.Fatalf("body should contain 外包加工: %s", body)
		}
		if strings.Contains(body, "時間缺失外包") {
			t.Fatalf("body should skip scan-error row: %s", body)
		}
	})

	t.Run("編輯表單 404", func(t *testing.T) {
		seed(t)
		req := httptest.NewRequest("GET", "/subcontracts/999/edit", nil)
		req.SetPathValue("id", "999")
		w := httptest.NewRecorder()
		EditSubcontractForm(w, req, owner)
		if w.Code != 404 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("列表查詢失敗", func(t *testing.T) {
		seed(t)
		mustExec(t, `DROP VIEW active_subcontracts`)
		w := httptest.NewRecorder()
		ListSubcontracts(w, httptest.NewRequest("GET", "/subcontracts", nil), owner)
		if w.Code != 400 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("搜尋查詢失敗", func(t *testing.T) {
		seed(t)
		mustExec(t, `DROP VIEW active_subcontracts`)
		w := httptest.NewRecorder()
		SearchSubcontractsAPI(w, httptest.NewRequest("GET", "/subcontracts/search?q=外包", nil))
		if w.Code != 500 {
			t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
		}
	})
}
