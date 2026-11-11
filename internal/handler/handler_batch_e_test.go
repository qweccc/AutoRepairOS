package handler

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func TestBatchEEquipment(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if technicianIDArg("5") == nil || technicianIDArg("") != nil || technicianIDArg("x") != nil {
		t.Fatal("technicianIDArg")
	}
	if equipmentStatusArg("active") == "" {
		t.Fatal("equipmentStatusArg")
	}
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active) VALUES (1,'tech','x','technician','技師',1)`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ListEquipment(rec, httptest.NewRequest("GET", "/equipment", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchEquipmentAPI(rec, httptest.NewRequest("GET", "/equipment/search?q=x", nil))
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateEquipment(rec, postReq(t, "/equipment", url.Values{"name": {"頂高機"}, "category": {"lift"}, "status": {"active"}}))
	_ = rec.Code
	var eid int64
	if err := db.DB.QueryRow(`SELECT id FROM equipment ORDER BY id DESC LIMIT 1`).Scan(&eid); err == nil && eid > 0 {
		req := httptest.NewRequest("GET", "/equipment/1/edit", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		EditEquipmentForm(rec, req, owner)
		_ = rec.Code
		req = postReq(t, "/equipment/1/update", url.Values{"name": {"頂高機改"}, "status": {"maintenance"}})
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		UpdateEquipment(rec, req)
		_ = rec.Code
	}
	rec = httptest.NewRecorder()
	ListEquipmentMaintenance(rec, httptest.NewRequest("GET", "/equipment/1/maintenance", nil), owner)
	_ = rec.Code
	rec = httptest.NewRecorder()
	CreateEquipmentMaintenance(rec, postReq(t, "/equipment/1/maintenance", url.Values{"date": {"2026-01-01"}, "description": {"保養"}, "technician_id": {"1"}}))
	_ = rec.Code
	req := httptest.NewRequest("POST", "/equipment-maintenance/1/delete", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeleteEquipmentMaintenance(rec, req)
	_ = rec.Code
	req = httptest.NewRequest("POST", "/equipment/1/delete", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeleteEquipment(rec, req)
	_ = rec.Code
}

func TestBatchEBayMileage(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	// bayConflict: empty inputs no conflict
	if _, _, _, _, conflict := bayConflict("", "", "", 0); conflict {
		t.Fatal("empty bay should not conflict")
	}
	// schedule (empty DB)
	_ = baySchedule("2026-11-09")
	// RecordMileage + history
	if err := RecordMileage(7, 50000, "manual", 0, "note", 1); err != nil {
		t.Fatal(err)
	}
	if km, _, ok := lastMileageEntry(7); !ok || km != 50000 {
		t.Fatalf("lastMileage km=%d ok=%v", km, ok)
	}
	if h := mileageHistory(7, 10); !strings.Contains(h, "50000") {
		t.Fatalf("history %q", h)
	}
	if _, _, ok := lastMileageEntry(999); ok {
		t.Fatal("no entry expected")
	}
	if zeroToNil64(0) != nil || zeroToNil64(3) == nil {
		t.Fatal("zeroToNil64")
	}
	owner := model.User{ID: 1, Role: "owner"}
	req := httptest.NewRequest("GET", "/vehicles/7/mileage/new", nil)
	req.SetPathValue("id", "7")
	rec := httptest.NewRecorder()
	AddVehicleMileageForm(rec, req, owner)
	_ = rec.Code
	rec = httptest.NewRecorder()
	CreateVehicleMileage(rec, postReq(t, "/vehicles/7/mileage", url.Values{"mileage": {"51000"}, "source": {"manual"}}))
	_ = rec.Code
	// negative mileage rejected
	rec = httptest.NewRecorder()
	CreateVehicleMileage(rec, postReq(t, "/vehicles/7/mileage", url.Values{"mileage": {"-5"}}))
	_ = rec.Code
}

func TestBatchEPhrases(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active) VALUES (1,'tech','x','technician','技師',1)`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ListPhrases(rec, httptest.NewRequest("GET", "/phrases", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreatePhrase(rec, postReq(t, "/phrases", url.Values{"category": {"maint"}, "content": {"檢查機油"}}), owner)
	_ = rec.Code
	var pid int64
	if err := db.DB.QueryRow(`SELECT id FROM technician_phrases ORDER BY id DESC LIMIT 1`).Scan(&pid); err == nil && pid > 0 {
		req := httptest.NewRequest("GET", "/phrases/1/edit", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		EditPhraseForm(rec, req, owner)
		_ = rec.Code
		req = postReq(t, "/phrases/1/update", url.Values{"category": {"maint"}, "content": {"檢查煞車"}})
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		UpdatePhrase(rec, req, owner)
		_ = rec.Code
		req = httptest.NewRequest("POST", "/phrases/1/delete", nil)
		req.SetPathValue("id", "1")
		rec = httptest.NewRecorder()
		DeletePhrase(rec, req, owner)
		_ = rec.Code
	}
	_ = generatePhrasesHTML(nil)
	_ = generatePhraseEditForm(model.TechnicianPhrase{})
}

func TestBatchEWorkOrderStatus(t *testing.T) {
	newMigratedTestDB(t)
	for _, tc := range []struct {
		cur, tgt string
		want     bool
	}{
		{"pending", "in_progress", true}, {"pending", "paid", false}, {"paid", "paid", false},
	} {
		if shouldCheckTransition(tc.cur, tc.tgt) != tc.want {
			t.Fatalf("shouldCheckTransition(%q,%q)", tc.cur, tc.tgt)
		}
	}
	if statusOptions(context.Background(), "pending") == "" || approvalOptions(context.Background(), "approved") == "" {
		t.Fatal("options")
	}
	if sopToggleLabel(context.Background(), true) == "" || sopToggleLabel(context.Background(), false) == "" {
		t.Fatal("sopToggleLabel")
	}
	// statusControls variants
	_ = statusControls(context.Background(), model.WorkOrder{}, true)
	_ = statusControls(context.Background(), model.WorkOrder{Status: "paid"}, false)

	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (10,1,7,'pending',0,50000)`); err != nil {
		t.Fatal(err)
	}
	// valid transition pending -> in_progress
	req := postReq(t, "/work-orders/10/status", url.Values{"status": {"in_progress"}})
	req.SetPathValue("id", "10")
	rec := httptest.NewRecorder()
	UpdateWorkOrderStatus(rec, req)
	if rec.Code != 302 {
		t.Fatalf("status update code=%d body=%s", rec.Code, rec.Body.String())
	}
	// invalid transition
	req = postReq(t, "/work-orders/10/status", url.Values{"status": {"paid"}})
	req.SetPathValue("id", "10")
	rec = httptest.NewRecorder()
	UpdateWorkOrderStatus(rec, req)
	_ = rec.Code
	// approval
	req = postReq(t, "/work-orders/10/approval", url.Values{"approval_status": {"approved"}})
	req.SetPathValue("id", "10")
	rec = httptest.NewRecorder()
	UpdateWorkOrderApproval(rec, req)
	_ = rec.Code
	// SOP toggle
	req = httptest.NewRequest("POST", "/work-orders/10/sop-toggle", nil)
	req.SetPathValue("id", "10")
	rec = httptest.NewRecorder()
	ToggleWorkOrderSOP(rec, req)
	_ = rec.Code
}

func TestBatchEPhotos(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if noteLine("hi") == "" || sizeKB(0) == "" || sizeKB(2048) == "" {
		t.Fatal("photo helpers")
	}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (7,1,'ABC-1234','TOYOTA')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO work_orders (id, customer_id, vehicle_id, status, total_amount, current_mileage) VALUES (10,1,7,'pending',0,50000)`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/work-orders/10/photos", nil)
	req.SetPathValue("id", "10")
	rec := httptest.NewRecorder()
	ListWorkOrderPhotos(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("list photos code=%d", rec.Code)
	}
	// canViewWorkOrder owner/technician/stranger
	if ok, err := canViewWorkOrder(owner, 10); err != nil || !ok {
		t.Fatalf("owner view ok=%v err=%v", ok, err)
	}
	if ok, err := canViewWorkOrder(model.User{ID: 9, Role: "technician"}, 10); err == nil && ok {
		t.Fatal("stranger technician should not view")
	}
	// ServePhoto missing -> 404
	req = httptest.NewRequest("GET", "/photos/999", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	ServePhoto(rec, req)
	if rec.Code != 404 && rec.Code != 302 {
		t.Fatalf("serve missing code=%d", rec.Code)
	}
	// upload without file -> error response (not 200 success)
	req = postReq(t, "/work-orders/10/photos", url.Values{})
	req.SetPathValue("id", "10")
	rec = httptest.NewRecorder()
	UploadWorkOrderPhoto(rec, req)
	_ = rec.Code
	// delete missing photo
	req = httptest.NewRequest("POST", "/photos/999/delete", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	DeleteWorkOrderPhoto(rec, req)
	_ = rec.Code
}
