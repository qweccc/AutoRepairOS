package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"autorepairos/internal/db"
	_ "modernc.org/sqlite"
)

func setupConcurrentInventoryDB(t *testing.T) {
	t.Helper()
	original := db.DB
	t.Cleanup(func() { db.DB = original })

	path := filepath.ToSlash(filepath.Join(t.TempDir(), "inventory-concurrency.db"))
	d, err := sql.Open("sqlite", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = d.Close() })
	db.DB = d
	createPurchaseTestSchema(t, d)
}

func insertStockAndPurchase(t *testing.T, stock int, quantity float64, status string) {
	t.Helper()
	_, err := db.DB.Exec(`INSERT INTO inventory_items
		(id, part_number, name, bin_location, stock_quantity) VALUES (1, 'TEST-1', '測試零件', 'A-1', ?)`, stock)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec(`INSERT INTO purchase_orders (id, supplier_id, order_date, status)
		VALUES (1, 1, '2026-10-16', ?)`, status)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB.Exec(`INSERT INTO purchase_order_items
		(id, order_id, inventory_item_id, description, quantity, unit_cost)
		VALUES (1, 1, 1, '測試零件', ?, 10)`, quantity)
	if err != nil {
		t.Fatal(err)
	}
}

func performStockAdjust(delta int) *httptest.ResponseRecorder {
	form := url.Values{"delta": {strconv.Itoa(delta)}, "note": {"併發測試"}}
	req := httptest.NewRequest(http.MethodPost, "/inventory/1/stock", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	recorder := httptest.NewRecorder()
	ApplyStockAdjust(recorder, req)
	return recorder
}

func performPurchaseReceive() *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/purchase-orders/1/receive", nil)
	req.SetPathValue("id", "1")
	recorder := httptest.NewRecorder()
	ReceivePurchaseOrder(recorder, req)
	return recorder
}

func TestApplyStockAdjustRecordsReturnedQuantitiesAndRejectsInsufficientStock(t *testing.T) {
	setupConcurrentInventoryDB(t)
	insertStockAndPurchase(t, 5, 2, "draft")

	if got := performStockAdjust(-3); got.Code != http.StatusFound {
		t.Fatalf("successful adjustment status = %d, want %d; body: %s", got.Code, http.StatusFound, got.Body.String())
	}
	var stock, delta, before, after int
	if err := db.DB.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=1`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(`SELECT delta, before_quantity, after_quantity FROM inventory_movements`).Scan(&delta, &before, &after); err != nil {
		t.Fatal(err)
	}
	if stock != 2 || delta != -3 || before != 5 || after != 2 {
		t.Fatalf("stock/movement = %d / (%d, %d, %d), want 2 / (-3, 5, 2)", stock, delta, before, after)
	}

	if got := performStockAdjust(-3).Code; got != http.StatusBadRequest {
		t.Fatalf("insufficient adjustment status = %d, want %d", got, http.StatusBadRequest)
	}
	var movements int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM inventory_movements`).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if movements != 1 {
		t.Fatalf("movement count after rejected adjustment = %d, want 1", movements)
	}
	if err := db.DB.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=1`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 2 {
		t.Fatalf("stock after rejected adjustment = %d, want 2", stock)
	}
}

func TestConcurrentStockAdjustmentsDoNotOversellOrMisreport(t *testing.T) {
	setupConcurrentInventoryDB(t)
	insertStockAndPurchase(t, 10, 1, "draft")

	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			ready.Done()
			<-start
			results <- performStockAdjust(-7)
		}()
	}
	ready.Wait()
	close(start)
	first, second := <-results, <-results
	if !((first.Code == http.StatusFound && second.Code == http.StatusBadRequest) ||
		(first.Code == http.StatusBadRequest && second.Code == http.StatusFound)) {
		t.Fatalf("concurrent adjustment statuses = %d (%s), %d (%s); want one success and one insufficient-stock response",
			first.Code, first.Body.String(), second.Code, second.Body.String())
	}

	var stock, count, delta, before, after int
	if err := db.DB.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=1`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(delta),0), COALESCE(MAX(before_quantity),0),
		COALESCE(MAX(after_quantity),0) FROM inventory_movements`).Scan(&count, &delta, &before, &after); err != nil {
		t.Fatal(err)
	}
	if stock != 3 || count != 1 || delta != -7 || before != 10 || after != 3 {
		t.Fatalf("stock/movement = %d / (%d, %d, %d, %d), want 3 / (1, -7, 10, 3)", stock, count, delta, before, after)
	}
}

func TestConcurrentPurchaseReceiveOnlyStocksOnce(t *testing.T) {
	setupConcurrentInventoryDB(t)
	insertStockAndPurchase(t, 4, 3, "draft")

	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			ready.Done()
			<-start
			results <- performPurchaseReceive()
		}()
	}
	ready.Wait()
	close(start)
	first, second := <-results, <-results
	if !((first.Code == http.StatusFound && second.Code == http.StatusBadRequest) ||
		(first.Code == http.StatusBadRequest && second.Code == http.StatusFound)) {
		t.Fatalf("concurrent receive statuses = %d (%s), %d (%s); want one success and one rejection",
			first.Code, first.Body.String(), second.Code, second.Body.String())
	}

	var stock, movements, before, after int
	if err := db.DB.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=1`).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(before_quantity),0), COALESCE(MAX(after_quantity),0)
		FROM inventory_movements`).Scan(&movements, &before, &after); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM purchase_orders WHERE id=1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if stock != 7 || movements != 1 || before != 4 || after != 7 || status != "received" {
		t.Fatalf("receipt state = stock:%d movements:%d before:%d after:%d status:%q; want 7, 1, 4, 7, received",
			stock, movements, before, after, status)
	}
}

func TestPurchaseReceiveWithoutItemsRollsBackClaim(t *testing.T) {
	setupConcurrentInventoryDB(t)
	if _, err := db.DB.Exec(`INSERT INTO purchase_orders (id, supplier_id, order_date, status)
		VALUES (1, 1, '2026-10-16', 'draft')`); err != nil {
		t.Fatal(err)
	}
	if got := performPurchaseReceive(); got.Code != http.StatusBadRequest {
		t.Fatalf("empty receipt status = %d, want %d; body: %s", got.Code, http.StatusBadRequest, got.Body.String())
	}
	var status string
	if err := db.DB.QueryRow(`SELECT status FROM purchase_orders WHERE id=1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "draft" {
		t.Fatalf("status after rejected empty receipt = %q, want draft", status)
	}
}
