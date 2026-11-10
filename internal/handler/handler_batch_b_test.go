package handler

import (
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/vision"
	"autorepairos/internal/model"
)

// ---------- shared batch B seed helpers (unique names) ----------

func batchBExec(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func batchBSeedBase(t *testing.T) {
	t.Helper()
	newMigratedTestDB(t)
	batchBSeedRows(t)
}

// batchBFileDB 用檔案型 DB（多連線）取代記憶體 DB：部分 handler（如 ListExpenses、
// ListTechnicianWorklogs）在 rows 未關閉時又發 QueryRow，單連線 harness 會死鎖。
func batchBFileDB(t *testing.T) {
	t.Helper()
	orig := db.DB
	t.Cleanup(func() { db.DB = orig })
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "batchb.db"))
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

func batchBSeedRows(t *testing.T) {
	t.Helper()
	batchBExec(t, `INSERT INTO customers (id, name, phone, line_user_id) VALUES (1,'王小明','0912','U-line-1')`)
	batchBExec(t, `INSERT INTO vehicles (id, customer_id, plate_number, brand_model) VALUES (1,1,'ABC-1234','TOYOTA')`)
}

func batchBSeedWO(t *testing.T, id int64, status string, techID int64) {
	t.Helper()
	batchBExec(t, `INSERT INTO work_orders (id, customer_id, vehicle_id, technician_id, status, total_amount, current_mileage)
		VALUES (?,?,?,?,?,0,50000)`, id, 1, 1, techID, status)
}

func batchBSeedTech(t *testing.T, id int64) {
	t.Helper()
	batchBExec(t, `INSERT INTO users (id, username, password, role, name, active, staff_kind)
		VALUES (?,'tech','x','technician','技師甲',1,'employee')`, id)
}

func batchBQueryInt(t *testing.T, q string, args ...interface{}) int64 {
	t.Helper()
	var n int64
	if err := db.DB.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return n
}

// ---------- workorder_items.go ----------

func TestBatchBAddWorkOrderItemLaborAndFragment(t *testing.T) {
	batchBSeedBase(t)
	batchBSeedWO(t, 1, "in_progress", 0)
	owner := model.User{ID: 1, Role: "owner"}

	// plain POST → redirect, row + total persisted
	req := postReq(t, "/work-orders/1/items", url.Values{
		"description": {"換煞車皮"}, "quantity": {"2"}, "unit_price": {"100"},
		"item_type": {"labor"},
	})
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	AddWorkOrderItem(rec, req, owner)
	if rec.Code != http.StatusFound {
		t.Fatalf("add labor: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM work_order_items WHERE work_order_id=1`); n != 1 {
		t.Fatalf("items count=%d", n)
	}
	if tot := batchBQueryInt(t, `SELECT CAST(total_amount AS INTEGER) FROM work_orders WHERE id=1`); tot != 200 {
		t.Fatalf("total=%d want 200", tot)
	}

	// HTMX fragment path
	req2 := postReq(t, "/work-orders/1/items", url.Values{
		"description": {"施工費"}, "quantity": {"1"}, "unit_price": {"500"},
	})
	req2.SetPathValue("id", "1")
	req2.Header.Set("HX-Request", "true")
	rec2 := httptest.NewRecorder()
	AddWorkOrderItem(rec2, req2, owner)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), "施工費") {
		t.Fatalf("fragment: code=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// AJAX JSON path
	req3 := postReq(t, "/work-orders/1/items", url.Values{
		"description": {"檢查"}, "quantity": {"1"}, "unit_price": {"300"},
	})
	req3.SetPathValue("id", "1")
	req3.Header.Set("X-Requested-With", "XMLHttpRequest")
	rec3 := httptest.NewRecorder()
	AddWorkOrderItem(rec3, req3, owner)
	if rec3.Code != http.StatusOK || !strings.Contains(rec3.Body.String(), `"ok":true`) {
		t.Fatalf("ajax: code=%d body=%s", rec3.Code, rec3.Body.String())
	}
}

func TestBatchBAddWorkOrderItemValidation(t *testing.T) {
	batchBSeedBase(t)
	batchBSeedWO(t, 1, "in_progress", 0)
	owner := model.User{ID: 1, Role: "owner"}

	cases := []struct {
		name string
		form url.Values
	}{
		{"qty zero", url.Values{"description": {"x"}, "quantity": {"0"}}},
		{"desc empty", url.Values{"quantity": {"1"}}},
		{"unknown part", url.Values{"description": {"x"}, "quantity": {"1"}, "item_type": {"part"}, "inventory_item_id": {"999"}}},
	}
	for _, c := range cases {
		req := postReq(t, "/work-orders/1/items", c.form)
		req.SetPathValue("id", "1")
		rec := httptest.NewRecorder()
		AddWorkOrderItem(rec, req, owner)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: code=%d want 400", c.name, rec.Code)
		}
	}
	// invalid item_type defaults to labor and succeeds
	req := postReq(t, "/work-orders/1/items", url.Values{
		"description": {"代工"}, "quantity": {"1"}, "unit_price": {"50"}, "item_type": {"bogus"},
	})
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	AddWorkOrderItem(rec, req, owner)
	if rec.Code != http.StatusFound {
		t.Fatalf("default type: code=%d", rec.Code)
	}
	var itemType string
	db.DB.QueryRow(`SELECT item_type FROM work_order_items WHERE work_order_id=1`).Scan(&itemType)
	if itemType != "labor" {
		t.Fatalf("item_type=%q want labor", itemType)
	}
	// subcontract also accepted
	req = postReq(t, "/work-orders/1/items", url.Values{
		"description": {"鈑金"}, "quantity": {"1"}, "unit_price": {"80"}, "item_type": {"subcontract"},
	})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	AddWorkOrderItem(rec, req, owner)
	if rec.Code != http.StatusFound {
		t.Fatalf("subcontract: code=%d", rec.Code)
	}
}

func TestBatchBAddWorkOrderItemPartReserve(t *testing.T) {
	batchBSeedBase(t)
	batchBSeedWO(t, 1, "in_progress", 0)
	batchBExec(t, `INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity, reserved_quantity, selling_price)
		VALUES (1,'P-1','煞車皮','A1',10,0,500)`)
	owner := model.User{ID: 1, Role: "owner"}

	// empty desc filled from inventory, price from selling_price
	req := postReq(t, "/work-orders/1/items", url.Values{
		"quantity": {"3"}, "item_type": {"part"}, "inventory_item_id": {"1"},
	})
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	AddWorkOrderItem(rec, req, owner)
	if rec.Code != http.StatusFound {
		t.Fatalf("part add: code=%d body=%s", rec.Code, rec.Body.String())
	}
	var desc string
	var sub float64
	db.DB.QueryRow(`SELECT description, subtotal FROM work_order_items WHERE work_order_id=1`).Scan(&desc, &sub)
	if desc != "煞車皮" || sub != 1500 {
		t.Fatalf("desc=%q subtotal=%v", desc, sub)
	}
	if r := batchBQueryInt(t, `SELECT reserved_quantity FROM inventory_items WHERE id=1`); r != 3 {
		t.Fatalf("reserved=%d want 3", r)
	}

	// exceeding available stock → 400 and transaction rolled back
	req2 := postReq(t, "/work-orders/1/items", url.Values{
		"description": {"多要"}, "quantity": {"99"}, "item_type": {"part"}, "inventory_item_id": {"1"},
	})
	req2.SetPathValue("id", "1")
	rec2 := httptest.NewRecorder()
	AddWorkOrderItem(rec2, req2, owner)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("oversell: code=%d want 400 body=%s", rec2.Code, rec2.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM work_order_items WHERE work_order_id=1`); n != 1 {
		t.Fatalf("rollback failed, items=%d", n)
	}
}

func TestBatchBAddWorkOrderItemPaidLocked(t *testing.T) {
	batchBSeedBase(t)
	batchBSeedWO(t, 1, "paid", 0)
	req := postReq(t, "/work-orders/1/items", url.Values{
		"description": {"x"}, "quantity": {"1"},
	})
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	AddWorkOrderItem(rec, req, model.User{ID: 1, Role: "owner"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("paid add: code=%d want 400", rec.Code)
	}
}

func TestBatchBDeleteWorkOrderItem(t *testing.T) {
	batchBSeedBase(t)
	batchBSeedWO(t, 1, "in_progress", 0)
	batchBExec(t, `INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity, reserved_quantity, selling_price)
		VALUES (1,'P-1','濾芯','A1',10,2,200)`)
	batchBExec(t, `INSERT INTO work_order_items (id, work_order_id, item_type, inventory_item_id, description, quantity, unit_price, subtotal)
		VALUES (1,1,'part',1,'濾芯',2,200,400)`)

	// nonexistent item → 404
	req := httptest.NewRequest("POST", "/work-orders/1/items/999/delete", nil)
	req.SetPathValue("id", "1")
	req.SetPathValue("item_id", "999")
	rec := httptest.NewRecorder()
	DeleteWorkOrderItem(rec, req, model.User{ID: 1, Role: "owner"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: code=%d want 404", rec.Code)
	}

	// happy path: row deleted, reserved released
	req2 := httptest.NewRequest("POST", "/work-orders/1/items/1/delete", nil)
	req2.SetPathValue("id", "1")
	req2.SetPathValue("item_id", "1")
	rec2 := httptest.NewRecorder()
	DeleteWorkOrderItem(rec2, req2, model.User{ID: 1, Role: "owner"})
	if rec2.Code != http.StatusFound {
		t.Fatalf("delete: code=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM work_order_items WHERE id=1`); n != 0 {
		t.Fatalf("row still exists")
	}
	if r := batchBQueryInt(t, `SELECT reserved_quantity FROM inventory_items WHERE id=1`); r != 0 {
		t.Fatalf("reserved=%d want 0", r)
	}
	if tot := batchBQueryInt(t, `SELECT CAST(total_amount AS INTEGER) FROM work_orders WHERE id=1`); tot != 0 {
		t.Fatalf("total=%d want 0", tot)
	}
}

func TestBatchBLoadItemsAndTechnicianOptions(t *testing.T) {
	batchBSeedBase(t)
	batchBSeedWO(t, 1, "in_progress", 0)
	batchBSeedTech(t, 7)
	batchBExec(t, `INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity, selling_price)
		VALUES (1,'P-9','火星塞','A1',5,300)`)
	batchBExec(t, `INSERT INTO work_order_items (work_order_id, item_type, inventory_item_id, description, quantity, unit_price, subtotal, primary_technician_id)
		VALUES (1,'part',1,'ignored',1,300,300,7),(1,'labor',NULL,'施工',1,100,100,NULL)`)

	items, total := loadWorkOrderItems(1)
	if len(items) != 2 || total != 400 {
		t.Fatalf("items=%d total=%v", len(items), total)
	}
	if items[0].Description != "火星塞 (P-9)" {
		t.Fatalf("composed desc=%q", items[0].Description)
	}
	if got := technicianOptions(); !strings.Contains(got, "技師甲") {
		t.Fatalf("technicianOptions missing name: %q", got)
	}
	// HX fragment renders with tech names
	req := httptest.NewRequest("GET", "/work-orders/1/items", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	if !renderWorkOrderItemsFragment(rec, req, 1, model.User{ID: 1, Role: "owner"}) {
		t.Fatal("fragment should return true for HX request")
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "技師甲") {
		t.Fatalf("fragment body: code=%d", rec.Code)
	}
	req2 := httptest.NewRequest("GET", "/work-orders/1/items", nil)
	if renderWorkOrderItemsFragment(httptest.NewRecorder(), req2, 1, model.User{ID: 1, Role: "owner"}) {
		t.Fatal("fragment should return false without HX")
	}
}

// ---------- legacy_extras.go ----------

func TestBatchBLegacyExtras(t *testing.T) {
	batchBFileDB(t)
	batchBSeedRows(t)
	batchBExec(t, `INSERT INTO maintenance_templates (id, code, name, notes) VALUES (1,'A0002','5千公里定期保養','n1'),(2,'A0005','1萬公里定期保養','')`)
	batchBExec(t, `INSERT INTO maintenance_template_items (template_id, sequence_no, part_number, description, quantity, unit_price)
		VALUES (1,1,'P-1','機油',4,500)`)
	batchBExec(t, `INSERT INTO expenses (sequence_no, spend_date, amount_in, amount_out, part_number, notes)
		VALUES (1,'2026-01-02',100,50,'ACC','茶水'),(2,'2026-01-03',0,200,'','油錢')`)
	owner := model.User{ID: 1, Role: "owner"}
	tech := model.User{ID: 2, Role: "technician"}

	list := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ListMaintenanceTemplates(rec, httptest.NewRequest("GET", path, nil), owner)
		return rec
	}
	if rec := list("/maintenance-templates"); rec.Code != http.StatusOK {
		t.Fatalf("templates list: %d", rec.Code)
	}
	if rec := list("/maintenance-templates?q=A0002"); rec.Code != http.StatusOK {
		t.Fatalf("templates q: %d", rec.Code)
	}
	if rec := list("/maintenance-templates?id=1"); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "5千公里定期保養") {
		t.Fatalf("templates detail: %d", rec.Code)
	}
	d := templateDetailData(1)
	if d == nil || d.Code != "A0002" || len(d.Items) != 1 {
		t.Fatalf("templateDetailData(1)=%+v", d)
	}
	if templateDetailData(999) != nil {
		t.Fatal("templateDetailData(999) should be nil")
	}

	// expenses list + search
	rec := httptest.NewRecorder()
	ListExpenses(rec, httptest.NewRequest("GET", "/expenses?q=茶水", nil), owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("expenses: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchExpensesAPI(rec, httptest.NewRequest("GET", "/expenses/search?q=油錢", nil), owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("expenses search owner: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchExpensesAPI(rec, httptest.NewRequest("GET", "/expenses/search?q=x", nil), tech)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expenses search tech: %d want 403", rec.Code)
	}
}

// ---------- inventory_negative.go ----------

func TestBatchBNegativeStock(t *testing.T) {
	batchBSeedBase(t)
	batchBExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'供應商A')`)
	batchBExec(t, `INSERT INTO inventory_items (id, part_number, name, stock_quantity, min_safety_stock, selling_price, supplier_id, bin_location)
		VALUES (1,'N-1','電源線',-117,10,300,1,'A1'),(2,'N-2','螺絲',5,0,0,NULL,'B1')`)
	owner := model.User{ID: 1, Role: "owner"}

	stats := loadNegativeStockStats()
	if stats.Total != 1 || stats.TotalAll != 2 || stats.WorstQty != -117 || stats.SumDeficit != 117 ||
		stats.SellableOnly != 1 || stats.WithSupplier != 1 {
		t.Fatalf("stats=%+v", stats)
	}

	for _, path := range []string{
		"/inventory/negative", "/inventory/negative?q=電源", "/inventory/negative?sort=sellable",
		"/inventory/negative?sort=name", "/inventory/negative?sort=deficit", "/inventory/negative?id=1",
	} {
		rec := httptest.NewRecorder()
		NegativeStockList(rec, httptest.NewRequest("GET", path, nil), owner)
		if rec.Code != http.StatusOK {
			t.Fatalf("NegativeStockList %s: %d", path, rec.Code)
		}
	}
	if !strings.Contains(func() string {
		rec := httptest.NewRecorder()
		NegativeStockList(rec, httptest.NewRequest("GET", "/inventory/negative", nil), owner)
		return rec.Body.String()
	}(), "電源線") {
		t.Fatal("list missing negative item")
	}

	rec := httptest.NewRecorder()
	NegativeStockFixForm(rec, httptest.NewRequest("GET", "/inventory/negative/fix?id=1", nil), owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("fix form: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	NegativeStockFixForm(rec, httptest.NewRequest("GET", "/inventory/negative/fix?id=999", nil), owner)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("fix form 404: %d", rec.Code)
	}
}

func TestBatchBApplyNegativeStockFix(t *testing.T) {
	batchBSeedBase(t)
	batchBExec(t, `INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity, min_safety_stock)
		VALUES (1,'N-1','電源線','A1',-117,10)`)
	owner := model.User{ID: 1, Role: "owner"}

	apply := func(form url.Values) *httptest.ResponseRecorder {
		req := postReq(t, "/inventory/negative/fix", form)
		rec := httptest.NewRecorder()
		ApplyNegativeStockFix(rec, req, owner)
		return rec
	}
	if rec := apply(url.Values{"item_id": {"999"}, "mode": {"zero"}}); rec.Code != http.StatusNotFound {
		t.Fatalf("missing item: %d", rec.Code)
	}
	if rec := apply(url.Values{"item_id": {"1"}, "mode": {"bogus"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus mode: %d", rec.Code)
	}
	if rec := apply(url.Values{"item_id": {"1"}, "mode": {"custom"}, "target": {"-5"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("negative target: %d", rec.Code)
	}
	// custom fix
	if rec := apply(url.Values{"item_id": {"1"}, "mode": {"custom"}, "target": {"5"}, "reason": {"盤點"}}); rec.Code != http.StatusFound {
		t.Fatalf("custom fix: %d body=%s", rec.Code, rec.Body.String())
	}
	if stock := batchBQueryInt(t, `SELECT stock_quantity FROM inventory_items WHERE id=1`); stock != 5 {
		t.Fatalf("stock=%d want 5", stock)
	}
	var src, reason string
	var before, after, delta int
	if err := db.DB.QueryRow(`SELECT source, reason, before_quantity, after_quantity, delta FROM inventory_movements WHERE inventory_item_id=1`).Scan(&src, &reason, &before, &after, &delta); err != nil {
		t.Fatal(err)
	}
	if src != "legacy_fix" || reason != "盤點" || before != -117 || after != 5 || delta != 122 {
		t.Fatalf("movement src=%s reason=%s %d->%d delta=%d", src, reason, before, after, delta)
	}
	// zero mode with no movement when already 0
	batchBExec(t, `INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity) VALUES (2,'N-2','零庫存','B1',0)`)
	if rec := apply(url.Values{"item_id": {"2"}, "mode": {"zero"}}); rec.Code != http.StatusFound {
		t.Fatalf("noop fix: %d", rec.Code)
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM inventory_movements WHERE inventory_item_id=2`); n != 0 {
		t.Fatalf("noop wrote movement")
	}
	// safety mode → min_safety_stock
	batchBExec(t, `UPDATE inventory_items SET stock_quantity=-3 WHERE id=1`)
	if rec := apply(url.Values{"item_id": {"1"}, "mode": {"safety"}}); rec.Code != http.StatusFound {
		t.Fatalf("safety fix: %d", rec.Code)
	}
	if stock := batchBQueryInt(t, `SELECT stock_quantity FROM inventory_items WHERE id=1`); stock != 10 {
		t.Fatalf("safety stock=%d want 10", stock)
	}
	// zero mode
	batchBExec(t, `UPDATE inventory_items SET stock_quantity=-3 WHERE id=1`)
	if rec := apply(url.Values{"item_id": {"1"}, "mode": {"zero"}}); rec.Code != http.StatusFound {
		t.Fatalf("zero fix: %d", rec.Code)
	}
	if stock := batchBQueryInt(t, `SELECT stock_quantity FROM inventory_items WHERE id=1`); stock != 0 {
		t.Fatalf("zero stock=%d want 0", stock)
	}
	if got := recentMovements(1, 10); len(got) == 0 {
		t.Fatal("recentMovements empty")
	}
	if recentMovements(999, 10) != nil {
		t.Fatal("recentMovements(999) should be nil")
	}
}

func TestBatchBMovementLogAndRecord(t *testing.T) {
	batchBSeedBase(t)
	batchBExec(t, `INSERT INTO inventory_items (id, part_number, name, bin_location, stock_quantity) VALUES (1,'N-1','電源線','A1',0)`)
	batchBExec(t, `INSERT INTO inventory_movements (inventory_item_id, delta, before_quantity, after_quantity, reason, source, created_by)
		VALUES (1,5,0,5,'補貨','manual',NULL)`)
	owner := model.User{ID: 1, Role: "owner"}
	for _, path := range []string{"/inventory/movements", "/inventory/movements?q=電源", "/inventory/movements?source=manual", "/inventory/movements?source=legacy_fix"} {
		rec := httptest.NewRecorder()
		MovementLog(rec, httptest.NewRequest("GET", path, nil), owner)
		if rec.Code != http.StatusOK {
			t.Fatalf("MovementLog %s: %d", path, rec.Code)
		}
	}
	// recordMovement inside a tx
	tx, err := db.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := recordMovement(tx, 1, 2, 5, 7, "test", "manual", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM inventory_movements WHERE inventory_item_id=1`); n != 2 {
		t.Fatalf("movements=%d want 2", n)
	}
}

// ---------- stage7_options.go ----------

func TestBatchBStage7Options(t *testing.T) {
	batchBSeedBase(t)
	batchBExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'供應商A'),(2,'供應商B')`)
	batchBExec(t, `INSERT INTO lookup_items (category, code, label, sort_order, active) VALUES ('color','RED','紅色',1,1),('color','BLUE','藍色',2,1),('color','X','停用',3,0)`)
	batchBExec(t, `INSERT INTO insurance_companies (id, code, name, active) VALUES (1,'CT','國泰',1),(2,'FB','富邦',0)`)

	sups := supplierSelectOptions(2)
	if len(sups) != 2 || !sups[1].Selected || sups[0].Selected {
		t.Fatalf("supplierSelectOptions=%+v", sups)
	}
	looks := lookupSelectOptions("color", "RED")
	if len(looks) != 2 || !looks[0].Selected {
		t.Fatalf("lookupSelectOptions=%+v", looks)
	}
	vehs := vehicleSelectOptions(1)
	if len(vehs) != 1 || !vehs[0].Selected || !strings.Contains(vehs[0].Label, "ABC-1234") {
		t.Fatalf("vehicleSelectOptions=%+v", vehs)
	}
	ins := insuranceCompanySelectOptions(1)
	if len(ins) != 1 || !ins[0].Selected {
		t.Fatalf("insuranceCompanySelectOptions=%+v", ins)
	}
}

// ---------- receipt.go ----------

func TestBatchBReceipt(t *testing.T) {
	batchBSeedBase(t)
	batchBSeedTech(t, 7)
	batchBSeedWO(t, 1, "paid", 7)
	batchBExec(t, `UPDATE work_orders SET total_amount=400, current_mileage=50000 WHERE id=1`)
	batchBExec(t, `INSERT INTO work_order_items (work_order_id, item_type, description, quantity, unit_price, subtotal)
		VALUES (1,'labor','施工費',1,400,400)`)
	owner := model.User{ID: 1, Role: "owner", Username: "owner"}

	// missing WO → 404
	req := httptest.NewRequest("GET", "/work-orders/999/receipt", nil)
	req.SetPathValue("id", "999")
	rec := httptest.NewRecorder()
	PrintWorkOrderReceipt(rec, req, owner)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("receipt 404: %d", rec.Code)
	}
	// other technician → 403
	req = httptest.NewRequest("GET", "/work-orders/1/receipt", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	PrintWorkOrderReceipt(rec, req, model.User{ID: 99, Role: "technician"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("receipt tech 403: %d", rec.Code)
	}
	// happy path + LINE header
	req = httptest.NewRequest("GET", "/work-orders/1/receipt", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	PrintWorkOrderReceipt(rec, req, owner)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "施工費") {
		t.Fatalf("receipt: %d body=%s", rec.Code, rec.Body.String())
	}
	if h := rec.Header().Get("X-Line-User-ID"); h != "U-line-1" {
		t.Fatalf("X-Line-User-ID=%q", h)
	}
	// assigned technician can view own
	req = httptest.NewRequest("GET", "/work-orders/1/receipt", nil)
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	PrintWorkOrderReceipt(rec, req, model.User{ID: 7, Role: "technician", Username: "tech"})
	if rec.Code != http.StatusOK {
		t.Fatalf("receipt own tech: %d", rec.Code)
	}
}

// ---------- scheduled.go ----------

func TestBatchBScheduled(t *testing.T) {
	batchBSeedBase(t)
	batchBExec(t, `INSERT INTO scheduled_tasks (task_key, name, interval_minutes, enabled, run_count) VALUES ('batchb_task','批次任務',5,1,2)`)
	batchBExec(t, `INSERT INTO notifications (kind, channel, body, scheduled_at, status, retry_count, error_message)
		VALUES ('maintenance_due','line','提醒內容','2026-01-01 00:00:00','pending',1,'連線失敗')`)
	batchBExec(t, `INSERT INTO backup_records (backup_date, db_backup_path, db_size, photo_count, photo_size, status, error_message)
		VALUES ('2026-01-01','/tmp/x.db',1024,3,2048,'success','')`)
	owner := model.User{ID: 1, Role: "owner"}

	rec := httptest.NewRecorder()
	ListScheduledTasks(rec, httptest.NewRequest("GET", "/scheduled-tasks", nil), owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("ListScheduledTasks: %d", rec.Code)
	}

	// toggle existing → 302 and flipped
	req := httptest.NewRequest("POST", "/scheduled-tasks/batchb_task/toggle", nil)
	req.SetPathValue("key", "batchb_task")
	rec = httptest.NewRecorder()
	ToggleScheduledTask(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("toggle: %d", rec.Code)
	}
	if en := batchBQueryInt(t, `SELECT enabled FROM scheduled_tasks WHERE task_key='batchb_task'`); en != 0 {
		t.Fatalf("enabled=%d want 0", en)
	}
	// toggle unknown → 404
	req = httptest.NewRequest("POST", "/scheduled-tasks/nope/toggle", nil)
	req.SetPathValue("key", "nope")
	rec = httptest.NewRecorder()
	ToggleScheduledTask(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("toggle 404: %d", rec.Code)
	}

	// cancel pending notification
	var nid int64
	db.DB.QueryRow(`SELECT id FROM notifications WHERE status='pending'`).Scan(&nid)
	req = httptest.NewRequest("POST", "/scheduled-tasks/notifications/"+strconv.FormatInt(nid, 10)+"/cancel", nil)
	req.SetPathValue("id", strconv.FormatInt(nid, 10))
	rec = httptest.NewRecorder()
	CancelNotification(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("cancel: %d", rec.Code)
	}
	if st := batchBQueryInt(t, `SELECT COUNT(*) FROM notifications WHERE id=? AND status='cancelled'`, nid); st != 1 {
		t.Fatal("not cancelled")
	}
	// cancel unknown → 404
	req = httptest.NewRequest("POST", "/scheduled-tasks/notifications/999/cancel", nil)
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	CancelNotification(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cancel 404: %d", rec.Code)
	}

	for name, fn := range map[string]func(http.ResponseWriter, *http.Request){
		"RunNotificationsNow":   RunNotificationsNow,
		"RunMaintenanceScanNow": RunMaintenanceScanNow,
		"RunWorkOrderReadyNow":  RunWorkOrderReadyNow,
	} {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest("POST", "/scheduled-tasks/run", nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("%s: %d body=%s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestBatchBRunBackupNow(t *testing.T) {
	batchBSeedBase(t)
	t.Setenv("BACKUP_DIR", t.TempDir())
	rec := httptest.NewRecorder()
	RunBackupNow(rec, httptest.NewRequest("POST", "/scheduled-tasks/backup", nil))
	// in-memory test DB: backup may succeed (302) or fail (400); either way a record is written
	if rec.Code != http.StatusFound && rec.Code != http.StatusBadRequest {
		t.Fatalf("RunBackupNow: %d body=%s", rec.Code, rec.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM backup_records`); n < 1 {
		t.Fatal("no backup record written")
	}
}

// ---------- vision.go ----------

func TestBatchBVision(t *testing.T) {
	batchBSeedBase(t)
	orig := visionClient
	defer func() { visionClient = orig }()

	// nil client branches
	visionClient = nil
	var pngB64 = base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4E, 0x47})
	postJSON := func(body string) *http.Request {
		r := httptest.NewRequest("POST", "/vision", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	rec := httptest.NewRecorder()
	RecognizePlate(rec, postJSON(`{"image":"`+pngB64+`"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("plate nil client: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	RecognizeInvoice(rec, postJSON(`{"image":"`+pngB64+`"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("invoice nil client: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	VisionStatus(rec, httptest.NewRequest("GET", "/vision/status", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("status nil: %d %s", rec.Code, rec.Body.String())
	}

	// non-nil (zero) client: early validation branches
	SetVisionClient(&vision.Client{Mode: "local"})
	defer StopVisionClient()

	rec = httptest.NewRecorder()
	VisionStatus(rec, httptest.NewRequest("GET", "/vision/status", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("status on: %d %s", rec.Code, rec.Body.String())
	}
	for _, tc := range []struct {
		name string
		body string
		code int
	}{
		{"bad json", `{`, http.StatusBadRequest},
		{"empty image", `{"image":""}`, http.StatusBadRequest},
		{"invalid b64", `{"image":"!!!not-base64!!!"}`, http.StatusRequestEntityTooLarge},
	} {
		rec = httptest.NewRecorder()
		RecognizePlate(rec, postJSON(tc.body))
		if rec.Code != tc.code {
			t.Fatalf("plate %s: %d want %d", tc.name, rec.Code, tc.code)
		}
		rec = httptest.NewRecorder()
		RecognizeInvoice(rec, postJSON(tc.body))
		if rec.Code != tc.code {
			t.Fatalf("invoice %s: %d want %d", tc.name, rec.Code, tc.code)
		}
	}

	// SupplierOptionsAPI: JSON + HX paths
	batchBExec(t, `INSERT INTO suppliers (id, name) VALUES (1,'供應商A')`)
	rec = httptest.NewRecorder()
	SupplierOptionsAPI(rec, httptest.NewRequest("GET", "/suppliers", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "供應商A") {
		t.Fatalf("supplier json: %d %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest("GET", "/suppliers", nil)
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	SupplierOptionsAPI(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "供應商A") {
		t.Fatalf("supplier hx: %d", rec.Code)
	}
}

// ---------- worklogs.go ----------

func TestBatchBWorklogsCRUD(t *testing.T) {
	batchBFileDB(t)
	batchBSeedRows(t)
	batchBSeedTech(t, 7)
	batchBSeedWO(t, 1, "in_progress", 7)
	batchBExec(t, `INSERT INTO technician_worklogs (work_order_id, technician_id, work_date, hours, hourly_rate, labor_amount, commission_rate, commission_amount, notes)
		VALUES (1,7,'2026-01-02',2,500,1000,10,100,'初檢')`)
	owner := model.User{ID: 1, Role: "owner"}
	counter := Actor{ID: 1, Role: "counter"}
	techActor := Actor{ID: 7, Role: "technician"}

	// list: owner + assigned tech ok, other tech forbidden
	for name, tc := range map[string]struct {
		u    model.User
		want int
	}{
		"owner": {owner, http.StatusOK},
		"self":  {model.User{ID: 7, Role: "technician"}, http.StatusOK},
		"other": {model.User{ID: 99, Role: "technician"}, http.StatusForbidden},
	} {
		req := httptest.NewRequest("GET", "/work-orders/1/worklogs", nil)
		req.SetPathValue("id", "1")
		rec := httptest.NewRecorder()
		ListTechnicianWorklogs(rec, req, tc.u)
		if rec.Code != tc.want {
			t.Fatalf("list %s: %d want %d", name, rec.Code, tc.want)
		}
	}

	// counter adds for tech → computed amounts
	req := postReq(t, "/work-orders/1/worklogs", url.Values{
		"technician_id": {"7"}, "work_date": {"2026-01-03"}, "hours": {"3"},
		"hourly_rate": {"400"}, "commission_rate": {"10"}, "notes": {"換油"},
	})
	req.SetPathValue("id", "1")
	rec := httptest.NewRecorder()
	AddTechnicianWorklog(rec, req, counter)
	if rec.Code != http.StatusFound {
		t.Fatalf("add by counter: %d body=%s", rec.Code, rec.Body.String())
	}
	var labor, comm float64
	db.DB.QueryRow(`SELECT labor_amount, commission_amount FROM technician_worklogs WHERE work_date='2026-01-03'`).Scan(&labor, &comm)
	if labor != 1200 || comm != 120 {
		t.Fatalf("labor=%v comm=%v", labor, comm)
	}
	// tech adds own (form tech ignored)
	req = postReq(t, "/work-orders/1/worklogs", url.Values{
		"technician_id": {"999"}, "work_date": {"2026-01-04"}, "hours": {"1"}, "hourly_rate": {"200"},
	})
	req.SetPathValue("id", "1")
	rec = httptest.NewRecorder()
	AddTechnicianWorklog(rec, req, techActor)
	if rec.Code != http.StatusFound {
		t.Fatalf("add by tech: %d body=%s", rec.Code, rec.Body.String())
	}

	// validation failures
	failCases := []struct {
		name  string
		form  url.Values
		actor Actor
		code  int
	}{
		{"counter no tech", url.Values{"work_date": {"2026-01-05"}, "hours": {"1"}}, counter, http.StatusBadRequest},
		{"non-tech user", url.Values{"technician_id": {"1"}, "work_date": {"2026-01-05"}, "hours": {"1"}}, counter, http.StatusBadRequest},
		{"bad date", url.Values{"technician_id": {"7"}, "work_date": {"05/01/2026"}, "hours": {"1"}}, counter, http.StatusBadRequest},
		{"zero hours", url.Values{"technician_id": {"7"}, "work_date": {"2026-01-05"}, "hours": {"0"}}, counter, http.StatusBadRequest},
	}
	for _, c := range failCases {
		req := postReq(t, "/work-orders/1/worklogs", c.form)
		req.SetPathValue("id", "1")
		rec := httptest.NewRecorder()
		AddTechnicianWorklog(rec, req, c.actor)
		if rec.Code != c.code {
			t.Fatalf("%s: %d want %d", c.name, rec.Code, c.code)
		}
	}
	// missing WO → 404
	req = postReq(t, "/work-orders/999/worklogs", url.Values{
		"technician_id": {"7"}, "work_date": {"2026-01-05"}, "hours": {"1"},
	})
	req.SetPathValue("id", "999")
	rec = httptest.NewRecorder()
	AddTechnicianWorklog(rec, req, counter)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("add missing WO: %d", rec.Code)
	}
	// tech not assigned to WO → 400
	batchBSeedWO(t, 2, "in_progress", 0)
	req = postReq(t, "/work-orders/2/worklogs", url.Values{"work_date": {"2026-01-05"}, "hours": {"1"}})
	req.SetPathValue("id", "2")
	rec = httptest.NewRecorder()
	AddTechnicianWorklog(rec, req, techActor)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tech unassigned: %d", rec.Code)
	}

	// edit form + update + delete
	var logID int64
	db.DB.QueryRow(`SELECT id FROM technician_worklogs WHERE work_date='2026-01-03'`).Scan(&logID)
	req2 := httptest.NewRequest("GET", "/work-orders/1/worklogs/"+strconv.FormatInt(logID, 10)+"/edit", nil)
	req2.SetPathValue("id", "1")
	req2.SetPathValue("log_id", strconv.FormatInt(logID, 10))
	rec = httptest.NewRecorder()
	EditTechnicianWorklogForm(rec, req2, owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit form: %d", rec.Code)
	}
	req2 = httptest.NewRequest("GET", "/work-orders/1/worklogs/999/edit", nil)
	req2.SetPathValue("id", "1")
	req2.SetPathValue("log_id", "999")
	rec = httptest.NewRecorder()
	EditTechnicianWorklogForm(rec, req2, owner)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("edit form 404: %d", rec.Code)
	}

	upd := postReq(t, "/work-orders/1/worklogs/"+strconv.FormatInt(logID, 10), url.Values{
		"work_date": {"2026-01-06"}, "hours": {"2"}, "hourly_rate": {"600"}, "commission_rate": {"5"},
	})
	upd.SetPathValue("id", "1")
	upd.SetPathValue("log_id", strconv.FormatInt(logID, 10))
	rec = httptest.NewRecorder()
	UpdateTechnicianWorklog(rec, upd)
	if rec.Code != http.StatusFound {
		t.Fatalf("update: %d body=%s", rec.Code, rec.Body.String())
	}
	db.DB.QueryRow(`SELECT labor_amount, commission_amount FROM technician_worklogs WHERE id=?`, logID).Scan(&labor, &comm)
	if labor != 1200 || comm != 60 {
		t.Fatalf("after update labor=%v comm=%v", labor, comm)
	}
	// update nonexistent → 404; bad date → 400
	upd = postReq(t, "/work-orders/1/worklogs/999", url.Values{"work_date": {"2026-01-06"}, "hours": {"1"}})
	upd.SetPathValue("id", "1")
	upd.SetPathValue("log_id", "999")
	rec = httptest.NewRecorder()
	UpdateTechnicianWorklog(rec, upd)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update 404: %d", rec.Code)
	}
	upd = postReq(t, "/work-orders/1/worklogs/"+strconv.FormatInt(logID, 10), url.Values{"work_date": {"bad"}, "hours": {"1"}})
	upd.SetPathValue("id", "1")
	upd.SetPathValue("log_id", strconv.FormatInt(logID, 10))
	rec = httptest.NewRecorder()
	UpdateTechnicianWorklog(rec, upd)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update bad date: %d", rec.Code)
	}

	del := httptest.NewRequest("POST", "/work-orders/1/worklogs/"+strconv.FormatInt(logID, 10)+"/delete", nil)
	del.SetPathValue("id", "1")
	del.SetPathValue("log_id", strconv.FormatInt(logID, 10))
	rec = httptest.NewRecorder()
	DeleteTechnicianWorklog(rec, del)
	if rec.Code != http.StatusFound {
		t.Fatalf("delete: %d", rec.Code)
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM technician_worklogs WHERE id=? AND deleted_at IS NULL`, logID); n != 0 {
		t.Fatal("soft delete failed")
	}
	del = httptest.NewRequest("POST", "/work-orders/1/worklogs/999/delete", nil)
	del.SetPathValue("id", "1")
	del.SetPathValue("log_id", "999")
	rec = httptest.NewRecorder()
	DeleteTechnicianWorklog(rec, del)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete 404: %d", rec.Code)
	}

	// commissions: tech forbidden, owner ok
	rec = httptest.NewRecorder()
	ListTechnicianCommissions(rec, httptest.NewRequest("GET", "/commissions", nil), model.User{ID: 7, Role: "technician"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("commissions tech: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	ListTechnicianCommissions(rec, httptest.NewRequest("GET", "/commissions?month=2026-01", nil), owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("commissions owner: %d", rec.Code)
	}
	// helpers
	techs := phase5Technicians()
	if len(techs) != 1 || techs[0].Value != "7" {
		t.Fatalf("phase5Technicians=%+v", techs)
	}
	if _, err := timeParseDate(""); err != nil {
		t.Fatal("timeParseDate empty should default to today")
	}
	if trimNum(2.5) != "2.5" || trimNum(0) != "0" {
		t.Fatalf("trimNum: %q %q", trimNum(2.5), trimNum(0))
	}
	// scanTechWorklog over a real row
	row := db.DB.QueryRow(`SELECT `+techWorklogCols+` FROM technician_worklogs wl WHERE wl.id=?`, logID)
	lg, err := scanTechWorklog(row)
	if err != nil || lg.WorkOrderID != 1 || lg.TechnicianID != 7 || lg.Hours != 2 {
		t.Fatalf("scanTechWorklog=%+v err=%v", lg, err)
	}
	if _, err := scanTechWorklog(db.DB.QueryRow(`SELECT 1 WHERE 0`)); err == nil {
		t.Fatal("scanTechWorklog on empty row should error")
	}
}
