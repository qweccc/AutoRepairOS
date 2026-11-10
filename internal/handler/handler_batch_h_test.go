package handler

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/telegram"
	"autorepairos/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func TestBatchHSubcontractAndSupplier(t *testing.T) {
	newMigratedTestDB(t)
	owner := model.User{ID: 1, Role: "owner"}
	if _, err := db.DB.Exec(`INSERT INTO suppliers (id, name) VALUES (1,'協力廠')`); err != nil {
		t.Fatal(err)
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
	// create with WO -> row exists
	rec := httptest.NewRecorder()
	CreateSubcontract(rec, postReq(t, "/subcontracts", url.Values{"supplier_id": {"1"}, "description": {"外包"}, "cost_amount": {"500"}, "work_order_id": {"10"}}))
	if rec.Code != 302 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	// list/search now scan the row (covers scanSubcontract)
	rec = httptest.NewRecorder()
	ListSubcontracts(rec, httptest.NewRequest("GET", "/subcontracts", nil), owner)
	if rec.Code != 200 {
		t.Fatalf("list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchSubcontractsAPI(rec, httptest.NewRequest("GET", "/subcontracts/search?q=外包", nil))
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	var sid int64
	if err := db.DB.QueryRow(`SELECT id FROM subcontracts ORDER BY id DESC LIMIT 1`).Scan(&sid); err != nil || sid <= 0 {
		t.Fatalf("sid=%d err=%v", sid, err)
	}
	// edit form
	req := httptest.NewRequest("GET", "/subcontracts/1/edit", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	EditSubcontractForm(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("edit form code=%d", rec.Code)
	}
	// update
	req = postReq(t, "/subcontracts/1/update", url.Values{"description": {"外包改"}, "cost_amount": {"600"}})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	UpdateSubcontract(rec, req)
	_ = rec.Code
	// delete
	req = httptest.NewRequest("POST", "/subcontracts/1/delete", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	DeleteSubcontract(rec, req)
	_ = rec.Code
	// supplier quick create fragment
	rec = httptest.NewRecorder()
	QuickCreateSupplier(rec, postReq(t, "/suppliers/quick", url.Values{"name": {"快商"}, "supplier_type": {"material"}}))
	_ = rec.Code
	// writeQuickVehicleFragment path via QuickVehicle
	rec = httptest.NewRecorder()
	QuickVehicle(rec, postReq(t, "/vehicles/quick", url.Values{"plate_number": {"QVX-1"}, "brand_model": {"MAZDA"}, "customer_id": {"1"}}))
	_ = rec.Code
}

func TestBatchHTelegramBindMessage(t *testing.T) {
	newMigratedTestDB(t)
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912345678')`); err != nil {
		t.Fatal(err)
	}
	client := telegram.NewClient(&telegram.Config{Mock: true})
	// already bound chat -> early return
	if err := bindTelegramUser("100", 1); err != nil {
		t.Fatal(err)
	}
	handleTelegramBindMessage(client, 100, "100", "綁定 123456")
	// binding code format, bad code -> failure message, no panic
	handleTelegramBindMessage(client, 200, "200", "綁定 000000")
	// non-phone text -> prompt once
	handleTelegramBindMessage(client, 300, "300", "hello")
	handleTelegramBindMessage(client, 300, "300", "hello again") // prompted already
	// phone with no customer
	handleTelegramBindMessage(client, 400, "400", "0900000000")
	// phone matching customer 1 after unbinding chat 100 -> auto bind
	if _, err := db.DB.Exec(`UPDATE customers SET telegram_user_id=NULL WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	handleTelegramBindMessage(client, 500, "500", "0912345678")
	if !isTelegramBound("500") {
		t.Fatal("phone bind should succeed")
	}
	// phone matching two customers via normalized digits: stored phones
	// differ as strings (UNIQUE passes) but share the first 10 digits.
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (2,'王大明','0912-345678')`); err != nil {
		t.Fatal(err)
	}
	handleTelegramBindMessage(client, 600, "600", "0912345678")
}

func TestBatchHStartTelegramBindingCancelled(t *testing.T) {
	newMigratedTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		StartTelegramBinding(ctx, &wg)
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		// if Shared() cfg invalid it returns before polling; either way it must exit
		<-done
	}
}

func TestBatchHSearchWithData(t *testing.T) {
	newMigratedTestDB(t)
	h, _ := bcrypt.GenerateFromPassword([]byte("pw12345"), bcrypt.MinCost)
	if _, err := db.DB.Exec(`INSERT INTO users (id, username, password, role, name, active) VALUES (1,'owner1',?,'owner','O',1)`, string(h)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO customers (id, name, phone) VALUES (1,'王小明','0912')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity) VALUES (1,'PN-1','煞車皮','A-1',10),(2,'PN-2','機油','B-1',5)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`INSERT INTO equipment (id, name, model, status) VALUES (1,'頂高機','X1','active')`); err != nil {
		t.Fatal(err)
	}
	// SearchPartsAPI returns rows (covers scan loop)
	rec := httptest.NewRecorder()
	SearchPartsAPI(rec, httptest.NewRequest("GET", "/parts/search?q=煞車", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "煞車皮") {
		t.Fatalf("parts code=%d body=%s", rec.Code, rec.Body.String())
	}
	// ListEquipmentMaintenance with rows
	owner := model.User{ID: 1, Role: "owner"}
	mreq := httptest.NewRequest("GET", "/equipment/1/maintenance", nil)
	mreq.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ListEquipmentMaintenance(rec, mreq, owner)
	if rec.Code != 200 {
		t.Fatalf("maint list code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateEquipmentMaintenance(rec, postReq(t, "/equipment/1/maintenance", url.Values{"date": {"2026-01-01"}, "description": {"保養"}, "technician_id": {"1"}}))
	_ = rec.Code
	// ListEquipmentMaintenance again to scan the new row
	mreq = httptest.NewRequest("GET", "/equipment/1/maintenance", nil)
	mreq.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	ListEquipmentMaintenance(rec, mreq, owner)
	if rec.Code != 200 {
		t.Fatalf("maint list2 code=%d", rec.Code)
	}
	// customerOptions / vehicleOptions with rows
	_ = customerOptions()
	_ = vehicleOptions()
	// RegisterStatic: chi + std mux variants already covered; nil should not panic
	RegisterStatic(nil)
}
