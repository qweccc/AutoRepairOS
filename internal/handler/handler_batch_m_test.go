package handler

// Batch M：insurances.go / vehicles_list.go / appointments_list.go 的列表、
// 搜尋與檢視 handler 分支補強。全部走 newMigratedTestDB（真實 migration），
// 直接呼叫 handler 並以 httptest 驗證狀態碼與畫面內容。

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ---------- batch M 共用 seed helpers ----------

func batchMExec(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func batchMSeedCustomer(t *testing.T, id int64, name, phone string) {
	t.Helper()
	batchMExec(t, `INSERT INTO customers (id, name, phone) VALUES (?,?,?)`, id, name, phone)
}

func batchMSeedVehicle(t *testing.T, id, customerID int64, plate, brand string) {
	t.Helper()
	batchMExec(t, `INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (?,?,?,?)`,
		id, customerID, plate, brand)
}

func batchMSeedInsuranceCompany(t *testing.T, id int64, name string) {
	t.Helper()
	batchMExec(t, `INSERT INTO insurance_companies (id, code, name, active) VALUES (?,?,?,1)`,
		id, "IC"+batchMItoa(id), name)
}

func batchMItoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func batchMSeedUser(t *testing.T, id int64, username, role string) {
	t.Helper()
	batchMExec(t, `INSERT INTO users (id, username, password, role, name, active) VALUES (?,?,?,?,?,1)`,
		id, username, "pw", role, username)
}

func batchMDate(offsetDays int) string {
	return time.Now().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

func batchMToday() string { return time.Now().Format("2006-01-02") }

func batchMGet(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, path, nil)
}

// batchMFileDB 用檔案型 DB（多連線）取代單一連線的記憶體 DB。
// SearchAppointmentsAPI 在 rows 未關閉時呼叫 bayLabel → bayList 又發查詢，
// 單連線 harness 會死鎖，因此該 handler 一律走多連線 DB。
func batchMFileDB(t *testing.T) {
	t.Helper()
	orig := db.DB
	t.Cleanup(func() { db.DB = orig })
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "batchm.db"))
	d, err := sql.Open("sqlite", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(5)
	t.Cleanup(func() { _ = d.Close() })
	db.DB = d
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
}

func batchMRecv() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func batchMAssertBody(t *testing.T, rec *httptest.ResponseRecorder, want int, subs ...string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("code=%d want %d body=%s", rec.Code, want, rec.Body.String())
	}
	for _, s := range subs {
		if !strings.Contains(rec.Body.String(), s) {
			t.Fatalf("body missing %q: %s", s, rec.Body.String())
		}
	}
}

// batchMInsuranceFixture 建立客戶／車輛／保險公司與三筆投保紀錄，
// 其中第三筆的 insurance_company_id 故意放文字，用來觸發 Scan 錯誤分支。
func batchMInsuranceFixture(t *testing.T) {
	t.Helper()
	batchMSeedCustomer(t, 1, "王小明", "0912")
	batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
	batchMSeedInsuranceCompany(t, 1, "國泰產險")
	batchMExec(t, `INSERT INTO vehicle_insurances
		(id, vehicle_id, insurance_company_id, company_no, insurance_class, insurance_class_name,
		 license_date, card_no, amount_raw, notes, policy_number, expiry_date)
		VALUES (1,1,1,'','B','任意險','2026-01-02','CARD-1','200萬','備註一','POL-1',
		        date('now','localtime','+10 days'))`)
	batchMExec(t, `INSERT INTO vehicle_insurances
		(id, vehicle_id, insurance_company_id, company_no, policy_number, expiry_date)
		VALUES (2,1,NULL,'C-999','POL-2', date('now','localtime','+60 days'))`)
	// Scan 錯誤分支：insurance_company_id 是文字，Scan 進 int64 會失敗。
	batchMExec(t, `INSERT INTO vehicle_insurances
		(id, vehicle_id, insurance_company_id, company_no, policy_number, expiry_date)
		VALUES (3,1,'abc','C-X','POL-3', date('now','localtime','+5 days'))`)
}

// ---------- insurances.go ----------

func TestBatchMListVehicleInsurances(t *testing.T) {
	newMigratedTestDB(t)
	batchMInsuranceFixture(t)
	owner := model.User{ID: 1, Username: "owneruser", Role: "owner"}

	cases := []struct {
		name string
		path string
		subs []string
	}{
		{"全部", "/insurances", []string{"ABC-1234", "POL-1", "POL-2", "國泰產險", "任意險", "CARD-1"}},
		{"q 車牌", "/insurances?q=ABC", []string{"POL-1", "POL-2"}},
		{"q 不存在", "/insurances?q=ZZZZ", nil},
		{"company 篩選", "/insurances?company=國泰產險", []string{"POL-1"}},
		{"company 無對應", "/insurances?company=(未知)", nil},
		{"expiring 參數", "/insurances?expiring=30", []string{"POL-1"}},
		{"expiring 路徑", "/insurances/expiring", []string{"POL-1"}},
		{"分頁", "/insurances?page=1&per=10", []string{"POL-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := batchMRecv()
			ListVehicleInsurances(rec, batchMGet(tc.path), owner)
			batchMAssertBody(t, rec, http.StatusOK, tc.subs...)
		})
	}
}

func TestBatchMSearchInsurancesAPI(t *testing.T) {
	newMigratedTestDB(t)
	batchMInsuranceFixture(t)

	cases := []struct {
		name string
		path string
		subs []string
	}{
		{"全部", "/insurances/search", []string{"POL-1", "POL-2"}},
		{"q 保單號", "/insurances/search?q=POL-1", []string{"POL-1"}},
		{"q 車牌", "/insurances/search?q=ABC", []string{"POL-1"}},
		{"expiring", "/insurances/search?expiring=30", []string{"POL-1"}},
		{"q 不存在", "/insurances/search?q=ZZZZ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := batchMRecv()
			SearchInsurancesAPI(rec, batchMGet(tc.path))
			batchMAssertBody(t, rec, http.StatusOK, tc.subs...)
		})
	}
}

func TestBatchMInsuranceDBErrors(t *testing.T) {
	t.Run("列表 DB 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMInsuranceFixture(t)
		batchMExec(t, `DROP VIEW active_vehicle_insurances`)
		rec := batchMRecv()
		ListVehicleInsurances(rec, batchMGet("/insurances"), model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusBadRequest, "查詢投保紀錄失敗")
	})
	t.Run("搜尋 DB 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMInsuranceFixture(t)
		batchMExec(t, `DROP VIEW active_vehicle_insurances`)
		rec := batchMRecv()
		SearchInsurancesAPI(rec, batchMGet("/insurances/search?q=POL"))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500 body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestBatchMExpiringInsurancesAPI(t *testing.T) {
	t.Run("有即將到期保單", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMInsuranceFixture(t)
		rec := batchMRecv()
		ExpiringInsurancesAPI(rec, batchMGet("/api/insurances/expiring"))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var entries []struct {
			ID           int64  `json:"id"`
			VehicleID    int64  `json:"vehicle_id"`
			Plate        string `json:"plate_number"`
			Customer     string `json:"customer_name"`
			Company      string `json:"insurance_company"`
			PolicyNumber string `json:"policy_number"`
			ExpiryDate   string `json:"expiry_date"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
			t.Fatalf("json: %v body=%s", err, rec.Body.String())
		}
		if len(entries) != 2 {
			t.Fatalf("entries=%d want 2: %+v", len(entries), entries)
		}
		byPolicy := map[string]int{}
		for _, e := range entries {
			byPolicy[e.PolicyNumber]++
			if e.Plate != "ABC-1234" || e.Customer != "王小明" || e.ExpiryDate == "" {
				t.Fatalf("entry %+v", e)
			}
		}
		if byPolicy["POL-1"] != 1 || byPolicy["POL-3"] != 1 {
			t.Fatalf("byPolicy=%v", byPolicy)
		}
	})
	t.Run("無即將到期保單", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `INSERT INTO vehicle_insurances (id, vehicle_id, policy_number, expiry_date)
			VALUES (1,1,'POL-FAR', date('now','localtime','+90 days'))`)
		rec := batchMRecv()
		ExpiringInsurancesAPI(rec, batchMGet("/api/insurances/expiring"))
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("查詢錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMInsuranceFixture(t)
		batchMExec(t, `DROP VIEW active_vehicle_insurances`)
		rec := batchMRecv()
		ExpiringInsurancesAPI(rec, batchMGet("/api/insurances/expiring"))
		batchMAssertBody(t, rec, http.StatusInternalServerError, "查詢即將到期保單失敗")
	})
	t.Run("讀取錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		// 換掉 view：id 欄位型別放寬為 TEXT，塞入非數字即可觸發 Scan 錯誤。
		batchMExec(t, `DROP VIEW active_vehicle_insurances`)
		batchMExec(t, `CREATE TABLE batch_m_vi_probe (
			id TEXT, vehicle_id INTEGER, insurance_company_id INTEGER, company_no TEXT,
			insurance_class TEXT, insurance_class_name TEXT, license_date TEXT, card_no TEXT,
			amount_raw TEXT, notes TEXT, created_at TEXT, policy_number TEXT,
			expiry_date TEXT, deleted_at TEXT)`)
		batchMExec(t, `INSERT INTO batch_m_vi_probe (id, vehicle_id, expiry_date)
			VALUES ('abc', 1, date('now','localtime','+3 days'))`)
		batchMExec(t, `CREATE VIEW active_vehicle_insurances AS
			SELECT * FROM batch_m_vi_probe WHERE deleted_at IS NULL`)
		rec := batchMRecv()
		ExpiringInsurancesAPI(rec, batchMGet("/api/insurances/expiring"))
		batchMAssertBody(t, rec, http.StatusInternalServerError, "讀取即將到期保單失敗")
	})
}

func TestBatchMListVehicleInspections(t *testing.T) {
	newMigratedTestDB(t)
	batchMSeedCustomer(t, 1, "王小明", "0912")
	batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
	batchMExec(t, `INSERT INTO vehicle_inspections (id, vehicle_id, order_date, check_date, firm_no, notes) VALUES
		(1,1,?,'', 'F-1','逾期單'),
		(2,1,?,'', 'F-2','待驗單'),
		(3,1,?,?, 'F-3','完成單'),
		(4,1,'','', '', '無排定日')`,
		batchMDate(-10), batchMDate(10), batchMDate(-20), batchMDate(-15))
	owner := model.User{ID: 1, Username: "owneruser", Role: "owner"}

	cases := []struct {
		name string
		path string
		subs []string
	}{
		{"全部", "/inspections", []string{"ABC-1234", "已逾期", "待檢驗", "已完成"}},
		{"q 車牌", "/inspections?q=ABC", []string{"已逾期"}},
		{"q 客戶", "/inspections?q=王", []string{"已逾期"}},
		{"pending", "/inspections?pending=1", []string{"已逾期", "待檢驗"}},
		{"分頁", "/inspections?page=1&per=10", []string{"已逾期"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := batchMRecv()
			ListVehicleInspections(rec, batchMGet(tc.path), owner)
			batchMAssertBody(t, rec, http.StatusOK, tc.subs...)
		})
	}
}

func TestBatchMSearchInspectionsAPI(t *testing.T) {
	newMigratedTestDB(t)
	batchMSeedCustomer(t, 1, "王小明", "0912")
	batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
	batchMExec(t, `INSERT INTO vehicle_inspections (id, vehicle_id, order_date, check_date)
		VALUES (1,1,?,'')`, batchMDate(-10))
	batchMExec(t, `INSERT INTO vehicle_inspections (id, vehicle_id, order_date, check_date)
		VALUES (2,1,?,'')`, batchMDate(10))
	batchMExec(t, `INSERT INTO vehicle_inspections (id, vehicle_id, order_date, check_date)
		VALUES (3,1,?,?)`, batchMDate(-20), batchMDate(-15))

	cases := []struct {
		name string
		path string
		subs []string
	}{
		{"全部", "/inspections/search", []string{"已逾期", "待檢驗", "已完成"}},
		{"q 車牌", "/inspections/search?q=ABC", []string{"已逾期"}},
		{"q 不存在", "/inspections/search?q=ZZZZ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := batchMRecv()
			SearchInspectionsAPI(rec, batchMGet(tc.path))
			batchMAssertBody(t, rec, http.StatusOK, tc.subs...)
		})
	}
}

func TestBatchMInspectionDBErrors(t *testing.T) {
	t.Run("列表 DB 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `DROP VIEW active_vehicle_inspections`)
		rec := batchMRecv()
		ListVehicleInspections(rec, batchMGet("/inspections"), model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusBadRequest, "查詢定檢排程失敗")
	})
	t.Run("搜尋 DB 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `DROP VIEW active_vehicle_inspections`)
		rec := batchMRecv()
		SearchInspectionsAPI(rec, batchMGet("/inspections/search?q=ABC"))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500 body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("列表 Scan 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		// order_date 允许 NULL 的替身 view：Scan 進 string 會失敗 → continue。
		batchMExec(t, `DROP VIEW active_vehicle_inspections`)
		batchMExec(t, `CREATE TABLE batch_m_insp_probe (
			id INTEGER, vehicle_id INTEGER, order_date TEXT, check_date TEXT,
			firm_no TEXT, notes TEXT, created_at TEXT, deleted_at TEXT)`)
		batchMExec(t, `INSERT INTO batch_m_insp_probe (id, vehicle_id, order_date) VALUES (99, 1, NULL)`)
		batchMExec(t, `CREATE VIEW active_vehicle_inspections AS
			SELECT * FROM batch_m_insp_probe WHERE deleted_at IS NULL`)
		rec := batchMRecv()
		ListVehicleInspections(rec, batchMGet("/inspections"), model.User{ID: 1, Role: "owner"})
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d want 200 body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("搜尋 Scan 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `DROP VIEW active_vehicle_inspections`)
		batchMExec(t, `CREATE TABLE batch_m_insp_probe (
			id INTEGER, vehicle_id INTEGER, order_date TEXT, check_date TEXT,
			firm_no TEXT, notes TEXT, created_at TEXT, deleted_at TEXT)`)
		batchMExec(t, `INSERT INTO batch_m_insp_probe (id, vehicle_id, order_date) VALUES (99, 1, NULL)`)
		batchMExec(t, `CREATE VIEW active_vehicle_inspections AS
			SELECT * FROM batch_m_insp_probe WHERE deleted_at IS NULL`)
		rec := batchMRecv()
		SearchInspectionsAPI(rec, batchMGet("/inspections/search?q=ABC"))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d want 200 body=%s", rec.Code, rec.Body.String())
		}
	})
}

// ---------- vehicles_list.go ----------

func TestBatchMSearchVehiclesAPI(t *testing.T) {
	newMigratedTestDB(t)
	batchMSeedCustomer(t, 1, "王小明", "0912")
	batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
	batchMExec(t, `INSERT INTO lookup_items (category, code, label, sort_order, active)
		VALUES ('vehicle_type','SEDAN','房車',1,1)`)
	batchMExec(t, `UPDATE vehicles SET vehicle_type='SEDAN' WHERE id=1`)

	cases := []struct {
		name string
		path string
		subs []string
	}{
		{"空白 q", "/vehicles/search", []string{"查無車輛"}},
		{"q 車牌", "/vehicles/search?q=ABC", []string{"ABC-1234", "TOYOTA", "房車"}},
		{"q 客戶", "/vehicles/search?q=王", []string{"ABC-1234"}},
		{"q 不存在", "/vehicles/search?q=ZZZZ", []string{"查無車輛"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := batchMRecv()
			SearchVehiclesAPI(rec, batchMGet(tc.path))
			batchMAssertBody(t, rec, http.StatusOK, tc.subs...)
		})
	}
}

func TestBatchMSearchVehiclesAPIErrors(t *testing.T) {
	t.Run("DB 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `DROP VIEW active_vehicles`)
		rec := batchMRecv()
		SearchVehiclesAPI(rec, batchMGet("/vehicles/search?q=ABC"))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500 body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("Scan 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		// door_count 是 INTEGER 非主鍵：塞文字即可讓 Scan 失敗。
		batchMSeedVehicle(t, 2, 1, "XYZ-9876", "HONDA")
		batchMExec(t, `UPDATE vehicles SET door_count='abc' WHERE id=2`)
		rec := batchMRecv()
		SearchVehiclesAPI(rec, batchMGet("/vehicles/search?q=XYZ"))
		batchMAssertBody(t, rec, http.StatusInternalServerError, "讀取車輛搜尋結果失敗")
	})
}

func TestBatchMListVehiclesExtra(t *testing.T) {
	t.Run("customer 篩選與里程儀錶", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedCustomer(t, 2, "李大華", "0923")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMSeedVehicle(t, 2, 2, "XYZ-9876", "HONDA")
		batchMExec(t, `UPDATE vehicles SET next_maintenance_mileage=5500,
			next_maintenance_date='2026-12-01 10:00:00' WHERE id=1`)
		batchMExec(t, `INSERT INTO work_orders
			(id, vehicle_id, status, current_mileage, customer_id, total_amount)
			VALUES (1,1,'completed',5000,1,0)`)
		rec := batchMRecv()
		ListVehicles(rec, batchMGet("/vehicles?customer=1"), model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusOK, "ABC-1234", "5500km 剩 500")
		if strings.Contains(rec.Body.String(), "XYZ-9876") {
			t.Fatalf("customer 篩選漏出其他客戶車輛：%s", rec.Body.String())
		}
	})
	t.Run("DB 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `DROP VIEW active_vehicles`)
		rec := batchMRecv()
		ListVehicles(rec, batchMGet("/vehicles"), model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusBadRequest, "查詢車輛失敗")
	})
	t.Run("Scan 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `UPDATE vehicles SET door_count='abc' WHERE id=1`)
		rec := batchMRecv()
		ListVehicles(rec, batchMGet("/vehicles"), model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusBadRequest, "讀取車輛列表失敗")
	})
}

func TestBatchMShowVehicle(t *testing.T) {
	seed := func(t *testing.T) {
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedUser(t, 2, "tech1", "technician")
		batchMSeedInsuranceCompany(t, 1, "國泰產險")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `INSERT INTO work_orders
			(id, vehicle_id, status, current_mileage, customer_id, technician_id, total_amount,
			 insurance_company_id, claim_no, notes, created_at) VALUES
			(1,1,'completed',50000,1,2,1800,1,'CLM-1','保養','2026-01-02 10:00:00'),
			(2,1,'paid',51000,1,2,900,1,'','鈑金','2026-01-03 11:00:00'),
			(3,1,'pending',52000,1,2,0,NULL,'CLM-3','待料','2026-01-04 12:00:00')`)
		batchMExec(t, `INSERT INTO work_order_items
			(work_order_id, item_type, description, quantity, unit_price, subtotal) VALUES
			(1,'part','煞車皮',2,250,500),
			(1,'labor','工資',1,1000,1000),
			(2,'part','機油',1,300,300)`)
		batchMExec(t, `INSERT INTO vehicle_mileages (vehicle_id, mileage, source, work_order_id, notes)
			VALUES (1,50000,'work_order',1,'進廠')`)
		// scanVehicle 的 lookupLabel 分支 + maintDueText 的日期/里程分支。
		batchMExec(t, `INSERT INTO lookup_items (category, code, label, sort_order, active) VALUES
			('vehicle_type','SEDAN','房車',1,1),
			('fuel_type','GAS','汽油',1,1),
			('body_style','HATCH','掀背',1,1)`)
		batchMExec(t, `UPDATE vehicles SET vehicle_type='SEDAN', fuel_type='GAS', body_style='HATCH',
			next_maintenance_date='2026-12-01 10:00:00', next_maintenance_mileage=60000 WHERE id=1`)
	}

	t.Run("404", func(t *testing.T) {
		newMigratedTestDB(t)
		req := batchMGet("/vehicles/999")
		req.SetPathValue("id", "999")
		rec := batchMRecv()
		ShowVehicle(rec, req, model.User{ID: 1, Role: "owner"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code=%d want 404", rec.Code)
		}
	})
	t.Run("完整資料", func(t *testing.T) {
		newMigratedTestDB(t)
		seed(t)
		req := batchMGet("/vehicles/1")
		req.SetPathValue("id", "1")
		rec := batchMRecv()
		ShowVehicle(rec, req, model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusOK,
			"ABC-1234", "煞車皮", "機油", "CLM-1", "歷年更換項目", "1800",
			"2026-12-01", "60000km", "房車", "汽油", "掀背")
	})
	t.Run("無工單", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		// 無工單 → lastMileageOf = 0 → maintDueText 的 currentMileage == 0 分支。
		batchMExec(t, `UPDATE vehicles SET next_maintenance_mileage=5000 WHERE id=1`)
		req := batchMGet("/vehicles/1")
		req.SetPathValue("id", "1")
		rec := batchMRecv()
		ShowVehicle(rec, req, model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusOK, "這台車還沒有維修紀錄", "5000km")
	})
	t.Run("工單查詢錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `DROP VIEW active_work_orders`)
		req := batchMGet("/vehicles/1")
		req.SetPathValue("id", "1")
		rec := batchMRecv()
		ShowVehicle(rec, req, model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusBadRequest, "查詢工單失敗")
	})
	t.Run("零件查詢錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		seed(t)
		batchMExec(t, `DROP TABLE work_order_items`)
		req := batchMGet("/vehicles/1")
		req.SetPathValue("id", "1")
		rec := batchMRecv()
		ShowVehicle(rec, req, model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusOK, "ABC-1234")
	})
	t.Run("工單 Scan 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		// status 允许 NULL：Scan 進 string 失敗 → continue。
		batchMExec(t, `INSERT INTO work_orders
			(id, vehicle_id, status, current_mileage, customer_id, total_amount)
			VALUES (1,1,NULL,100,1,0)`)
		req := batchMGet("/vehicles/1")
		req.SetPathValue("id", "1")
		rec := batchMRecv()
		ShowVehicle(rec, req, model.User{ID: 1, Role: "owner"})
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d want 200 body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("零件 Scan 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMSeedCustomer(t, 1, "王小明", "0912")
		batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
		batchMExec(t, `INSERT INTO work_orders
			(id, vehicle_id, status, current_mileage, customer_id, total_amount)
			VALUES (1,1,'completed',100,1,0)`)
		// quantity 全 NULL → SUM 回 NULL → Scan 進 float64 失敗 → continue。
		batchMExec(t, `INSERT INTO work_order_items
			(work_order_id, item_type, description, quantity, unit_price, subtotal)
			VALUES (1,'part','特殊件',NULL,NULL,NULL)`)
		req := batchMGet("/vehicles/1")
		req.SetPathValue("id", "1")
		rec := batchMRecv()
		ShowVehicle(rec, req, model.User{ID: 1, Role: "owner"})
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d want 200 body=%s", rec.Code, rec.Body.String())
		}
	})
}

// ---------- appointments_list.go ----------

func batchMAppointmentFixture(t *testing.T) {
	t.Helper()
	batchMSeedCustomer(t, 1, "王小明", "0912")
	batchMSeedCustomer(t, 2, "李大華", "0923")
	batchMSeedVehicle(t, 1, 1, "ABC-1234", "TOYOTA")
	batchMSeedVehicle(t, 2, 2, "XYZ-9876", "HONDA")
	batchMExec(t, `INSERT INTO appointments
		(id, customer_id, vehicle_id, appointment_date, appointment_time, service_type, bay, status) VALUES
		(1,1,1,?,'09:00','定期保養','LIFT','booked'),
		(2,2,2,?,'10:30','煞車檢查','LIFT','confirmed'),
		(3,1,1,?,'14:00','道路救援','CUSTOM-9','booked'),
		(4,1,1,?,'09:00','輪胎更換','','cancelled'),
		(5,1,1,?,'11:00','引擎檢查','TIRE','completed')`,
		batchMToday(), batchMToday(), batchMToday(), batchMDate(1), batchMDate(2))
}

func TestBatchMListAppointments(t *testing.T) {
	newMigratedTestDB(t)
	batchMAppointmentFixture(t)
	owner := model.User{ID: 1, Username: "owneruser", Role: "owner"}

	cases := []struct {
		name string
		path string
		subs []string
	}{
		{"預設", "/appointments", []string{"ABC-1234", "XYZ-9876"}},
		{"日期篩選", "/appointments?date=" + batchMToday(), []string{"ABC-1234", "CUSTOM-9（非標準）"}},
		{"狀態篩選", "/appointments?status=booked", []string{"ABC-1234"}},
		{"週視圖", "/appointments?date=" + batchMToday() + "&view=week", []string{"ABC-1234"}},
		{"calendar_date", "/appointments?calendar_date=" + batchMDate(1), []string{"ABC-1234"}},
		{"calendar_date 無效", "/appointments?date=" + batchMToday() + "&calendar_date=bad", []string{"ABC-1234"}},
		{"分頁", "/appointments?page=1&per=10", []string{"ABC-1234"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := batchMRecv()
			ListAppointments(rec, batchMGet(tc.path), owner)
			batchMAssertBody(t, rec, http.StatusOK, tc.subs...)
		})
	}
}

func TestBatchMListAppointmentsErrors(t *testing.T) {
	t.Run("DB 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMAppointmentFixture(t)
		batchMExec(t, `DROP VIEW active_appointments`)
		rec := batchMRecv()
		ListAppointments(rec, batchMGet("/appointments"), model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusBadRequest, "查詢預約失敗")
	})
	t.Run("行事曆 Scan 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMAppointmentFixture(t)
		// status 允许 NULL：主列表 Scan 失敗會 continue，
		// 但 appointmentCalendar 的 Scan 失敗會直接回傳錯誤。
		batchMExec(t, `INSERT INTO appointments
			(id, customer_id, vehicle_id, appointment_date, appointment_time, service_type, bay, status)
			VALUES (6,1,1,?,'16:00','夜間檢查','TIRE',NULL)`, batchMToday())
		rec := batchMRecv()
		ListAppointments(rec, batchMGet("/appointments"), model.User{ID: 1, Role: "owner"})
		batchMAssertBody(t, rec, http.StatusBadRequest, "查詢預約行事曆失敗")
	})
}

func TestBatchMAppointmentCalendarAPI(t *testing.T) {
	newMigratedTestDB(t)
	batchMAppointmentFixture(t)
	owner := model.User{ID: 1, Role: "owner"}

	cases := []struct {
		name string
		path string
		subs []string
	}{
		{"無日期", "/api/appointments/calendar", []string{batchMToday()}},
		{"日視圖", "/api/appointments/calendar?date=" + batchMToday() + "&view=day", []string{batchMToday()}},
		{"週視圖", "/api/appointments/calendar?date=" + batchMToday() + "&view=week", []string{batchMToday(), batchMDate(1)}},
		{"狀態篩選", "/api/appointments/calendar?date=" + batchMToday() + "&status=confirmed", []string{batchMToday()}},
		{"日期無效", "/api/appointments/calendar?date=bad", []string{batchMToday()}},
		{"view 參數無效", "/api/appointments/calendar?date=" + batchMToday() + "&view=month", []string{batchMToday()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := batchMRecv()
			AppointmentCalendarAPI(rec, batchMGet(tc.path), owner)
			batchMAssertBody(t, rec, http.StatusOK, tc.subs...)
		})
	}
}

func TestBatchMAppointmentCalendarAPIErrors(t *testing.T) {
	t.Run("DB 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMAppointmentFixture(t)
		batchMExec(t, `DROP VIEW active_appointments`)
		rec := batchMRecv()
		AppointmentCalendarAPI(rec, batchMGet("/api/appointments/calendar?date="+batchMToday()), model.User{})
		batchMAssertBody(t, rec, http.StatusInternalServerError, "查詢預約行事曆失敗")
	})
	t.Run("Scan 錯誤", func(t *testing.T) {
		newMigratedTestDB(t)
		batchMAppointmentFixture(t)
		batchMExec(t, `INSERT INTO appointments
			(id, customer_id, vehicle_id, appointment_date, appointment_time, service_type, bay, status)
			VALUES (6,1,1,?,'16:00','夜間檢查','TIRE',NULL)`, batchMToday())
		rec := batchMRecv()
		AppointmentCalendarAPI(rec, batchMGet("/api/appointments/calendar?date="+batchMToday()), model.User{})
		batchMAssertBody(t, rec, http.StatusInternalServerError, "查詢預約行事曆失敗")
	})
}

func TestBatchMSearchAppointmentsAPI(t *testing.T) {
	batchMFileDB(t)
	batchMAppointmentFixture(t)
	// 讓一筆預約有關聯工單，covers HasWorkOrder 分支。
	batchMExec(t, `INSERT INTO work_orders
		(id, vehicle_id, status, current_mileage, customer_id, total_amount, appointment_id)
		VALUES (1,1,'pending',100,1,0,1)`)

	cases := []struct {
		name string
		path string
		subs []string
	}{
		{"全部", "/appointments/search", []string{"ABC-1234", "XYZ-9876", "已開單"}},
		{"q 車牌", "/appointments/search?q=ABC", []string{"ABC-1234", "已開單"}},
		{"q 服務", "/appointments/search?q=定期", []string{"ABC-1234"}},
		{"q 不存在", "/appointments/search?q=ZZZZ", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := batchMRecv()
			SearchAppointmentsAPI(rec, batchMGet(tc.path))
			batchMAssertBody(t, rec, http.StatusOK, tc.subs...)
		})
	}
}

func TestBatchMSearchAppointmentsAPIErrors(t *testing.T) {
	t.Run("DB 錯誤", func(t *testing.T) {
		batchMFileDB(t)
		batchMAppointmentFixture(t)
		batchMExec(t, `DROP VIEW active_appointments`)
		rec := batchMRecv()
		SearchAppointmentsAPI(rec, batchMGet("/appointments/search?q=ABC"))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500 body=%s", rec.Code, rec.Body.String())
		}
	})
	t.Run("Scan 錯誤", func(t *testing.T) {
		batchMFileDB(t)
		batchMAppointmentFixture(t)
		batchMExec(t, `INSERT INTO appointments
			(id, customer_id, vehicle_id, appointment_date, appointment_time, service_type, bay, status)
			VALUES (6,1,1,?,'16:00','夜間檢查','TIRE',NULL)`, batchMToday())
		rec := batchMRecv()
		SearchAppointmentsAPI(rec, batchMGet("/appointments/search?q=ABC"))
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d want 200 body=%s", rec.Code, rec.Body.String())
		}
	})
}
