package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
	"autorepairos/internal/model"
)

// ---------- helpers (unique names) ----------

func batchDExec(t *testing.T, q string, args ...interface{}) {
	t.Helper()
	if _, err := db.DB.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func batchDGetReq(t *testing.T, path string) *http.Request {
	t.Helper()
	return httptest.NewRequest("GET", path, nil)
}

func batchDPostWithSession(t *testing.T, path string, form url.Values, userID int64) *http.Request {
	t.Helper()
	tok, _ := startTestSession(t, userID)
	r := postReq(t, path, form)
	r.AddCookie(&http.Cookie{Name: "session_token", Value: tok})
	return r
}

func batchDOwner() model.User { return model.User{ID: 1, Role: "owner"} }

// ---------- ap.go ----------

func TestBatchDAP(t *testing.T) {
	batchBFileDB(t)
	batchBExec(t, `INSERT INTO suppliers (id,name,payment_terms) VALUES (1,'材料供應商','check30'),(2,'外包廠商','')`)
	todayStr := today()
	batchBExec(t, `INSERT INTO ap_payables (id,source_type,source_id,supplier_id,amount,paid_amount,balance,due_date,status,created_at)
		VALUES (1,'purchase_receive',10,1,1000,400,600,?,'partial',?),
		       (2,'subcontract',20,2,500,0,500,?,'open',?),
		       (3,'purchase_receive',11,1,300,300,0,?,'paid',?)`,
		todayStr, todayStr, todayStr, todayStr, todayStr, todayStr)
	batchBExec(t, `INSERT INTO ap_payments (payable_id,amount,paid_at) VALUES (1,400,?)`, todayStr)

	// direct loaders
	rows, err := loadAPRows(0, "", "", "", false)
	if err != nil || len(rows) != 2 {
		t.Fatalf("loadAPRows err=%v len=%d", err, len(rows))
	}
	if len(rows[0].Payments) == 0 {
		// first row is newest; payments attached via loadAPPaymentRows
		found := false
		for _, r := range rows {
			if len(r.Payments) > 0 {
				found = true
			}
		}
		if !found {
			t.Fatal("payments not attached")
		}
	}
	if _, err := loadAPRows(1, "paid", "", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAPRows(1, "all", "2026-01-01", "2026-12-31", true); err != nil {
		t.Fatal(err)
	}
	suppliers, err := loadAPSuppliers()
	if err != nil || len(suppliers) != 2 {
		t.Fatalf("suppliers err=%v len=%d", err, len(suppliers))
	}
	aging, err := summarizeAPAging(0, todayStr)
	if err != nil || len(aging) != 4 {
		t.Fatalf("aging err=%v len=%d", err, len(aging))
	}
	if _, err := summarizeAPAging(1, todayStr); err != nil {
		t.Fatal(err)
	}

	// APHome
	for _, path := range []string{"/ap", "/ap?supplier_id=1&status=partial", "/ap?status=bogus&supplier_id=abc"} {
		rec := httptest.NewRecorder()
		APHome(rec, batchDGetReq(t, path), batchDOwner())
		if rec.Code != 200 {
			t.Fatalf("APHome %s code=%d", path, rec.Code)
		}
	}

	// RecordAPPayment: bad amount, then valid partial payment on payable 2
	rec := httptest.NewRecorder()
	r := postReq(t, "/ap/2/pay", url.Values{"amount": {"abc"}})
	r.SetPathValue("id", "2")
	RecordAPPayment(rec, r)
	if rec.Code != 400 {
		t.Fatalf("bad amount code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	r = postReq(t, "/ap/2/pay", url.Values{"amount": {"200"}, "paid_at": {todayStr}})
	r.SetPathValue("id", "2")
	RecordAPPayment(rec, r)
	if rec.Code != 302 {
		t.Fatalf("pay code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT CAST(paid_amount AS INTEGER) FROM ap_payables WHERE id=2`); n != 200 {
		t.Fatalf("paid_amount=%d want 200", n)
	}
	// overpayment rejected
	rec = httptest.NewRecorder()
	r = postReq(t, "/ap/2/pay", url.Values{"amount": {"9999"}})
	r.SetPathValue("id", "2")
	RecordAPPayment(rec, r)
	if rec.Code != 400 {
		t.Fatalf("overpay code=%d", rec.Code)
	}

	// apStatementPeriod
	if _, _, err := apStatementPeriod(batchDGetReq(t, "/ap/statement?from=bad")); err == nil {
		t.Fatal("bad from should error")
	}
	if _, _, err := apStatementPeriod(batchDGetReq(t, "/ap/statement?from=2026-02-01&to=2026-01-01")); err == nil {
		t.Fatal("to<from should error")
	}
	if _, _, err := apStatementPeriod(batchDGetReq(t, "/ap/statement")); err != nil {
		t.Fatal(err)
	}

	// APStatement: no supplier selected, valid supplier, bad period
	rec = httptest.NewRecorder()
	APStatement(rec, batchDGetReq(t, "/ap/statement"), batchDOwner())
	if rec.Code != 200 {
		t.Fatalf("statement empty code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	APStatement(rec, batchDGetReq(t, "/ap/statement?supplier_id=1&from=2026-01-01&to=2026-12-31"), batchDOwner())
	if rec.Code != 200 {
		t.Fatalf("statement code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	APStatement(rec, batchDGetReq(t, "/ap/statement?from=x"), batchDOwner())
	if rec.Code != 400 {
		t.Fatalf("statement bad period code=%d", rec.Code)
	}

	// APStatementCSV: missing supplier, unknown supplier, valid
	rec = httptest.NewRecorder()
	APStatementCSV(rec, batchDGetReq(t, "/ap/statement.csv"))
	if rec.Code != 400 {
		t.Fatalf("csv no supplier code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	APStatementCSV(rec, batchDGetReq(t, "/ap/statement.csv?supplier_id=999&from=2026-01-01&to=2026-12-31"))
	if rec.Code != 404 {
		t.Fatalf("csv unknown supplier code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	APStatementCSV(rec, batchDGetReq(t, "/ap/statement.csv?supplier_id=1&from=2026-01-01&to=2026-12-31"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "供應商") {
		t.Fatalf("csv code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------- purchase_orders.go ----------

func TestBatchDPurchaseOrders(t *testing.T) {
	batchBFileDB(t)
	batchBExec(t, `INSERT INTO suppliers (id,name) VALUES (1,'供應商A'),(2,'供應商B')`)
	batchBExec(t, `INSERT INTO purchase_orders (id,supplier_id,order_date,status,total_amount,notes) VALUES
		(1,1,'2026-01-01','draft',0,'備註甲'),(2,2,'2026-01-02','received',100,'')`)
	batchBExec(t, `INSERT INTO purchase_order_items (id,order_id,description,quantity,unit_cost,subtotal) VALUES
		(1,1,'機油',2,100,200),(2,2,'濾芯',1,100,100)`)

	owner := batchDOwner()
	// List + Search (nested queries inside row loop -> file DB)
	for _, path := range []string{"/purchase-orders", "/purchase-orders?q=備註&status=draft"} {
		rec := httptest.NewRecorder()
		ListPurchaseOrders(rec, batchDGetReq(t, path), owner)
		if rec.Code != 200 {
			t.Fatalf("list %s code=%d", path, rec.Code)
		}
	}
	for _, path := range []string{"/purchase-orders/search", "/purchase-orders/search?q=供應商&status=received"} {
		rec := httptest.NewRecorder()
		SearchPurchaseOrdersAPI(rec, batchDGetReq(t, path))
		if rec.Code != 200 {
			t.Fatalf("search %s code=%d", path, rec.Code)
		}
	}
	// New form
	rec := httptest.NewRecorder()
	NewPurchaseOrderForm(rec, batchDGetReq(t, "/purchase-orders/new"), owner)
	if rec.Code != 200 {
		t.Fatalf("new form code=%d", rec.Code)
	}
	// UnitCost
	if (orderItemDraft{UnitPrice: 42}).UnitCost() != 42 {
		t.Fatal("UnitCost")
	}
	// Create: missing supplier fails; valid with items_json; valid empty
	rec = httptest.NewRecorder()
	CreatePurchaseOrder(rec, postReq(t, "/purchase-orders", url.Values{}))
	if rec.Code != 400 {
		t.Fatalf("create no supplier code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreatePurchaseOrder(rec, postReq(t, "/purchase-orders", url.Values{
		"supplier_id": {"1"}, "order_date": {"2026-02-01"}, "notes": {"新單"},
		"items_json": {`[{"description":"煞車皮","quantity":2,"unit_price":150,"part_number":"BP-1"}]`},
	}))
	if rec.Code != 302 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM purchase_orders WHERE notes='新單'`); n != 1 {
		t.Fatalf("created PO count=%d", n)
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM purchase_order_items WHERE description='煞車皮'`); n != 1 {
		t.Fatalf("created item count=%d", n)
	}
	rec = httptest.NewRecorder()
	CreatePurchaseOrder(rec, postReq(t, "/purchase-orders", url.Values{"supplier_id": {"2"}}))
	if rec.Code != 302 {
		t.Fatalf("create empty code=%d", rec.Code)
	}
	// Show: found / missing
	rec = httptest.NewRecorder()
	req := batchDGetReq(t, "/purchase-orders/1")
	req.SetPathValue("id", "1")
	ShowPurchaseOrder(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("show code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/purchase-orders/999")
	req.SetPathValue("id", "999")
	ShowPurchaseOrder(rec, req, owner)
	if rec.Code != 404 {
		t.Fatalf("show missing code=%d", rec.Code)
	}
	// load items
	items, total := loadPurchaseOrderItems(1)
	if len(items) != 1 || total != 200 {
		t.Fatalf("items=%d total=%v", len(items), total)
	}
	// Add item: missing PO, locked (received), validation, success
	rec = httptest.NewRecorder()
	req = postReq(t, "/purchase-orders/999/items", url.Values{"description": {"x"}, "quantity": {"1"}})
	req.SetPathValue("id", "999")
	AddPurchaseOrderItem(rec, req)
	if rec.Code != 404 {
		t.Fatalf("add missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/purchase-orders/2/items", url.Values{"description": {"x"}, "quantity": {"1"}})
	req.SetPathValue("id", "2")
	AddPurchaseOrderItem(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "已進貨") {
		t.Fatalf("add locked code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/purchase-orders/1/items", url.Values{"quantity": {"0"}})
	req.SetPathValue("id", "1")
	AddPurchaseOrderItem(rec, req)
	if !strings.Contains(rec.Body.String(), "必填") {
		t.Fatalf("add validation body=%s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/purchase-orders/1/items", url.Values{"description": {"雨刷"}, "quantity": {"3"}, "unit_cost": {"50"}})
	req.SetPathValue("id", "1")
	AddPurchaseOrderItem(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "雨刷") {
		t.Fatalf("add ok code=%d body=%s", rec.Code, rec.Body.String())
	}
	if tot := batchBQueryInt(t, `SELECT CAST(total_amount AS INTEGER) FROM purchase_orders WHERE id=1`); tot != 350 {
		t.Fatalf("total after add=%d want 350", tot)
	}
	// Delete item (draft)
	rec = httptest.NewRecorder()
	req = postReq(t, "/purchase-orders/1/items/1/delete", url.Values{})
	req.SetPathValue("id", "1")
	req.SetPathValue("itemId", "1")
	DeletePurchaseOrderItem(rec, req)
	if rec.Code != 200 {
		t.Fatalf("delete item code=%d", rec.Code)
	}
	// Delete PO: received locked, draft ok, missing
	rec = httptest.NewRecorder()
	req = postReq(t, "/purchase-orders/2/delete", url.Values{})
	req.SetPathValue("id", "2")
	DeletePurchaseOrder(rec, req)
	if rec.Code != 400 {
		t.Fatalf("delete received code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/purchase-orders/999/delete", url.Values{})
	req.SetPathValue("id", "999")
	DeletePurchaseOrder(rec, req)
	if rec.Code != 404 {
		t.Fatalf("delete missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/purchase-orders/1/delete", url.Values{})
	req.SetPathValue("id", "1")
	DeletePurchaseOrder(rec, req)
	if rec.Code != 302 {
		t.Fatalf("delete draft code=%d", rec.Code)
	}
	recalcPurchaseOrderTotal(2)
}

// ---------- sop.go ----------

func TestBatchDSOP(t *testing.T) {
	newMigratedTestDB(t)
	batchBSeedRows(t)
	batchDExec(t, `INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount,current_mileage) VALUES (1,1,1,'pending',0,50000)`)

	// Create template: empty name fails, valid succeeds
	rec := httptest.NewRecorder()
	CreateSOPTemplate(rec, postReq(t, "/sop-templates", url.Values{}))
	if rec.Code != 400 {
		t.Fatalf("create empty code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateSOPTemplate(rec, postReq(t, "/sop-templates", url.Values{"name": {"保養SOP"}, "repair_type": {"maintenance"}}))
	if rec.Code != 302 {
		t.Fatalf("create code=%d", rec.Code)
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM sop_templates WHERE name='保養SOP'`); n != 1 {
		t.Fatalf("template count=%d", n)
	}
	tplID := batchBQueryInt(t, `SELECT id FROM sop_templates WHERE name='保養SOP'`)

	// Edit form found/missing; Update: empty, valid, missing
	rec = httptest.NewRecorder()
	req := batchDGetReq(t, "/sop-templates/x/edit")
	req.SetPathValue("id", itoa64(tplID))
	EditSOPTemplateForm(rec, req, batchDOwner())
	if rec.Code != 200 {
		t.Fatalf("edit form code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/sop-templates/999/edit")
	req.SetPathValue("id", "999")
	EditSOPTemplateForm(rec, req, batchDOwner())
	if rec.Code != 404 {
		t.Fatalf("edit missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/x", url.Values{})
	req.SetPathValue("id", itoa64(tplID))
	UpdateSOPTemplate(rec, req)
	if rec.Code != 400 {
		t.Fatalf("update empty code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/x", url.Values{"name": {"保養SOP2"}, "repair_type": {"repair"}})
	req.SetPathValue("id", itoa64(tplID))
	UpdateSOPTemplate(rec, req)
	if rec.Code != 302 {
		t.Fatalf("update code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/999", url.Values{"name": {"x"}})
	req.SetPathValue("id", "999")
	UpdateSOPTemplate(rec, req)
	if rec.Code != 404 {
		t.Fatalf("update missing code=%d", rec.Code)
	}

	// Create item: bad id, empty desc, valid
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/0/items", url.Values{"description": {"檢查"}})
	req.SetPathValue("template_id", "0")
	CreateSOPItem(rec, req)
	if rec.Code != 400 {
		t.Fatalf("item bad id code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/x/items", url.Values{})
	req.SetPathValue("template_id", itoa64(tplID))
	CreateSOPItem(rec, req)
	if rec.Code != 400 {
		t.Fatalf("item empty code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/x/items", url.Values{"description": {"檢查煞車"}, "required": {"1"}})
	req.SetPathValue("template_id", itoa64(tplID))
	CreateSOPItem(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("item ok code=%d body=%s", rec.Code, rec.Body.String())
	}
	itemID := batchBQueryInt(t, `SELECT id FROM sop_items WHERE template_id=?`, tplID)

	// Replace items: missing template, valid (update existing + add new)
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/999/items", url.Values{"item_desc": {"a"}})
	req.SetPathValue("id", "999")
	ReplaceSOPItems(rec, req)
	if rec.Code != 404 {
		t.Fatalf("replace missing code=%d", rec.Code)
	}
	form := url.Values{}
	form["item_desc"] = []string{"檢查煞車改", "換機油"}
	form["required"] = []string{"1", "0"}
	form["item_id"] = []string{itoa64(itemID), "0"}
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/x/items", form)
	req.SetPathValue("id", itoa64(tplID))
	ReplaceSOPItems(rec, req)
	if rec.Code != 302 {
		t.Fatalf("replace code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM active_sop_items WHERE template_id=?`, tplID); n != 2 {
		t.Fatalf("items after replace=%d", n)
	}

	// Attach: bad ids, missing template, valid
	rec = httptest.NewRecorder()
	req = postReq(t, "/work-orders/1/sop", url.Values{})
	req.SetPathValue("id", "1")
	AttachSOPTemplate(rec, req)
	if rec.Code != 400 {
		t.Fatalf("attach bad code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/work-orders/1/sop", url.Values{"template_id": {"999"}})
	req.SetPathValue("id", "1")
	AttachSOPTemplate(rec, req)
	if rec.Code != 400 {
		t.Fatalf("attach missing tpl code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/work-orders/1/sop", url.Values{"template_id": {itoa64(tplID)}})
	req.SetPathValue("id", "1")
	AttachSOPTemplate(rec, req)
	if rec.Code != 302 {
		t.Fatalf("attach code=%d body=%s", rec.Code, rec.Body.String())
	}
	recID := batchBQueryInt(t, `SELECT id FROM work_order_sop_records WHERE work_order_id=1 LIMIT 1`)

	// Toggle on then off; missing record 404
	rec = httptest.NewRecorder()
	req = postReq(t, "/work-orders/1/sop-records/x/toggle", url.Values{})
	req.SetPathValue("id", "1")
	req.SetPathValue("record_id", itoa64(recID))
	ToggleSOPRecord(rec, req, batchDOwner())
	if rec.Code != 302 {
		t.Fatalf("toggle code=%d", rec.Code)
	}
	if n := batchBQueryInt(t, `SELECT completed FROM work_order_sop_records WHERE id=?`, recID); n != 1 {
		t.Fatalf("completed=%d want 1", n)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/work-orders/1/sop-records/999/toggle", url.Values{})
	req.SetPathValue("id", "1")
	req.SetPathValue("record_id", "999")
	ToggleSOPRecord(rec, req, batchDOwner())
	if rec.Code != 404 {
		t.Fatalf("toggle missing code=%d", rec.Code)
	}

	// Detach
	rec = httptest.NewRecorder()
	req = postReq(t, "/work-orders/1/sop/x/detach", url.Values{})
	req.SetPathValue("id", "1")
	req.SetPathValue("template_id", itoa64(tplID))
	DetachSOPTemplate(rec, req)
	if rec.Code != 302 {
		t.Fatalf("detach code=%d", rec.Code)
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM work_order_sop_templates WHERE work_order_id=1`); n != 0 {
		t.Fatalf("attached after detach=%d", n)
	}

	// Delete template
	rec = httptest.NewRecorder()
	req = postReq(t, "/sop-templates/x/delete", url.Values{})
	req.SetPathValue("id", itoa64(tplID))
	DeleteSOPTemplate(rec, req)
	if rec.Code != 302 {
		t.Fatalf("delete tpl code=%d", rec.Code)
	}
	_ = itemID
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// ---------- line_webhook.go ----------

func TestBatchDLineWebhook(t *testing.T) {
	log_line("batchD probe %s", "x")

	newMigratedTestDB(t)
	batchDExec(t, `INSERT INTO customers (id,name,phone) VALUES (1,'王小明','0912-345678'),(2,'李大華','0912345678'),(3,'陳小華','0912 345678')`)
	batchDExec(t, `INSERT INTO vehicles (id,customer_id,plate_number,brand_model) VALUES (1,1,'ABC-1234','TOYOTA')`)
	batchDExec(t, `INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount,current_mileage) VALUES (1,1,1,'in_progress',0,50000)`)

	// isBound / bindLineUser
	if isBound("U1") {
		t.Fatal("U1 should not be bound")
	}
	if err := bindLineUser("", 1); err == nil {
		t.Fatal("empty user should fail")
	}
	if err := bindLineUser("U1", 1); err != nil {
		t.Fatal(err)
	}
	if !isBound("U1") {
		t.Fatal("U1 should be bound")
	}
	if err := bindLineUser("U1", 2); err == nil {
		t.Fatal("duplicate line user should fail")
	}
	if err := bindLineUser("U2", 1); err == nil {
		t.Fatal("customer already bound should fail")
	}
	if err := bindLineUser("U2", 999); err == nil {
		t.Fatal("missing customer should fail")
	}

	// beginBinding: bound user (empty reply token so nil client is safe) + new user
	beginBinding(nil, line.FollowEvent{UserID: "U1"})
	beginBinding(nil, line.FollowEvent{UserID: "U9"})
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM notifications WHERE kind='line_follow'`); n < 1 {
		t.Fatalf("follow notifications=%d", n)
	}

	// tryBindByPhone branches
	if _, done := tryBindByPhone(nil, line.MessageEvent{UserID: "U1", Text: "0912345678"}); done {
		t.Fatal("bound user should not be handled by tryBind")
	}
	if _, done := tryBindByPhone(nil, line.MessageEvent{UserID: "U8", Text: "hello"}); done {
		t.Fatal("non-phone should not be handled")
	}
	msg, done := tryBindByPhone(nil, line.MessageEvent{UserID: "U8", Text: "0999888777"})
	if !done || !strings.Contains(msg, "查無") {
		t.Fatalf("unknown phone msg=%q done=%v", msg, done)
	}
	msg, done = tryBindByPhone(nil, line.MessageEvent{UserID: "U8", Text: "0912345678"})
	if !done || !strings.Contains(msg, "筆客戶紀錄") {
		t.Fatalf("dup phone msg=%q done=%v", msg, done)
	}
	// unique phone: customer 1 already bound to U1, so bind will fail -> handled with error text
	// use a fresh customer with unique phone
	batchDExec(t, `INSERT INTO customers (id,name,phone) VALUES (4,'唯一','0987654321')`)
	msg, done = tryBindByPhone(nil, line.MessageEvent{UserID: "U7", Text: "0987 654321"})
	if !done || !strings.Contains(msg, "已為您連結") {
		t.Fatalf("bind msg=%q done=%v", msg, done)
	}
	if !isBound("U7") {
		t.Fatal("U7 should be bound after tryBind")
	}

	// handlePostback: deprecated approve + unknown (nil client safe)
	handlePostback(nil, line.PostbackEvent{Data: "approve:1"})
	handlePostback(nil, line.PostbackEvent{Data: "something-else"})

	// handleCustomerMessage branches
	if got := handleCustomerMessage(nil, line.MessageEvent{UserID: "U1", Text: "進度"}); !strings.Contains(got, "ABC-1234") {
		t.Fatalf("progress got=%q", got)
	}
	if got := handleCustomerMessage(nil, line.MessageEvent{UserID: "UX", Text: "進度"}); !strings.Contains(got, "查不到") {
		t.Fatalf("progress unknown got=%q", got)
	}
	if got := handleCustomerMessage(nil, line.MessageEvent{UserID: "U1", Text: "工單 abc"}); !strings.Contains(got, "工單編號") {
		t.Fatalf("wo parse got=%q", got)
	}
	if got := handleCustomerMessage(nil, line.MessageEvent{UserID: "U1", Text: "工單 1"}); !strings.Contains(got, "ABC-1234") {
		t.Fatalf("wo got=%q", got)
	}
	if got := handleCustomerMessage(nil, line.MessageEvent{UserID: "U1", Text: "工單 999"}); !strings.Contains(got, "找不到") {
		t.Fatalf("wo missing got=%q", got)
	}
	if got := handleCustomerMessage(nil, line.MessageEvent{UserID: "U1", Text: "履歷"}); got == "" {
		t.Fatal("passbook summary empty")
	}
	if got := handleCustomerMessage(nil, line.MessageEvent{UserID: "U1", Text: "你好"}); !strings.Contains(got, "進度") {
		t.Fatalf("default got=%q", got)
	}

	// clientIP
	r := batchDGetReq(t, "/")
	r.Header.Set("Cf-Connecting-Ip", "1.2.3.4")
	if clientIP(r) != "cf:1.2.3.4" {
		t.Fatal("clientIP cf")
	}
	r.Header.Del("Cf-Connecting-Ip")
	r.RemoteAddr = "5.6.7.8:9000"
	if clientIP(r) != "5.6.7.8" {
		t.Fatal("clientIP hostport")
	}
	r.RemoteAddr = "weird"
	if clientIP(r) != "weird" {
		t.Fatal("clientIP fallback")
	}

	// LineWebhook HTTP paths
	t.Setenv("LINE_CHANNEL_ID", "test-id")
	t.Setenv("LINE_CHANNEL_SECRET", "test-secret")
	t.Setenv("LINE_CHANNEL_TOKEN", "")
	line.ResetSharedForTest()
	t.Cleanup(func() { line.ResetSharedForTest() })

	rec := httptest.NewRecorder()
	LineWebhook(rec, batchDGetReq(t, "/line/webhook"))
	if rec.Code != 405 {
		t.Fatalf("GET code=%d", rec.Code)
	}
	// bad signature
	body := []byte(`{"events":[]}`)
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/line/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Line-Signature", "bad")
	LineWebhook(rec, req)
	if rec.Code != 401 {
		t.Fatalf("bad sig code=%d", rec.Code)
	}
	// valid signature, empty events
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/line/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-Line-Signature", sig)
	LineWebhook(rec, req)
	if rec.Code != 200 {
		t.Fatalf("empty events code=%d body=%s", rec.Code, rec.Body.String())
	}
	// valid signature, message event that binds by phone (mock reply, no token)
	body2 := []byte(`{"events":[{"type":"message","replyToken":"tok","source":{"type":"user","userId":"U55"},"message":{"type":"text","text":"0912345678"}}]}`)
	_ = body2 // duplicate phone -> handled, still 200
	mac = hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(body2)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/line/webhook", strings.NewReader(string(body2)))
	req.Header.Set("X-Line-Signature", hex.EncodeToString(mac.Sum(nil)))
	LineWebhook(rec, req)
	if rec.Code != 200 {
		t.Fatalf("message code=%d", rec.Code)
	}
	// valid signature, follow + postback events
	body3 := []byte(`{"events":[{"type":"follow","replyToken":"","source":{"type":"user","userId":"U66"}},{"type":"postback","source":{"type":"user","userId":"U66"},"postback":{"data":"approve:1"}}]}`)
	mac = hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(body3)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/line/webhook", strings.NewReader(string(body3)))
	req.Header.Set("X-Line-Signature", hex.EncodeToString(mac.Sum(nil)))
	LineWebhook(rec, req)
	if rec.Code != 200 {
		t.Fatalf("follow/postback code=%d", rec.Code)
	}
	// malformed JSON with valid signature
	body4 := []byte(`{not json`)
	mac = hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(body4)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/line/webhook", strings.NewReader(string(body4)))
	req.Header.Set("X-Line-Signature", hex.EncodeToString(mac.Sum(nil)))
	LineWebhook(rec, req)
	if rec.Code != 400 {
		t.Fatalf("bad json code=%d", rec.Code)
	}

	// unconfigured -> 503
	t.Setenv("LINE_CHANNEL_SECRET", "")
	line.ResetSharedForTest()
	rec = httptest.NewRecorder()
	LineWebhook(rec, httptest.NewRequest("POST", "/line/webhook", strings.NewReader(`{"events":[]}`)))
	if rec.Code != 503 {
		t.Fatalf("unconfigured code=%d", rec.Code)
	}
	line.ResetSharedForTest()
}

// ---------- receivables.go ----------

func TestBatchDReceivables(t *testing.T) {
	batchBFileDB(t)
	batchBSeedRows(t)
	batchBExec(t, `INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount,current_mileage,approval_status,created_at)
		VALUES (1,1,1,'completed',1000,50000,'approved','2026-01-01')`)
	batchBExec(t, `INSERT INTO receivables (id,voucher_no,customer_id,vehicle_id,work_order_id,pay_date,detail_total,real_total,discount,tax,pay_total,cash,card,check_amount,transfer_amount,voided,source)
		VALUES (1,'RCV-20260101-001',1,1,1,'2026-01-01',1000,1000,0,0,1000,1000,0,0,0,0,'manual')`)
	owner := batchDOwner()
	tech := model.User{ID: 9, Role: "technician"}

	// New form: forbidden, plain, with WO, missing WO
	rec := httptest.NewRecorder()
	NewReceivableForm(rec, batchDGetReq(t, "/receivables/new"), tech)
	if rec.Code != 403 {
		t.Fatalf("new forbidden code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	NewReceivableForm(rec, batchDGetReq(t, "/receivables/new"), owner)
	if rec.Code != 200 {
		t.Fatalf("new code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	NewReceivableForm(rec, batchDGetReq(t, "/receivables/new?work_order=1"), owner)
	if rec.Code != 200 {
		t.Fatalf("new with wo code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	NewReceivableForm(rec, batchDGetReq(t, "/receivables/new?work_order=999"), owner)
	if rec.Code != 404 {
		t.Fatalf("new missing wo code=%d", rec.Code)
	}

	// Create: unknown customer, negative, mismatch, empty voucher, valid
	rec = httptest.NewRecorder()
	CreateReceivable(rec, postReq(t, "/receivables", url.Values{"customer_id": {"999"}, "voucher_no": {"V1"}, "cash": {"100"}}))
	if rec.Code != 302 {
		t.Fatalf("create bad cust code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateReceivable(rec, postReq(t, "/receivables", url.Values{"customer_id": {"1"}, "voucher_no": {"V1"}, "cash": {"-5"}}))
	if rec.Code != 302 {
		t.Fatalf("create negative code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateReceivable(rec, postReq(t, "/receivables", url.Values{"customer_id": {"1"}, "voucher_no": {"V1"}, "cash": {"100"}, "pay_total": {"999"}}))
	if rec.Code != 302 {
		t.Fatalf("create mismatch code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateReceivable(rec, postReq(t, "/receivables", url.Values{"customer_id": {"1"}, "cash": {"100"}}))
	if rec.Code != 302 {
		t.Fatalf("create empty voucher code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateReceivable(rec, postReq(t, "/receivables", url.Values{
		"customer_id": {"1"}, "voucher_no": {"RCV-NEW-1"}, "cash": {"500"}, "pay_date": {"2026-01-02"},
		"detail_total": {"500"}, "real_total": {"500"},
	}))
	if rec.Code != 302 {
		t.Fatalf("create valid code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM receivables WHERE voucher_no='RCV-NEW-1'`); n != 1 {
		t.Fatalf("created receivable count=%d", n)
	}

	// Edit form: forbidden, found, missing
	rec = httptest.NewRecorder()
	req := batchDGetReq(t, "/receivables/1/edit")
	req.SetPathValue("id", "1")
	EditReceivableForm(rec, req, tech)
	if rec.Code != 403 {
		t.Fatalf("edit forbidden code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/receivables/1/edit")
	req.SetPathValue("id", "1")
	EditReceivableForm(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("edit code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/receivables/999/edit")
	req.SetPathValue("id", "999")
	EditReceivableForm(rec, req, owner)
	if rec.Code != 404 {
		t.Fatalf("edit missing code=%d", rec.Code)
	}

	// Update: negative, mismatch, empty voucher, valid, missing
	rec = httptest.NewRecorder()
	req = postReq(t, "/receivables/1", url.Values{"voucher_no": {"V"}, "cash": {"-1"}})
	req.SetPathValue("id", "1")
	UpdateReceivable(rec, req)
	if rec.Code != 400 {
		t.Fatalf("update negative code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/receivables/1", url.Values{"voucher_no": {"V"}, "cash": {"100"}, "pay_total": {"200"}})
	req.SetPathValue("id", "1")
	UpdateReceivable(rec, req)
	if rec.Code != 400 {
		t.Fatalf("update mismatch code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/receivables/1", url.Values{"cash": {"100"}})
	req.SetPathValue("id", "1")
	UpdateReceivable(rec, req)
	if rec.Code != 400 {
		t.Fatalf("update empty voucher code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/receivables/1", url.Values{"voucher_no": {"RCV-20260101-001"}, "cash": {"800"}, "pay_date": {"2026-01-03"}})
	req.SetPathValue("id", "1")
	UpdateReceivable(rec, req)
	if rec.Code != 302 {
		t.Fatalf("update valid code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT CAST(cash AS INTEGER) FROM receivables WHERE id=1`); n != 800 {
		t.Fatalf("cash after update=%d", n)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/receivables/999", url.Values{"voucher_no": {"V"}, "cash": {"100"}})
	req.SetPathValue("id", "999")
	UpdateReceivable(rec, req)
	if rec.Code != 404 {
		t.Fatalf("update missing code=%d", rec.Code)
	}

	// Void: missing id 404, valid, already-voided
	rec = httptest.NewRecorder()
	req = postReq(t, "/receivables/0/void", url.Values{})
	req.SetPathValue("id", "0")
	VoidReceivable(rec, req)
	if rec.Code != 404 {
		t.Fatalf("void zero code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/receivables/999/void", url.Values{})
	req.SetPathValue("id", "999")
	VoidReceivable(rec, req)
	if rec.Code != 302 {
		t.Fatalf("void missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/receivables/1/void", url.Values{})
	req.SetPathValue("id", "1")
	VoidReceivable(rec, req)
	if rec.Code != 302 {
		t.Fatalf("void code=%d", rec.Code)
	}
	if n := batchBQueryInt(t, `SELECT voided FROM receivables WHERE id=1`); n != 1 {
		t.Fatalf("voided=%d", n)
	}

	// List + Search
	for _, path := range []string{"/receivables", "/receivables?q=RCV&method=cash&from=2026-01-01&to=2026-12-31&voided=1&unpaid=1"} {
		rec = httptest.NewRecorder()
		ListReceivables(rec, batchDGetReq(t, path), owner)
		if rec.Code != 200 {
			t.Fatalf("list %s code=%d", path, rec.Code)
		}
	}
	rec = httptest.NewRecorder()
	ListReceivables(rec, batchDGetReq(t, "/receivables"), tech)
	if rec.Code != 403 {
		t.Fatalf("list forbidden code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchReceivablesAPI(rec, batchDGetReq(t, "/receivables/search?q=RCV"), owner)
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	SearchReceivablesAPI(rec, batchDGetReq(t, "/receivables/search"), tech)
	if rec.Code != 403 {
		t.Fatalf("search forbidden code=%d", rec.Code)
	}
}

// ---------- legacy_crud.go ----------

func TestBatchDLegacyCRUD(t *testing.T) {
	newMigratedTestDB(t)
	batchBSeedRows(t)
	batchDExec(t, `INSERT INTO insurance_companies (id,code,name,active) VALUES (1,'CT','國泰',1)`)

	// Insurance: new form, create bad vehicle / bad expiry / valid, edit, update, delete
	rec := httptest.NewRecorder()
	req := batchDGetReq(t, "/insurances/new?vehicle=1")
	NewVehicleInsuranceForm(rec, req, batchDOwner())
	if rec.Code != 200 {
		t.Fatalf("ins new code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateVehicleInsurance(rec, postReq(t, "/insurances", url.Values{"vehicle_id": {"999"}}))
	if rec.Code != 400 {
		t.Fatalf("ins create bad veh code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateVehicleInsurance(rec, postReq(t, "/insurances", url.Values{"vehicle_id": {"1"}, "expiry_date": {"not-a-date"}}))
	if rec.Code != 400 {
		t.Fatalf("ins create bad expiry code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateVehicleInsurance(rec, postReq(t, "/insurances", url.Values{
		"vehicle_id": {"1"}, "insurance_company_id": {"1"}, "expiry_date": {"2027-01-01"},
		"policy_number": {"P-1"}, "company_no": {"C1"},
	}))
	if rec.Code != 302 {
		t.Fatalf("ins create code=%d body=%s", rec.Code, rec.Body.String())
	}
	insID := batchBQueryInt(t, `SELECT id FROM vehicle_insurances WHERE policy_number='P-1'`)
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/insurances/1/edit")
	req.SetPathValue("id", itoa64(insID))
	EditVehicleInsuranceForm(rec, req, batchDOwner())
	if rec.Code != 200 {
		t.Fatalf("ins edit code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/insurances/999/edit")
	req.SetPathValue("id", "999")
	EditVehicleInsuranceForm(rec, req, batchDOwner())
	if rec.Code != 404 {
		t.Fatalf("ins edit missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/insurances/1", url.Values{"expiry_date": {"bad"}})
	req.SetPathValue("id", itoa64(insID))
	UpdateVehicleInsurance(rec, req)
	if rec.Code != 400 {
		t.Fatalf("ins update bad expiry code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/insurances/1", url.Values{"insurance_company_id": {"1"}, "expiry_date": {"2027-06-01"}, "policy_number": {"P-2"}})
	req.SetPathValue("id", itoa64(insID))
	UpdateVehicleInsurance(rec, req)
	if rec.Code != 302 {
		t.Fatalf("ins update code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/insurances/999", url.Values{"expiry_date": {"2027-01-01"}})
	req.SetPathValue("id", "999")
	UpdateVehicleInsurance(rec, req)
	if rec.Code != 404 {
		t.Fatalf("ins update missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/insurances/1/delete", url.Values{})
	req.SetPathValue("id", itoa64(insID))
	DeleteVehicleInsurance(rec, req)
	if rec.Code != 302 {
		t.Fatalf("ins delete code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/insurances/999/delete", url.Values{})
	req.SetPathValue("id", "999")
	DeleteVehicleInsurance(rec, req)
	if rec.Code != 404 {
		t.Fatalf("ins delete missing code=%d", rec.Code)
	}

	// Inspection: new, create bad/empty/valid, edit, update, delete
	rec = httptest.NewRecorder()
	NewVehicleInspectionForm(rec, batchDGetReq(t, "/inspections/new?vehicle=1"), batchDOwner())
	if rec.Code != 200 {
		t.Fatalf("insp new code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateVehicleInspection(rec, postReq(t, "/inspections", url.Values{"vehicle_id": {"999"}, "order_date": {"2026-03-01"}}))
	if rec.Code != 400 {
		t.Fatalf("insp bad veh code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateVehicleInspection(rec, postReq(t, "/inspections", url.Values{"vehicle_id": {"1"}}))
	if rec.Code != 400 {
		t.Fatalf("insp empty date code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	CreateVehicleInspection(rec, postReq(t, "/inspections", url.Values{
		"vehicle_id": {"1"}, "order_date": {"2026-03-01"}, "check_date": {"2026-03-10"}, "firm_no": {"F1"},
	}))
	if rec.Code != 302 {
		t.Fatalf("insp create code=%d", rec.Code)
	}
	inspID := batchBQueryInt(t, `SELECT id FROM vehicle_inspections WHERE firm_no='F1'`)
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/inspections/1/edit")
	req.SetPathValue("id", itoa64(inspID))
	EditVehicleInspectionForm(rec, req, batchDOwner())
	if rec.Code != 200 {
		t.Fatalf("insp edit code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/inspections/999/edit")
	req.SetPathValue("id", "999")
	EditVehicleInspectionForm(rec, req, batchDOwner())
	if rec.Code != 404 {
		t.Fatalf("insp edit missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/inspections/1", url.Values{})
	req.SetPathValue("id", itoa64(inspID))
	UpdateVehicleInspection(rec, req)
	if rec.Code != 400 {
		t.Fatalf("insp update empty code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/inspections/1", url.Values{"order_date": {"2026-04-01"}, "firm_no": {"F2"}})
	req.SetPathValue("id", itoa64(inspID))
	UpdateVehicleInspection(rec, req)
	if rec.Code != 302 {
		t.Fatalf("insp update code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/inspections/999", url.Values{"order_date": {"2026-04-01"}})
	req.SetPathValue("id", "999")
	UpdateVehicleInspection(rec, req)
	if rec.Code != 404 {
		t.Fatalf("insp update missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/inspections/1/delete", url.Values{})
	req.SetPathValue("id", itoa64(inspID))
	DeleteVehicleInspection(rec, req)
	if rec.Code != 302 {
		t.Fatalf("insp delete code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = postReq(t, "/inspections/999/delete", url.Values{})
	req.SetPathValue("id", "999")
	DeleteVehicleInspection(rec, req)
	if rec.Code != 404 {
		t.Fatalf("insp delete missing code=%d", rec.Code)
	}
}

// ---------- workorder_crud.go ----------

func TestBatchDWorkOrderCRUD(t *testing.T) {
	batchBFileDB(t)
	batchBSeedRows(t)
	batchBSeedTech(t, 7)
	batchBExec(t, `INSERT INTO work_orders (id,customer_id,vehicle_id,technician_id,status,total_amount,current_mileage,approval_status,created_at,updated_at)
		VALUES (1,1,1,7,'in_progress',500,50000,'not_required','2026-01-01','2026-01-01'),
		       (2,1,1,0,'pending',0,51000,'not_required','2026-01-02','2026-01-02')`)
	batchBExec(t, `INSERT INTO inventory_items (id,part_number,name,bin_location) VALUES (1,'PN-1','煞車皮','A1')`)
	batchBExec(t, `INSERT INTO work_order_items (work_order_id,inventory_item_id,description,quantity,unit_price,item_type) VALUES (1,1,'煞車皮',2,250,'part')`)

	owner := batchDOwner()
	techUser := model.User{ID: 7, Role: "technician"}

	// List with filters
	for _, path := range []string{
		"/work-orders", "/work-orders?status=in_progress", "/work-orders?q=ABC",
		"/work-orders?tech=0", "/work-orders?tech=7", "/work-orders?part=PN-1",
		"/work-orders?from=2026-01-01&to=2026-12-31", "/work-orders?mine=1",
	} {
		rec := httptest.NewRecorder()
		u := owner
		if strings.Contains(path, "mine=1") {
			u = techUser
		}
		ListWorkOrders(rec, batchDGetReq(t, path), u)
		if rec.Code != 200 {
			t.Fatalf("list %s code=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	// Search API: error branch + filters
	rec := httptest.NewRecorder()
	SearchWorkOrdersAPI(rec, batchDGetReq(t, "/work-orders/search?error=1"))
	if rec.Code != 500 {
		t.Fatalf("search error code=%d", rec.Code)
	}
	for _, path := range []string{"/work-orders/search", "/work-orders/search?q=ABC&status=pending&part=PN&tech=7", "/work-orders/search?tech=0"} {
		rec = httptest.NewRecorder()
		SearchWorkOrdersAPI(rec, batchDGetReq(t, path))
		if rec.Code != 200 {
			t.Fatalf("search %s code=%d", path, rec.Code)
		}
	}
	// woTechFilterOptions + woPartUsage
	if opts := woTechFilterOptions("7"); !strings.Contains(opts, `value="7"`) {
		t.Fatalf("tech options=%q", opts)
	}
	usage := woPartUsage(1)
	if len(usage) != 1 || usage[0].Qty != 2 {
		t.Fatalf("usage=%+v", usage)
	}
	if got := woPartUsage(999); len(got) != 0 {
		t.Fatalf("usage missing=%+v", got)
	}
	// Show: owner ok, missing 404, other technician forbidden
	rec = httptest.NewRecorder()
	req := batchDGetReq(t, "/work-orders/1")
	req.SetPathValue("id", "1")
	ShowWorkOrder(rec, req, owner)
	if rec.Code != 200 {
		t.Fatalf("show code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/work-orders/999")
	req.SetPathValue("id", "999")
	ShowWorkOrder(rec, req, owner)
	if rec.Code != 404 {
		t.Fatalf("show missing code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	req = batchDGetReq(t, "/work-orders/2")
	req.SetPathValue("id", "2")
	ShowWorkOrder(rec, req, techUser) // WO2 unassigned -> technician cannot view
	if rec.Code != 403 {
		t.Fatalf("show forbidden code=%d", rec.Code)
	}
}

// ---------- analytics.go ----------

func TestBatchDAnalytics(t *testing.T) {
	batchBFileDB(t)
	batchBSeedRows(t)
	todayStr := today()
	batchBExec(t, `INSERT INTO work_orders (id,customer_id,vehicle_id,status,total_amount,current_mileage,approval_status,created_at,updated_at,discount_amount)
		VALUES (1,1,1,'paid',2000,50000,'approved',?, ?,100)`, todayStr, todayStr)
	batchBExec(t, `INSERT INTO work_order_items (work_order_id,description,quantity,unit_price,subtotal,item_type) VALUES (1,'工資',1,2000,2000,'labor')`)
	owner := batchDOwner()
	tech := model.User{ID: 9, Role: "technician"}

	// periodRange branches
	for _, gran := range []string{"day", "week", "month", "year", "bogus"} {
		f, to := periodRange(gran, "2026-01-01", "2026-01-31")
		if f == "" || to == "" {
			t.Fatalf("periodRange %s -> %q %q", gran, f, to)
		}
	}

	// AnalyticsHome: forbidden, default, custom valid/invalid, day gran
	rec := httptest.NewRecorder()
	AnalyticsHome(rec, batchDGetReq(t, "/analytics"), tech)
	if rec.Code != 403 {
		t.Fatalf("home forbidden code=%d", rec.Code)
	}
	for _, path := range []string{
		"/analytics", "/analytics?gran=day", "/analytics?gran=week&group=customer",
		"/analytics?gran=custom&from=2026-01-01&to=2026-12-31", "/analytics?gran=bogus&group=bogus",
	} {
		rec = httptest.NewRecorder()
		AnalyticsHome(rec, batchDGetReq(t, path), owner)
		if rec.Code != 200 {
			t.Fatalf("home %s code=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	AnalyticsHome(rec, batchDGetReq(t, "/analytics?gran=custom&from=bad"), owner)
	if rec.Code != 400 {
		t.Fatalf("home custom bad code=%d", rec.Code)
	}

	// AnalyticsExport: unauthenticated -> login redirect; counter -> 403; owner -> CSV
	rec = httptest.NewRecorder()
	AnalyticsExport(rec, batchDGetReq(t, "/analytics/export"))
	if rec.Code != 302 {
		t.Fatalf("export unauth code=%d", rec.Code)
	}
	seedUser(t, 2, "counter", "x")
	rec = httptest.NewRecorder()
	AnalyticsExport(rec, batchDPostWithSession(t, "/analytics/export", url.Values{}, 2))
	if rec.Code != 403 {
		t.Fatalf("export counter code=%d", rec.Code)
	}
	seedUser(t, 1, "owner", "x")
	rec = httptest.NewRecorder()
	AnalyticsExport(rec, batchDPostWithSession(t, "/analytics/export?from=2026-01-01&to=2026-12-31", url.Values{}, 1))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "營收") {
		t.Fatalf("export owner code=%d body=%.200s", rec.Code, rec.Body.String())
	}
}

// ---------- staff.go ----------

func TestBatchDStaff(t *testing.T) {
	// file DB: searchStaffPicker calls branchLabel (QueryRow) inside its rows loop
	batchBFileDB(t)
	seedUser(t, 1, "owner", "x")
	seedUser(t, 2, "counter", "x")
	batchDExec(t, `INSERT INTO users (id,username,password,role,name,active,staff_kind,staff_branch) VALUES (7,'tech','x','technician','技師甲',1,'employee','A')`)

	// searchStaffPicker via SearchStaffAPI format=picker
	for _, path := range []string{"/staff/search?format=picker", "/staff/search?format=picker&q=技師"} {
		rec := httptest.NewRecorder()
		SearchStaffAPI(rec, batchDGetReq(t, path))
		if rec.Code != 200 {
			t.Fatalf("picker %s code=%d", path, rec.Code)
		}
	}
	// plain search too
	rec := httptest.NewRecorder()
	SearchStaffAPI(rec, batchDGetReq(t, "/staff/search?q=技師&kind=employee"))
	if rec.Code != 200 {
		t.Fatalf("search code=%d", rec.Code)
	}

	// renderStaffUpdateError direct
	rec = httptest.NewRecorder()
	req := batchDGetReq(t, "/staff/7/edit")
	req.SetPathValue("id", "7")
	renderStaffUpdateError(rec, req, batchDOwner(), "測試錯誤")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "測試錯誤") {
		t.Fatalf("render error code=%d", rec.Code)
	}

	// UpdateStaff: unauthenticated 401
	rec = httptest.NewRecorder()
	req = postReq(t, "/staff/7", url.Values{"staff_kind": {"employee"}})
	req.SetPathValue("id", "7")
	UpdateStaff(rec, req)
	if rec.Code != 401 {
		t.Fatalf("update unauth code=%d", rec.Code)
	}

	// Owner update with password + insurance change (writes history)
	rec = httptest.NewRecorder()
	req = batchDPostWithSession(t, "/staff/7", url.Values{
		"national_id": {"A123"}, "birthday": {"1990-01-01"},
		"insurance_start": {"2026-01-01"}, "insurance_end": {""},
		"staff_kind": {"employee"}, "staff_branch": {"A"}, "active": {"1"},
		"password": {"newpass123"},
	}, 1)
	req.SetPathValue("id", "7")
	UpdateStaff(rec, req)
	if rec.Code != 302 {
		t.Fatalf("owner update code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM insurance_history WHERE user_id=7`); n != 1 {
		t.Fatalf("history count=%d", n)
	}
	var nid string
	if err := db.DB.QueryRow(`SELECT COALESCE(national_id,'') FROM users WHERE id=7`).Scan(&nid); err != nil || nid != "A123" {
		t.Fatalf("national_id=%q err=%v", nid, err)
	}

	// Counter (non-owner) update must preserve national_id
	rec = httptest.NewRecorder()
	req = batchDPostWithSession(t, "/staff/7", url.Values{
		"national_id": {"HACKED"}, "staff_kind": {"employee"}, "staff_branch": {"B"}, "active": {"1"},
	}, 2)
	req.SetPathValue("id", "7")
	UpdateStaff(rec, req)
	if rec.Code != 302 {
		t.Fatalf("counter update code=%d", rec.Code)
	}
	if err := db.DB.QueryRow(`SELECT COALESCE(national_id,'') FROM users WHERE id=7`).Scan(&nid); err != nil || nid != "A123" {
		t.Fatalf("counter should not change national_id, got %q", nid)
	}
	// invalid kind/branch normalised
	rec = httptest.NewRecorder()
	req = batchDPostWithSession(t, "/staff/7", url.Values{"staff_kind": {"bogus"}, "staff_branch": {"ZZ"}}, 1)
	req.SetPathValue("id", "7")
	UpdateStaff(rec, req)
	if rec.Code != 302 {
		t.Fatalf("normalise update code=%d", rec.Code)
	}
	if n := batchBQueryInt(t, `SELECT COUNT(*) FROM users WHERE id=7 AND staff_kind='employee' AND COALESCE(staff_branch,'')=''`); n != 1 {
		t.Fatalf("normalised kind/branch not applied")
	}
}
