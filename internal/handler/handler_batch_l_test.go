package handler

// batchL：專攻 workorder_new.go 與 purchase_orders_items.go 的未覆蓋分支。
//
// 覆蓋策略：
//   - 頁面資料建置（buildNewWorkOrderPageData）：預約入廠横幅、車牌/客戶預選、
//     關鍵字篩選、里程預填、查無提示，以及 scanVehicle 失敗時跳過該列的分支。
//   - QuickVehicle：HTMX fragment 與 JSON 兩條路徑、必填驗證、重複車牌、
//     交易Begin/INSERT 錯誤分支（用 trigger 與關閉 DB 觸發）。
//   - ReceivePurchaseOrder：自動建立零件的完整路徑，以及每个 DB 錯誤分支
//     （用 DROP VIEW 與 BEFORE INSERT/UPDATE RAISE(ABORT) trigger 觸發）。
//
// 所有錯誤分支都在測試自己的 migrated DB 上造姿，不碰 production 與其他測試檔。

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
	"autorepairos/web/components"
)

// ---------- batchL helpers（前綴避免與平行工作者的 helper 撞名） ----------

func batchLOwner() model.User { return model.User{ID: 1, Role: "owner"} }

func batchLBody(rec *httptest.ResponseRecorder) string { return rec.Body.String() }

// batchLMsgParam 解出 redirect Location 裡 msg 參數（已解码）。
func batchLMsgParam(loc string) string {
	u, err := url.Parse(loc)
	if err != nil {
		return ""
	}
	return u.Query().Get("msg")
}

func batchLQueryStr(t *testing.T, q string, args ...interface{}) string {
	t.Helper()
	var v string
	if err := db.DB.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return v
}

func batchLQueryInt(t *testing.T, q string, args ...interface{}) int64 {
	t.Helper()
	var v int64
	if err := db.DB.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return v
}

func batchLSeedCustomerVehicle(t *testing.T) {
	t.Helper()
	mustExec(t, `INSERT INTO customers (id,name,phone) VALUES (1,'王小明','0912345678')`)
	mustExec(t, `INSERT INTO vehicles (id,customer_id,plate_number,brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`)
}

func batchLQuickReq(t *testing.T, form url.Values, hx bool, token string) *http.Request {
	t.Helper()
	r := postReq(t, "/api/quick-vehicle", form)
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	if token != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	}
	return r
}

func batchLReceiveReq(t *testing.T, id string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/purchase-orders/"+id+"/receive", nil)
	r.SetPathValue("id", id)
	return r
}

func batchLSeedDraftPO(t *testing.T) {
	t.Helper()
	mustExec(t, `INSERT INTO suppliers (id,name,payment_terms) VALUES (1,'測試供應商','')`)
	mustExec(t, `INSERT INTO purchase_orders (id,supplier_id,order_date,status) VALUES (1,1,'2026-11-10','draft')`)
}

// batchLSession 建立一個 owner 登入 session（QuickVehicle 需要真實 user 列）。
func batchLSession(t *testing.T) string {
	t.Helper()
	seedUser(t, 1, "owner", "x")
	tok, _ := startTestSession(t, 1)
	return tok
}

// qty 用 interface{} 傳入：數字走 REAL，'abc' 這類文字則可測 Scan 失敗分支。
func batchLSeedItem(t *testing.T, itemID, invID int64, desc string, qty interface{}, unitCost float64) {
	t.Helper()
	mustExec(t, `INSERT INTO purchase_order_items
		(id,order_id,inventory_item_id,description,quantity,unit_cost,subtotal)
		VALUES (?,?,?,?,?,?,?)`, itemID, 1, invID, desc, qty, unitCost, 0)
}

// ---------- workorder_new.go ----------

// TestBatchLNewWorkOrderAppointmentBanners 涵蓋預約進廠開單的横幅分支：
// 查無、已取消、已結案、已預約/已確認/已進廠（含時段與服務內容）。
func TestBatchLNewWorkOrderAppointmentBanners(t *testing.T) {
	cases := []struct {
		name  string
		query string
		setup func(t *testing.T)
		want  []string
	}{
		{
			name:  "預約不存在",
			query: "/work-orders/new?appointment_id=999",
			want:  []string{"找不到預約紀錄"},
		},
		{
			name:  "預約車輛為0",
			query: "/work-orders/new?appointment_id=1",
			setup: func(t *testing.T) {
				mustExec(t, `INSERT INTO appointments (id,customer_id,vehicle_id,appointment_date,status)
					VALUES (1,1,0,'2026-11-10','booked')`)
			},
			want: []string{"找不到預約紀錄"},
		},
		{
			name:  "已取消的預約",
			query: "/work-orders/new?appointment_id=1",
			setup: func(t *testing.T) {
				mustExec(t, `INSERT INTO appointments (id,customer_id,vehicle_id,appointment_date,status)
					VALUES (1,1,1,'2026-11-10','cancelled')`)
			},
			want: []string{"已取消"},
		},
		{
			name:  "已結案的預約",
			query: "/work-orders/new?appointment_id=1",
			setup: func(t *testing.T) {
				mustExec(t, `INSERT INTO appointments (id,customer_id,vehicle_id,appointment_date,status)
					VALUES (1,1,1,'2026-11-10','completed')`)
			},
			want: []string{"已結案"},
		},
		{
			name:  "已預約含時段與服務",
			query: "/work-orders/new?appointment_id=1",
			setup: func(t *testing.T) {
				mustExec(t, `INSERT INTO appointments (id,customer_id,vehicle_id,appointment_date,appointment_time,service_type,status)
					VALUES (1,1,1,'2026-11-10','09:30','定期保養','booked')`)
			},
			want: []string{"已預約", "2026-11-10", "09:30", "定期保養"},
		},
		{
			name:  "已確認無時段",
			query: "/work-orders/new?appointment_id=1",
			setup: func(t *testing.T) {
				mustExec(t, `INSERT INTO appointments (id,customer_id,vehicle_id,appointment_date,status)
					VALUES (1,1,1,'2026-11-10','confirmed')`)
			},
			want: []string{"已確認"},
		},
		{
			name:  "已進廠含時段",
			query: "/work-orders/new?appointment_id=1",
			setup: func(t *testing.T) {
				mustExec(t, `INSERT INTO appointments (id,customer_id,vehicle_id,appointment_date,appointment_time,status)
					VALUES (1,1,1,'2026-11-10','14:00','arrived')`)
			},
			want: []string{"已進廠", "14:00"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newMigratedTestDB(t)
			batchLSeedCustomerVehicle(t)
			if c.setup != nil {
				c.setup(t)
			}
			rec := httptest.NewRecorder()
			NewWorkOrder(rec, httptest.NewRequest(http.MethodGet, c.query, nil), batchLOwner())
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
			}
			for _, w := range c.want {
				if !strings.Contains(batchLBody(rec), w) {
					t.Errorf("response body 缺少 %q；body=%s", w, batchLBody(rec))
				}
			}
		})
	}
}

// TestBatchLNewWorkOrderAppointmentUnknownStatus 涵蓋狀態標籤的後備分支。
// migrated schema 的 appointments.status 有 CHECK 約束，五個合法值都被既有
// 分支處理，因此這裡在測試 DB 內重建沒有 CHECK 的 appointments 表，
// 塞入地圖外的狀態來驗證 label 後備顯示。
func TestBatchLNewWorkOrderAppointmentUnknownStatus(t *testing.T) {
	newMigratedTestDB(t)
	batchLSeedCustomerVehicle(t)
	mustExec(t, `DROP TABLE appointments`)
	mustExec(t, `CREATE TABLE appointments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		customer_id INTEGER NOT NULL,
		vehicle_id INTEGER NOT NULL,
		appointment_date DATE NOT NULL,
		appointment_time TEXT,
		service_type TEXT,
		status TEXT DEFAULT 'booked',
		notes TEXT)`)
	mustExec(t, `INSERT INTO appointments (id,customer_id,vehicle_id,appointment_date,status)
		VALUES (1,1,1,'2026-11-10','weird')`)

	rec := httptest.NewRecorder()
	NewWorkOrder(rec, httptest.NewRequest(http.MethodGet, "/work-orders/new?appointment_id=1", nil), batchLOwner())
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if !strings.Contains(batchLBody(rec), "weird") {
		t.Fatalf("狀態不在地圖內應原樣顯示，body=%s", batchLBody(rec))
	}
}

// TestBatchLNewWorkOrderPreselect 涵蓋 buildNewWorkOrderPageData 的預選與提示分支：
// 車牌查車、指定客戶列表、關鍵字篩選、查無提示、里程預填與車主提示。
func TestBatchLNewWorkOrderPreselect(t *testing.T) {
	newMigratedTestDB(t)
	mustExec(t, `INSERT INTO customers (id,name,phone) VALUES (1,'王小明','0912345678'),(2,'李大華','0923456789')`)
	mustExec(t, `INSERT INTO vehicles (id,customer_id,plate_number,brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`)
	mustExec(t, `INSERT INTO work_orders (id,customer_id,vehicle_id,status,current_mileage,total_amount)
		VALUES (1,1,7,'pending',50000,0)`)

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "指定車牌預選", query: "/work-orders/new?plate=ABC-1234", want: []string{"ABC-1234 / TOYOTA / 王小明"}},
		{name: "查無車牌提示", query: "/work-orders/new?plate=NO-SUCH", want: []string{"查無車牌"}},
		{name: "指定客戶列表", query: "/work-orders/new?customer=1", want: []string{"ABC-1234"}},
		{name: "指定無車客戶", query: "/work-orders/new?customer=2", want: nil},
		{name: "關鍵字篩選", query: "/work-orders/new?q=ABC", want: []string{"ABC-1234"}},
		{name: "關鍵字查無", query: "/work-orders/new?q=ZZZZ", want: []string{"查無符合"}},
		{name: "指定車輛帶里程", query: "/work-orders/new?vehicle=7", want: []string{"50000", "王小明 / 0912345678"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewWorkOrder(rec, httptest.NewRequest(http.MethodGet, c.query, nil), batchLOwner())
			if rec.Code != http.StatusOK {
				t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
			}
			for _, w := range c.want {
				if !strings.Contains(batchLBody(rec), w) {
					t.Errorf("response body 缺少 %q；body=%s", w, batchLBody(rec))
				}
			}
		})
	}
}

// TestBatchLNewWorkOrderSkipsBadVehicleRow 驗證 scanVehicle 失敗時該列被跳過，
// 頁面仍正常回 200（door_count 存入非數字文字，SQLite 柔性型別允許）。
func TestBatchLNewWorkOrderSkipsBadVehicleRow(t *testing.T) {
	newMigratedTestDB(t)
	mustExec(t, `INSERT INTO customers (id,name,phone) VALUES (1,'王小明','0912345678')`)
	mustExec(t, `INSERT INTO vehicles (id,customer_id,plate_number,brand_model,door_count)
		VALUES (7,1,'ABC-1234','TOYOTA','abc')`)

	rec := httptest.NewRecorder()
	NewWorkOrder(rec, httptest.NewRequest(http.MethodGet, "/work-orders/new?customer=1", nil), batchLOwner())
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
}

// TestBatchLSearchVehiclesAPIBranches 涵蓋 SearchWorkOrderVehiclesAPI 的
// 客戶篩選、查詢錯誤與掃描錯誤分支。
func TestBatchLSearchVehiclesAPIBranches(t *testing.T) {
	t.Run("依客戶篩選", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedCustomerVehicle(t)
		rec := httptest.NewRecorder()
		SearchWorkOrderVehiclesAPI(rec, httptest.NewRequest(http.MethodGet, "/vehicles/search?customer_id=1", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "ABC-1234") {
			t.Fatalf("body 缺車牌：%s", batchLBody(rec))
		}
	})

	t.Run("查詢錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedCustomerVehicle(t)
		mustExec(t, `DROP VIEW active_vehicles`)
		rec := httptest.NewRecorder()
		SearchWorkOrderVehiclesAPI(rec, httptest.NewRequest(http.MethodGet, "/vehicles/search?q=ABC", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "搜尋車輛失敗") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})

	t.Run("掃描錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedCustomerVehicle(t)
		// 把視圖的 id 換成非數字文字：WHERE 用車牌仍對得上，但 Scan 成 int64 會失敗。
		mustExec(t, `DROP VIEW active_vehicles`)
		mustExec(t, `CREATE VIEW active_vehicles AS
			SELECT 'X' AS id, customer_id, plate_number, brand_model, vin FROM vehicles`)
		rec := httptest.NewRecorder()
		SearchWorkOrderVehiclesAPI(rec, httptest.NewRequest(http.MethodGet, "/vehicles/search?q=ABC-1234", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "讀取車輛搜尋結果失敗") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})
}

// TestBatchLQuickVehicleBranches 涵蓋 QuickVehicle 的 HTMX fragment 與 JSON 路徑、
// 必填驗證、重複車牌、Begin/INSERT 錯誤分支。
func TestBatchLQuickVehicleBranches(t *testing.T) {
	validForm := url.Values{
		"plate_number":   {"NEW-9999"},
		"brand_model":    {"HONDA"},
		"customer_name":  {"陳小美"},
		"customer_phone": {"0955666777"},
	}

	t.Run("非 POST 回 405", func(t *testing.T) {
		newMigratedTestDB(t)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, httptest.NewRequest(http.MethodGet, "/api/quick-vehicle", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
	})

	t.Run("未登入走 HX fragment", func(t *testing.T) {
		newMigratedTestDB(t)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, validForm, true, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "未登入") || !strings.Contains(batchLBody(rec), "quickVehicleResult") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})

	t.Run("未登入走 JSON", func(t *testing.T) {
		newMigratedTestDB(t)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, validForm, false, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
	})

	t.Run("缺必填走 HX fragment", func(t *testing.T) {
		newMigratedTestDB(t)
		tok := batchLSession(t)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, url.Values{"plate_number": {"NEW-1"}}, true, tok))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "車牌、廠牌、姓名、電話為必填") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})

	t.Run("缺必填走 JSON", func(t *testing.T) {
		newMigratedTestDB(t)
		tok := batchLSession(t)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, url.Values{"plate_number": {"NEW-1"}}, false, tok))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
	})

	t.Run("重複車牌走 HX fragment", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedCustomerVehicle(t)
		tok := batchLSession(t)
		form := url.Values{
			"plate_number":   {"ABC-1234"},
			"brand_model":    {"TOYOTA"},
			"customer_name":  {"王小明"},
			"customer_phone": {"0912345678"},
		}
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, form, true, tok))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		body := batchLBody(rec)
		if !strings.Contains(body, `data-duplicate="true"`) || !strings.Contains(body, `data-owner-name="王小明"`) {
			t.Fatalf("body=%s", body)
		}
	})

	t.Run("建立成功走 HX fragment", func(t *testing.T) {
		newMigratedTestDB(t)
		tok := batchLSession(t)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, validForm, true, tok))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), `data-ok="true"`) {
			t.Fatalf("body=%s", batchLBody(rec))
		}
		var n int64
		if err := db.DB.QueryRow(`SELECT COUNT(*) FROM vehicles`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("應建立 1 台車，實際 %d", n)
		}
	})

	t.Run("建立成功走 JSON", func(t *testing.T) {
		newMigratedTestDB(t)
		tok := batchLSession(t)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, validForm, false, tok))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
	})

	t.Run("客戶 INSERT 失敗回 500", func(t *testing.T) {
		newMigratedTestDB(t)
		tok := batchLSession(t)
		mustExec(t, `CREATE TRIGGER batch_l_cust_abort BEFORE INSERT ON customers
			BEGIN SELECT RAISE(ABORT,'batchL'); END`)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, validForm, false, tok))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "建立客戶失敗") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})

	t.Run("車輛 INSERT 失敗回 500", func(t *testing.T) {
		newMigratedTestDB(t)
		// 先建好同電話客戶，確保流程走到車輛 INSERT。
		mustExec(t, `INSERT INTO customers (id,name,phone) VALUES (1,'陳小美','0955666777')`)
		mustExec(t, `CREATE TRIGGER batch_l_veh_abort BEFORE INSERT ON vehicles
			BEGIN SELECT RAISE(ABORT,'batchL'); END`)
		tok := batchLSession(t)
		rec := httptest.NewRecorder()
		QuickVehicle(rec, batchLQuickReq(t, validForm, false, tok))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "建立車輛失敗") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})
}

// ---------- purchase_orders_items.go ----------

// TestBatchLReceivePurchaseOrderAutoCreate 涵蓋進貨自動建立零件的完整路徑：
// 自動建檔、回填連結、入庫、成本回寫、異動紀錄與「自動建立 N 個新零件」訊息。
func TestBatchLReceivePurchaseOrderAutoCreate(t *testing.T) {
	newMigratedTestDB(t)
	batchLSeedDraftPO(t)
	// 既有零件佔用 PN-1，自動建檔應換後綴成 PN-1-1。
	mustExec(t, `INSERT INTO inventory_items (part_number,name,bin_location) VALUES ('PN-1','既有零件','A-1')`)
	mustExec(t, `INSERT INTO purchase_order_items
		(id,order_id,inventory_item_id,description,part_number,category,quantity,unit_cost,subtotal)
		VALUES (1,1,0,'測試零件','PN-1','引擎',2,100,200)`)

	rec := httptest.NewRecorder()
	ReceivePurchaseOrder(rec, batchLReceiveReq(t, "1"))
	if rec.Code != http.StatusFound {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(batchLMsgParam(loc), "自動建立") {
		t.Fatalf("Location 應帶自動建立訊息，實際 %q", loc)
	}
	if got := batchLQueryStr(t, `SELECT status FROM purchase_orders WHERE id=1`); got != "received" {
		t.Fatalf("status=%q want received", got)
	}

	var part, name, bin, cat string
	var stock int64
	var cost float64
	var itemID int64
	if err := db.DB.QueryRow(`SELECT i.id, i.part_number, i.name, i.bin_location, i.category,
		i.stock_quantity, i.cost_price
		FROM inventory_items i JOIN purchase_order_items p ON p.inventory_item_id = i.id
		WHERE p.id = 1`).Scan(&itemID, &part, &name, &bin, &cat, &stock, &cost); err != nil {
		t.Fatalf("查自動建立零件: %v", err)
	}
	if part != "PN-1-1" {
		t.Errorf("料號應換後綴 PN-1-1，got %q", part)
	}
	if name != "測試零件" || bin != "待歸位" {
		t.Errorf("名稱/庫位不正確：name=%q bin=%q", name, bin)
	}
	if cat != "引擎" {
		t.Errorf("品項類別應帶入「引擎」，got %q", cat)
	}
	if stock != 2 || cost != 100 {
		t.Errorf("庫存/成本不正確：stock=%d cost=%v", stock, cost)
	}
	if itemID <= 0 {
		t.Fatalf("品項未回填零件 id")
	}

	var delta, before, after int64
	var source string
	if err := db.DB.QueryRow(`SELECT delta, before_quantity, after_quantity, source
		FROM inventory_movements WHERE purchase_order_item_id = 1`).Scan(&delta, &before, &after, &source); err != nil {
		t.Fatalf("查異動紀錄: %v", err)
	}
	if delta != 2 || before != 0 || after != 2 || source != "purchase_receive" {
		t.Errorf("異動紀錄不正確：delta=%d before=%d after=%d source=%q", delta, before, after, source)
	}

	var amount, balance float64
	if err := db.DB.QueryRow(`SELECT amount, balance FROM ap_payables
		WHERE source_type='purchase_receive' AND source_id=1`).Scan(&amount, &balance); err != nil {
		t.Fatalf("查應付帳款: %v", err)
	}
	if amount != 200 || balance != 200 {
		t.Errorf("應付帳款不正確：amount=%v balance=%v", amount, balance)
	}
}

// TestBatchLReceivePurchaseOrderAutoCreateNoDescription 涵蓋 description 為空時，
// 自動建檔改用「進貨品項 #<id>」當名稱的分支。
func TestBatchLReceivePurchaseOrderAutoCreateNoDescription(t *testing.T) {
	newMigratedTestDB(t)
	batchLSeedDraftPO(t)
	batchLSeedItem(t, 1, 0, "", 1.0, 50)

	rec := httptest.NewRecorder()
	ReceivePurchaseOrder(rec, batchLReceiveReq(t, "1"))
	if rec.Code != http.StatusFound {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if got := batchLQueryStr(t, `SELECT name FROM inventory_items WHERE id =
		(SELECT inventory_item_id FROM purchase_order_items WHERE id=1)`); got != "進貨品項 #1" {
		t.Fatalf("空描述應使用「進貨品項 #1」，got %q", got)
	}
}

// TestBatchLReceivePurchaseOrderErrorBranches 用 DROP VIEW 與 RAISE(ABORT)
// trigger 觸發 ReceivePurchaseOrder 的每個 DB 錯誤分支。
func TestBatchLReceivePurchaseOrderErrorBranches(t *testing.T) {
	cases := []struct {
		name     string
		setup    func(t *testing.T)
		wantCode int
		wantBody string
	}{
		{
			name:     "單據不存在回 404",
			setup:    func(t *testing.T) {},
			wantCode: http.StatusNotFound,
		},
		{
			name: "搶占更新失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				mustExec(t, `CREATE TRIGGER batch_l_po_abort BEFORE UPDATE ON purchase_orders
					BEGIN SELECT RAISE(ABORT,'batchL'); END`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "進貨失敗",
		},
		{
			name: "讀取品項查詢失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				mustExec(t, `DROP VIEW active_purchase_order_items`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "進貨失敗",
		},
		{
			name: "品項掃描失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				batchLSeedItem(t, 1, 0, "測試", "abc", 10)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "進貨失敗",
		},
		{
			name: "讀取供應商失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				batchLSeedItem(t, 1, 1, "測試", 2.0, 100)
				mustExec(t, `DROP VIEW active_purchase_orders`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "進貨失敗",
		},
		{
			name: "建立應付帳款失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				batchLSeedItem(t, 1, 1, "測試", 2.0, 100)
				mustExec(t, `CREATE TRIGGER batch_l_ap_abort BEFORE INSERT ON ap_payables
					BEGIN SELECT RAISE(ABORT,'batchL'); END`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "建立應付帳款失敗",
		},
		{
			name: "自動建檔零件插入失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				batchLSeedItem(t, 1, 0, "測試", 2.0, 100)
				mustExec(t, `CREATE TRIGGER batch_l_inv_abort BEFORE INSERT ON inventory_items
					BEGIN SELECT RAISE(ABORT,'batchL'); END`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "自動建立零件失敗",
		},
		{
			name: "自動建檔料號查詢失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				batchLSeedItem(t, 1, 0, "測試", 2.0, 100)
				mustExec(t, `DROP VIEW active_inventory_items`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "自動建立零件失敗",
		},
		{
			name: "回填品項連結失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				batchLSeedItem(t, 1, 0, "測試", 2.0, 100)
				mustExec(t, `CREATE TRIGGER batch_l_poi_abort BEFORE UPDATE ON purchase_order_items
					BEGIN SELECT RAISE(ABORT,'batchL'); END`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "進貨失敗",
		},
		{
			name: "入庫更新失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				batchLSeedItem(t, 1, 0, "測試", 2.0, 100)
				mustExec(t, `CREATE TRIGGER batch_l_inv_upd_abort BEFORE UPDATE ON inventory_items
					BEGIN SELECT RAISE(ABORT,'batchL'); END`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "入庫失敗",
		},
		{
			name: "寫入異動紀錄失敗",
			setup: func(t *testing.T) {
				batchLSeedDraftPO(t)
				batchLSeedItem(t, 1, 0, "測試", 2.0, 100)
				mustExec(t, `CREATE TRIGGER batch_l_mov_abort BEFORE INSERT ON inventory_movements
					BEGIN SELECT RAISE(ABORT,'batchL'); END`)
			},
			wantCode: http.StatusBadRequest,
			wantBody: "寫入異動紀錄失敗",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newMigratedTestDB(t)
			c.setup(t)
			rec := httptest.NewRecorder()
			ReceivePurchaseOrder(rec, batchLReceiveReq(t, "1"))
			if rec.Code != c.wantCode {
				t.Fatalf("code=%d want %d body=%s", rec.Code, c.wantCode, batchLBody(rec))
			}
			if c.wantBody != "" && !strings.Contains(batchLBody(rec), c.wantBody) {
				t.Fatalf("body 缺少 %q：%s", c.wantBody, batchLBody(rec))
			}
		})
	}
}

// TestBatchLReceivePurchaseOrderBeginFailure 涵蓋 BeginImmediate 失敗分支。
func TestBatchLReceivePurchaseOrderBeginFailure(t *testing.T) {
	newMigratedTestDB(t)
	batchLSeedDraftPO(t)
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ReceivePurchaseOrder(rec, batchLReceiveReq(t, "1"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if !strings.Contains(batchLBody(rec), "進貨失敗") {
		t.Fatalf("body=%s", batchLBody(rec))
	}
}

// TestBatchLReceivePurchaseOrderRollbackOnError 驗證錯誤分支會回溯，
// 單據維持 draft、不留下庫存與異動紀錄。
func TestBatchLReceivePurchaseOrderRollbackOnError(t *testing.T) {
	newMigratedTestDB(t)
	batchLSeedDraftPO(t)
	batchLSeedItem(t, 1, 0, "測試", 2.0, 100)
	mustExec(t, `CREATE TRIGGER batch_l_mov_abort2 BEFORE INSERT ON inventory_movements
		BEGIN SELECT RAISE(ABORT,'batchL'); END`)

	rec := httptest.NewRecorder()
	ReceivePurchaseOrder(rec, batchLReceiveReq(t, "1"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if got := batchLQueryStr(t, `SELECT status FROM purchase_orders WHERE id=1`); got != "draft" {
		t.Fatalf("失敗後單據應維持 draft，got %q", got)
	}
	if n := batchLQueryInt(t, `SELECT COUNT(*) FROM inventory_items`); n != 0 {
		t.Fatalf("失敗後不應殘留自動建立零件，got %d", n)
	}
	if n := batchLQueryInt(t, `SELECT COUNT(*) FROM inventory_movements`); n != 0 {
		t.Fatalf("失敗後不應殘留異動紀錄，got %d", n)
	}
}

// TestBatchLDeletePurchaseOrderItemBranches 涵蓋 DeletePurchaseOrderItem 的
// 404 與 UPDATE 錯誤分支。
func TestBatchLDeletePurchaseOrderItemBranches(t *testing.T) {
	t.Run("單據不存在回 404", func(t *testing.T) {
		newMigratedTestDB(t)
		rec := httptest.NewRecorder()
		req := postReq(t, "/purchase-orders/999/items/1/delete", url.Values{})
		req.SetPathValue("id", "999")
		req.SetPathValue("itemId", "1")
		DeletePurchaseOrderItem(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
	})

	t.Run("已進貨不可刪除", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedDraftPO(t)
		mustExec(t, `UPDATE purchase_orders SET status='received' WHERE id=1`)
		batchLSeedItem(t, 1, 1, "測試", 1.0, 10)
		rec := httptest.NewRecorder()
		req := postReq(t, "/purchase-orders/1/items/1/delete", url.Values{})
		req.SetPathValue("id", "1")
		req.SetPathValue("itemId", "1")
		DeletePurchaseOrderItem(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(batchLBody(rec), "此單已進貨") {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
	})

	t.Run("刪除更新失敗回 400", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedDraftPO(t)
		batchLSeedItem(t, 1, 1, "測試", 1.0, 10)
		mustExec(t, `CREATE TRIGGER batch_l_poi_del_abort BEFORE UPDATE ON purchase_order_items
			BEGIN SELECT RAISE(ABORT,'batchL'); END`)
		rec := httptest.NewRecorder()
		req := postReq(t, "/purchase-orders/1/items/1/delete", url.Values{})
		req.SetPathValue("id", "1")
		req.SetPathValue("itemId", "1")
		DeletePurchaseOrderItem(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "刪除品項失敗") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})
}

// TestBatchLDeletePurchaseOrderBranches 涵蓋 DeletePurchaseOrder 的
// 品項 UPDATE 失敗與單據 UPDATE 失敗分支。
func TestBatchLDeletePurchaseOrderBranches(t *testing.T) {
	t.Run("品項更新失敗回 400", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedDraftPO(t)
		batchLSeedItem(t, 1, 1, "測試", 1.0, 10)
		mustExec(t, `CREATE TRIGGER batch_l_poi_del2_abort BEFORE UPDATE ON purchase_order_items
			BEGIN SELECT RAISE(ABORT,'batchL'); END`)
		rec := httptest.NewRecorder()
		req := postReq(t, "/purchase-orders/1/delete", url.Values{})
		req.SetPathValue("id", "1")
		DeletePurchaseOrder(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "刪除進貨單品項失敗") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})

	t.Run("單據更新失敗回 400", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedDraftPO(t)
		batchLSeedItem(t, 1, 1, "測試", 1.0, 10)
		mustExec(t, `CREATE TRIGGER batch_l_po_del_abort BEFORE UPDATE ON purchase_orders
			BEGIN SELECT RAISE(ABORT,'batchL'); END`)
		rec := httptest.NewRecorder()
		req := postReq(t, "/purchase-orders/1/delete", url.Values{})
		req.SetPathValue("id", "1")
		DeletePurchaseOrder(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "刪除進貨單失敗") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})
}

// TestBatchLAddPurchaseOrderItemBranches 涵蓋 AddPurchaseOrderItem 的
// 庫存零件帶入名稱與 INSERT 失敗分支。
func TestBatchLAddPurchaseOrderItemBranches(t *testing.T) {
	t.Run("從庫存零件帶入名稱", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedDraftPO(t)
		mustExec(t, `INSERT INTO inventory_items (id,part_number,name,bin_location) VALUES (5,'P-5','機油','A-1')`)
		rec := httptest.NewRecorder()
		req := postReq(t, "/purchase-orders/1/items", url.Values{"inventory_item_id": {"5"}, "quantity": {"2"}})
		req.SetPathValue("id", "1")
		AddPurchaseOrderItem(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
		if !strings.Contains(batchLBody(rec), "機油") {
			t.Fatalf("body=%s", batchLBody(rec))
		}
	})

	t.Run("插入失敗回 200 錯誤列", func(t *testing.T) {
		newMigratedTestDB(t)
		batchLSeedDraftPO(t)
		mustExec(t, `CREATE TRIGGER batch_l_poi_ins_abort BEFORE INSERT ON purchase_order_items
			BEGIN SELECT RAISE(ABORT,'batchL'); END`)
		rec := httptest.NewRecorder()
		req := postReq(t, "/purchase-orders/1/items", url.Values{"description": {"測試"}, "quantity": {"1"}})
		req.SetPathValue("id", "1")
		AddPurchaseOrderItem(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(batchLBody(rec), "加入品項失敗") {
			t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
		}
	})
}

// ---------- batchL 假 driver：真實 SQLite 難以製造的錯誤分支 ----------

// batchLFakeResult 控制 LastInsertId / RowsAffected 的回傳。
type batchLFakeResult struct {
	lastID   int64
	affected int64
	err      error
}

func (r batchLFakeResult) LastInsertId() (int64, error) { return r.lastID, r.err }
func (r batchLFakeResult) RowsAffected() (int64, error) { return r.affected, r.err }

// batchLFakeRows 回傳固定結果；errAt 之後 Next 回傳 err（模擬讀取中斷）。
type batchLFakeRows struct {
	cols   []string
	values [][]driver.Value
	pos    int
	errAt  int
	err    error
}

func (r *batchLFakeRows) Columns() []string { return r.cols }
func (r *batchLFakeRows) Close() error      { return nil }

func (r *batchLFakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.values) {
		if r.err != nil && r.pos >= r.errAt {
			return r.err
		}
		return io.EOF
	}
	copy(dest, r.values[r.pos])
	r.pos++
	return nil
}

func batchLRows(cols []string, vals ...[]driver.Value) *batchLFakeRows {
	return &batchLFakeRows{cols: cols, values: vals}
}

func batchLRowsErr(cols []string, vals [][]driver.Value, err error) *batchLFakeRows {
	return &batchLFakeRows{cols: cols, values: vals, errAt: len(vals), err: err}
}

type batchLFakeConn struct {
	queryFn func(query string) (driver.Rows, error)
	execFn  func(query string) (driver.Result, error)
	beginFn func() (driver.Tx, error)
}

func (c *batchLFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("batchL: Prepare 不支持")
}

func (c *batchLFakeConn) Close() error { return nil }

func (c *batchLFakeConn) Begin() (driver.Tx, error) {
	if c.beginFn != nil {
		return c.beginFn()
	}
	return batchLFakeTx{}, nil
}

func (c *batchLFakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	return c.execFn(query)
}

func (c *batchLFakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	return c.queryFn(query)
}

type batchLFakeTx struct{ commitErr error }

func (t batchLFakeTx) Commit() error   { return t.commitErr }
func (t batchLFakeTx) Rollback() error { return nil }

type batchLDriver struct{}

func (batchLDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("batchL: Open 不支持")
}

type batchLConnector struct{ conn *batchLFakeConn }

func (c *batchLConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c *batchLConnector) Driver() driver.Driver                        { return batchLDriver{} }

// batchLUseFakeDB 把 global db.DB 換成假 driver 的 DB，測試結束自動還原。
func batchLUseFakeDB(t *testing.T, conn *batchLFakeConn) {
	t.Helper()
	fake := sql.OpenDB(&batchLConnector{conn: conn})
	prev := db.DB
	db.DB = fake
	t.Cleanup(func() {
		db.DB = prev
		_ = fake.Close()
	})
}

// batchLLoginRows 讓假 DB 上的 session.Lookup / currentUser 能成功登入。
func batchLLoginRows(q string) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "FROM sessions WHERE id"):
		return batchLRows([]string{"user_id", "created_at"}, []driver.Value{int64(1), "2026-11-10 10:00:00"}), nil
	case strings.Contains(q, "FROM users WHERE id"):
		return batchLRows([]string{"id", "username", "role", "name", "active"},
			[]driver.Value{int64(1), "owner1", "owner", "老闆", int64(1)}), nil
	default:
		return batchLRows([]string{"id"}), nil
	}
}

// TestBatchLSearchVehiclesAPIRowsErr 涵蓋 SearchWorkOrderVehiclesAPI 的
// rows.Err() 分支： Driver 回傳一列後中途出錯。
func TestBatchLSearchVehiclesAPIRowsErr(t *testing.T) {
	newMigratedTestDB(t)
	conn := &batchLFakeConn{
		queryFn: func(string) (driver.Rows, error) {
			return batchLRowsErr(
				[]string{"id", "plate_number", "brand_model", "name", "customer_id"},
				[][]driver.Value{{int64(7), "ABC-1234", "TOYOTA", "王小明", int64(1)}},
				errors.New("batchL: 讀取中斷"),
			), nil
		},
		execFn: func(string) (driver.Result, error) { return batchLFakeResult{}, nil },
	}
	batchLUseFakeDB(t, conn)

	rec := httptest.NewRecorder()
	SearchWorkOrderVehiclesAPI(rec, httptest.NewRequest(http.MethodGet, "/vehicles/search?q=ABC", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if !strings.Contains(batchLBody(rec), "列舉車輛搜尋結果失敗") {
		t.Fatalf("body=%s", batchLBody(rec))
	}
}

// TestBatchLQuickVehicleBeginFailure 涵蓋 QuickVehicle 的 Begin 失敗分支。
// session 資料先在真實 DB 建好，再換假 driver 讓 Begin 回錯。
func TestBatchLQuickVehicleBeginFailure(t *testing.T) {
	newMigratedTestDB(t)
	tok := batchLSession(t)
	conn := &batchLFakeConn{
		queryFn: batchLLoginRows,
		execFn:  func(string) (driver.Result, error) { return batchLFakeResult{}, nil },
		beginFn: func() (driver.Tx, error) { return nil, errors.New("batchL: begin 失敗") },
	}
	batchLUseFakeDB(t, conn)

	rec := httptest.NewRecorder()
	QuickVehicle(rec, batchLQuickReq(t, url.Values{
		"plate_number":   {"NEW-9999"},
		"brand_model":    {"HONDA"},
		"customer_name":  {"陳小美"},
		"customer_phone": {"0955666777"},
	}, false, tok))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if !strings.Contains(batchLBody(rec), "建立失敗") {
		t.Fatalf("body=%s", batchLBody(rec))
	}
}

// TestBatchLQuickVehicleCommitFailure 涵蓋 QuickVehicle 的 Commit 失敗分支。
func TestBatchLQuickVehicleCommitFailure(t *testing.T) {
	newMigratedTestDB(t)
	tok := batchLSession(t)
	conn := &batchLFakeConn{
		queryFn: batchLLoginRows,
		execFn: func(q string) (driver.Result, error) {
			switch {
			case strings.Contains(q, "INSERT INTO customers"):
				return batchLFakeResult{lastID: 1}, nil
			case strings.Contains(q, "INSERT INTO vehicles"):
				return batchLFakeResult{lastID: 1}, nil
			default:
				return batchLFakeResult{}, nil
			}
		},
		beginFn: func() (driver.Tx, error) {
			return batchLFakeTx{commitErr: errors.New("batchL: commit 失敗")}, nil
		},
	}
	batchLUseFakeDB(t, conn)

	rec := httptest.NewRecorder()
	QuickVehicle(rec, batchLQuickReq(t, url.Values{
		"plate_number":   {"NEW-9999"},
		"brand_model":    {"HONDA"},
		"customer_name":  {"陳小美"},
		"customer_phone": {"0955666777"},
	}, false, tok))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if !strings.Contains(batchLBody(rec), "建立失敗") {
		t.Fatalf("body=%s", batchLBody(rec))
	}
}

// batchLFailOnceWriter 只在第一次 Write 回錯，之後正常寫入 recorder。
type batchLFailOnceWriter struct {
	rec    *httptest.ResponseRecorder
	failed bool
}

func (w *batchLFailOnceWriter) Header() http.Header { return w.rec.Header() }
func (w *batchLFailOnceWriter) WriteHeader(code int) {
	w.rec.WriteHeader(code)
}

func (w *batchLFailOnceWriter) Write(b []byte) (int, error) {
	if !w.failed {
		w.failed = true
		return 0, errors.New("batchL: 寫入失敗")
	}
	return w.rec.Write(b)
}

// TestBatchLWriteQuickVehicleFragmentRenderError 涵蓋 writeQuickVehicleFragment
// 的 Render 錯誤分支：templ flush 到失敗的 writer 時回傳錯誤。
func TestBatchLWriteQuickVehicleFragmentRenderError(t *testing.T) {
	newMigratedTestDB(t)
	rec := httptest.NewRecorder()
	w := &batchLFailOnceWriter{rec: rec}
	req := httptest.NewRequest(http.MethodPost, "/api/quick-vehicle", nil)
	writeQuickVehicleFragment(w, req, http.StatusOK, components.QuickVehicleResultData{
		OK: true, VehicleID: "1", Label: "ABC-1234 / TOYOTA / 王小明",
	})
	if !strings.Contains(batchLBody(rec), "渲染快速建車結果失敗") {
		t.Fatalf("body=%s", batchLBody(rec))
	}
}

// TestBatchLReceiveRowsAffectedError 涵蓋 ReceivePurchaseOrder 的
// RowsAffected 錯誤分支。
func TestBatchLReceiveRowsAffectedError(t *testing.T) {
	newMigratedTestDB(t)
	conn := &batchLFakeConn{
		execFn: func(q string) (driver.Result, error) {
			if strings.Contains(q, "UPDATE purchase_orders SET status='received'") {
				return batchLFakeResult{err: errors.New("batchL: RowsAffected 失敗")}, nil
			}
			return batchLFakeResult{}, nil
		},
		queryFn: func(string) (driver.Rows, error) { return nil, errors.New("batchL: query 失敗") },
	}
	batchLUseFakeDB(t, conn)

	rec := httptest.NewRecorder()
	ReceivePurchaseOrder(rec, batchLReceiveReq(t, "1"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if !strings.Contains(batchLBody(rec), "進貨失敗") {
		t.Fatalf("body=%s", batchLBody(rec))
	}
}

// TestBatchLReceiveRowsErr 涵蓋 ReceivePurchaseOrder 的 rows.Err() 分支。
func TestBatchLReceiveRowsErr(t *testing.T) {
	newMigratedTestDB(t)
	conn := &batchLFakeConn{
		execFn: func(q string) (driver.Result, error) {
			if strings.Contains(q, "UPDATE purchase_orders SET status='received'") {
				return batchLFakeResult{affected: 1}, nil
			}
			return batchLFakeResult{}, nil
		},
		queryFn: func(q string) (driver.Rows, error) {
			if strings.Contains(q, "FROM active_purchase_order_items WHERE order_id") {
				return batchLRowsErr(
					[]string{"id", "order_id", "inventory_item_id", "description", "part_number",
						"category", "quantity", "unit_cost", "subtotal"},
					[][]driver.Value{{int64(1), int64(1), int64(0), "測試", "", "",
						float64(2), float64(100), float64(200)}},
					errors.New("batchL: 列舉中斷"),
				), nil
			}
			return batchLRows([]string{"x"}), nil
		},
	}
	batchLUseFakeDB(t, conn)

	rec := httptest.NewRecorder()
	ReceivePurchaseOrder(rec, batchLReceiveReq(t, "1"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if !strings.Contains(batchLBody(rec), "進貨失敗") {
		t.Fatalf("body=%s", batchLBody(rec))
	}
}

// TestBatchLReceiveCommitFailure 涵蓋 ReceivePurchaseOrder 的 Commit 失敗分支：
// 完整走完搶占、讀品項、建应付帳款、自動建檔、入庫、寫異動紀錄後才 COMMIT。
func TestBatchLReceiveCommitFailure(t *testing.T) {
	newMigratedTestDB(t)
	conn := &batchLFakeConn{
		execFn: func(q string) (driver.Result, error) {
			switch {
			case strings.Contains(q, "COMMIT"):
				return batchLFakeResult{}, errors.New("batchL: commit 失敗")
			case strings.Contains(q, "UPDATE purchase_orders SET status='received'"):
				return batchLFakeResult{affected: 1}, nil
			case strings.Contains(q, "INSERT INTO inventory_items"):
				return batchLFakeResult{lastID: 10}, nil
			default:
				return batchLFakeResult{}, nil
			}
		},
		queryFn: func(q string) (driver.Rows, error) {
			switch {
			case strings.Contains(q, "FROM active_purchase_order_items WHERE order_id"):
				return batchLRows([]string{"id", "order_id", "inventory_item_id", "description",
					"part_number", "category", "quantity", "unit_cost", "subtotal"},
					[]driver.Value{int64(1), int64(1), int64(0), "測試", "", "",
						float64(2), float64(100), float64(200)}), nil
			case strings.Contains(q, "SELECT supplier_id FROM active_purchase_orders"):
				return batchLRows([]string{"supplier_id"}, []driver.Value{int64(1)}), nil
			case strings.Contains(q, "COALESCE(payment_terms,'') FROM suppliers"):
				return batchLRows([]string{"payment_terms"}, []driver.Value{""}), nil
			case strings.Contains(q, "COALESCE(category,'') FROM active_purchase_order_items WHERE id"):
				return batchLRows([]string{"category"}, []driver.Value{""}), nil
			case strings.Contains(q, "COUNT(1) FROM active_inventory_items WHERE part_number"):
				return batchLRows([]string{"n"}, []driver.Value{int64(0)}), nil
			case strings.Contains(q, "RETURNING stock_quantity"):
				return batchLRows([]string{"stock_quantity"}, []driver.Value{int64(2)}), nil
			case strings.Contains(q, "COALESCE(cost_price,0)"):
				return batchLRows([]string{"cost_price", "stock"}, []driver.Value{float64(0), int64(0)}), nil
			default:
				return batchLRows([]string{"x"}), nil
			}
		},
	}
	batchLUseFakeDB(t, conn)

	rec := httptest.NewRecorder()
	ReceivePurchaseOrder(rec, batchLReceiveReq(t, "1"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, batchLBody(rec))
	}
	if !strings.Contains(batchLBody(rec), "進貨失敗") {
		t.Fatalf("body=%s", batchLBody(rec))
	}
}
