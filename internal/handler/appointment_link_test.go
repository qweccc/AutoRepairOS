package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

// 這批測試涵蓋「預約進廠 → 直接開單」的關聯。
//
// 修復前：appointments 與 work_orders 兩張表毫無關聯（全 repo 沒有 appointment_id），
// 所以櫃檯已知車到了、預約資料也填好了，還是要在開單時重打一次車牌與客戶，
// 而且連「這張預約開過單了沒」都查不出來。

func appointmentLinkTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	for _, statement := range []string{
		`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT, phone TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_customers AS SELECT * FROM customers WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicles (id INTEGER PRIMARY KEY, customer_id INTEGER, plate_number TEXT,
			brand_model TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_vehicles AS SELECT * FROM vehicles WHERE deleted_at IS NULL`,
		`CREATE TABLE appointments (
			id INTEGER PRIMARY KEY, customer_id INTEGER, vehicle_id INTEGER,
			appointment_date TEXT, appointment_time TEXT, service_type TEXT,
			bay TEXT, status TEXT, notes TEXT, updated_at TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_appointments AS SELECT * FROM appointments WHERE deleted_at IS NULL`,
		`CREATE TABLE work_orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT, vehicle_id INTEGER, customer_id INTEGER,
			technician_id INTEGER, status TEXT, current_mileage INTEGER,
			customer_complaint TEXT, approval_status TEXT, total_amount REAL,
			sop_completed INTEGER, insurance_company_id INTEGER, claim_no TEXT,
			is_insurance INTEGER, accident_date TEXT, deductible_amount REAL,
			claim_amount REAL, claim_date TEXT, claim_note TEXT, appointment_id INTEGER,
			updated_at TEXT, deleted_at TEXT)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE vehicle_mileages (id INTEGER PRIMARY KEY AUTOINCREMENT, vehicle_id INTEGER,
			mileage INTEGER, source TEXT, work_order_id INTEGER, notes TEXT, recorded_by INTEGER)`,
		`INSERT INTO customers VALUES (1,'客戶甲','0911',NULL),(2,'客戶乙','0922',NULL)`,
		`INSERT INTO vehicles VALUES (1,1,'ABC-1234','Toyota',NULL),(2,2,'XYZ-9876','Honda',NULL)`,
		// 1=可開單(booked) 2=別人的車 3=已取消 4=已結案
		`INSERT INTO appointments VALUES
			(1,1,1,'2026-10-21','09:00','定期保養','LIFT','booked','',NULL,NULL),
			(2,2,2,'2026-10-22','11:00','煞車檢查','ALIGN','confirmed','',NULL,NULL),
			(3,1,1,'2026-10-21','10:00','輪胎更換','TIRE','cancelled','',NULL,NULL),
			(4,1,1,'2026-10-23','12:00','其他','DIAG','completed','',NULL,NULL)`,
	} {
		if _, err := d.Exec(statement); err != nil {
			t.Fatalf("setup failed: %v\n%s", err, statement)
		}
	}
}

func postCreateWorkOrder(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/work-orders", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	CreateWorkOrder(w, r)
	return w
}

func TestCreateWorkOrderFromAppointmentLinksAndMarksArrived(t *testing.T) {
	appointmentLinkTestDB(t)

	w := postCreateWorkOrder(t, url.Values{
		"vehicle_id": {"1"}, "current_mileage": {"50000"}, "appointment_id": {"1"},
	})
	if w.Code != http.StatusFound {
		t.Fatalf("開單應導向（302），得到 %d", w.Code)
	}

	var woID, apptID int64
	if err := db.DB.QueryRow(`SELECT id, COALESCE(appointment_id,0) FROM work_orders
		ORDER BY id DESC LIMIT 1`).Scan(&woID, &apptID); err != nil {
		t.Fatal(err)
	}
	if apptID != 1 {
		t.Errorf("工單的 appointment_id 應為 1，實際 %d", apptID)
	}

	var status string
	if err := db.DB.QueryRow(`SELECT status FROM appointments WHERE id = 1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "arrived" {
		t.Errorf("預約應被標記為 arrived，實際 %q（櫃檯無法從列表看出車已進廠）", status)
	}
}

// 預約與工單的車輛不符時不可把預約標記成已進廠——那會讓預約狀態與事實不符。
func TestCreateWorkOrderIgnoresMismatchedAppointment(t *testing.T) {
	appointmentLinkTestDB(t)

	// 用車輛 1 開單，但帶入屬於車輛 2 的預約 2
	w := postCreateWorkOrder(t, url.Values{
		"vehicle_id": {"1"}, "current_mileage": {"50000"}, "appointment_id": {"2"},
	})
	if w.Code != http.StatusFound {
		t.Fatalf("車輛不符時仍應能開單（302），得到 %d", w.Code)
	}

	var apptID int64
	if err := db.DB.QueryRow(`SELECT COALESCE(appointment_id,0) FROM work_orders
		ORDER BY id DESC LIMIT 1`).Scan(&apptID); err != nil {
		t.Fatal(err)
	}
	if apptID != 0 {
		t.Errorf("車輛不符時不該建立關聯，實際 appointment_id=%d", apptID)
	}

	var status string
	if err := db.DB.QueryRow(`SELECT status FROM appointments WHERE id = 2`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "confirmed" {
		t.Errorf("別張預約的狀態不該被改動，實際 %q（應維持 confirmed）", status)
	}
}

// 已取消／已結案的預約不能據以開單，狀態也不該被改。
func TestCreateWorkOrderIgnoresClosedAppointment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		apptID string
		want   string
	}{
		{"已取消", "3", "cancelled"},
		{"已結案", "4", "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			appointmentLinkTestDB(t)
			postCreateWorkOrder(t, url.Values{
				"vehicle_id": {"1"}, "current_mileage": {"50000"}, "appointment_id": {tc.apptID},
			})

			var apptID int64
			if err := db.DB.QueryRow(`SELECT COALESCE(appointment_id,0) FROM work_orders
				ORDER BY id DESC LIMIT 1`).Scan(&apptID); err != nil {
				t.Fatal(err)
			}
			if apptID != 0 {
				t.Errorf("%s 的預約不該建立關聯，實際 appointment_id=%d", tc.name, apptID)
			}

			var status string
			if err := db.DB.QueryRow(`SELECT status FROM appointments WHERE id = ?`, tc.apptID).
				Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != tc.want {
				t.Errorf("%s 的預約狀態不該被改動，實際 %q（應維持 %q）", tc.name, status, tc.want)
			}
		})
	}
}

// 預約不存在時仍要能開單——車已經在廠裡了，不該因為來源不見就擋下整張工單。
func TestCreateWorkOrderWithMissingAppointmentStillCreates(t *testing.T) {
	appointmentLinkTestDB(t)

	w := postCreateWorkOrder(t, url.Values{
		"vehicle_id": {"1"}, "current_mileage": {"50000"}, "appointment_id": {"99999"},
	})
	if w.Code != http.StatusFound {
		t.Fatalf("預約不存在時仍應開單成功（302），得到 %d", w.Code)
	}

	var n int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM work_orders`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("工單應被建立，實際 %d 張", n)
	}
	var apptID int64
	if err := db.DB.QueryRow(`SELECT COALESCE(appointment_id,0) FROM work_orders`).Scan(&apptID); err != nil {
		t.Fatal(err)
	}
	if apptID != 0 {
		t.Errorf("不存在的預約不該產生關聯，實際 %d", apptID)
	}
}

// 不帶 appointment_id 的一般開單不受影響（回歸）。
func TestCreateWorkOrderWithoutAppointmentUnaffected(t *testing.T) {
	appointmentLinkTestDB(t)

	if w := postCreateWorkOrder(t, url.Values{
		"vehicle_id": {"1"}, "current_mileage": {"50000"},
	}); w.Code != http.StatusFound {
		t.Fatalf("一般開單應成功（302），得到 %d", w.Code)
	}

	var apptID int64
	if err := db.DB.QueryRow(`SELECT COALESCE(appointment_id,0) FROM work_orders`).Scan(&apptID); err != nil {
		t.Fatal(err)
	}
	if apptID != 0 {
		t.Errorf("一般開單不該有 appointment_id，實際 %d", apptID)
	}
	// 沒有任何預約被動到
	var arrived int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM appointments WHERE status='arrived'`).Scan(&arrived); err != nil {
		t.Fatal(err)
	}
	if arrived != 0 {
		t.Errorf("一般開單不該改動任何預約狀態，實際有 %d 筆變 arrived", arrived)
	}
}

// appointmentWorkOrderIDs 的邊界：空集合不發 SQL、只回有工單的那些。
func TestAppointmentWorkOrderIDs(t *testing.T) {
	appointmentLinkTestDB(t)

	if got := appointmentWorkOrderIDs(nil); len(got) != 0 {
		t.Errorf("空集合應回空 map，實際 %v", got)
	}
	if got := appointmentWorkOrderIDs([]int64{}); len(got) != 0 {
		t.Errorf("空 slice 應回空 map，實際 %v", got)
	}

	// 沒有任何工單時全部為 false
	if got := appointmentWorkOrderIDs([]int64{1, 2, 3}); len(got) != 0 {
		t.Errorf("還沒有工單時應回空 map，實際 %v", got)
	}

	// 讓預約 1 有一張工單
	if _, err := db.DB.Exec(`INSERT INTO work_orders
		(vehicle_id, customer_id, status, current_mileage, appointment_id)
		VALUES (1, 1, 'pending', 100, 1)`); err != nil {
		t.Fatal(err)
	}
	got := appointmentWorkOrderIDs([]int64{1, 2, 3})
	if !got[1] {
		t.Error("預約 1 應該被標記為已有工單")
	}
	if got[2] || got[3] {
		t.Errorf("預約 2、3 不該被標記，實際 %v", got)
	}
}
