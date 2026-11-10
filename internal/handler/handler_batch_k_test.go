package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// ---------- batchK helpers (unique prefix; reuse newMigratedTestDB/postReq) ----------

func batchKSeedSupplier(t *testing.T, id int64, name string) {
	t.Helper()
	if _, err := db.DB.Exec(`INSERT INTO suppliers (id, name) VALUES (?, ?)`, id, name); err != nil {
		t.Fatalf("seed supplier %d: %v", id, err)
	}
}

func batchKSeedItem(t *testing.T, id int64, supplierID interface{}, part, barcode, name, bin, category string, cost, sell float64, stock, minSafety int, kind string) {
	t.Helper()
	if _, err := db.DB.Exec(`INSERT INTO inventory_items
		(id, supplier_id, part_number, barcode, name, bin_location, category, cost_price, selling_price, stock_quantity, min_safety_stock, item_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, supplierID, part, barcode, name, bin, category, cost, sell, stock, minSafety, kind); err != nil {
		t.Fatalf("seed item %d: %v", id, err)
	}
}

func batchKSeedPhrase(t *testing.T, id int64, phrase, category string, sortOrder, active int) {
	t.Helper()
	if _, err := db.DB.Exec(`INSERT INTO technician_phrases (id, phrase, category, sort_order, active)
		VALUES (?, ?, ?, ?, ?)`, id, phrase, category, sortOrder, active); err != nil {
		t.Fatalf("seed phrase %d: %v", id, err)
	}
}

// batchKDropView drops a view and optionally recreates it with a replacement
// SELECT (used to force Scan errors from real SQLite).
func batchKDropView(t *testing.T, name, as string) {
	t.Helper()
	if _, err := db.DB.Exec(`DROP VIEW IF EXISTS ` + name); err != nil {
		t.Fatalf("drop view %s: %v", name, err)
	}
	if as != "" {
		if _, err := db.DB.Exec(`CREATE VIEW ` + name + ` AS ` + as); err != nil {
			t.Fatalf("create view %s: %v", name, err)
		}
	}
}

func batchKDropTable(t *testing.T, name string) {
	t.Helper()
	if _, err := db.DB.Exec(`DROP TABLE IF EXISTS ` + name); err != nil {
		t.Fatalf("drop table %s: %v", name, err)
	}
}

func batchKSeedInventorySet(t *testing.T) {
	t.Helper()
	batchKSeedSupplier(t, 1, "正廠")
	batchKSeedSupplier(t, 2, "副廠")
	batchKSeedItem(t, 1, 1, "PN-001", "BC-001", "煞車皮", "A-1", "制動", 500, 800, 2, 5, "part")
	batchKSeedItem(t, 2, 2, "PN-002", "BC-002", "機油濾清器", "A-2", "引擎", 100, 200, -1, 3, "part")
	batchKSeedItem(t, 3, nil, "PN-003", "", "無分類品", "B-1", "", 50, 90, 10, 2, "part")
}

// ---------- inventory_list.go ----------

func TestBatchKRenderInventoryList(t *testing.T) {
	owner := model.User{ID: 1, Role: "owner"}
	tech := model.User{ID: 2, Role: "technician"}

	t.Run("owner sees cost, category tabs and negative banner", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, want := range []string{"煞車皮", "機油濾清器", "未分類", "$500", "$800", "庫存為負數"} {
			if !strings.Contains(body, want) {
				t.Errorf("body missing %q", want)
			}
		}
	})

	t.Run("technician hides cost", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory", nil), tech)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "$800") {
			t.Error("selling price should stay visible")
		}
		if strings.Contains(body, "$500") {
			t.Error("technician must not see cost price")
		}
	})

	t.Run("q filter", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory?q=煞車", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "煞車皮") || strings.Contains(body, "機油濾清器") {
			t.Errorf("q filter wrong: %s", body)
		}
	})

	t.Run("low stock filter", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory?low=1", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "煞車皮") || !strings.Contains(body, "機油濾清器") {
			t.Error("low stock items missing")
		}
		if strings.Contains(body, "無分類品") {
			t.Error("non-low item should be filtered out")
		}
	})

	t.Run("category filter", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory?category=制動", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "煞車皮") || strings.Contains(body, "機油濾清器") {
			t.Errorf("category filter wrong: %s", body)
		}
	})

	t.Run("uncategorized filter", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory?category=__none__", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "無分類品") || strings.Contains(body, "煞車皮") {
			t.Errorf("__none__ filter wrong: %s", body)
		}
	})

	t.Run("query error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		batchKDropView(t, "active_inventory_items", "")
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory", nil), owner)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d want 400", rec.Code)
		}
	})

	t.Run("closed db", func(t *testing.T) {
		d := newMigratedTestDB(t)
		if err := d.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory", nil), owner)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d want 400", rec.Code)
		}
	})

	t.Run("scan error skips row", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		if _, err := db.DB.Exec(`INSERT INTO inventory_items
			(part_number, name, bin_location, stock_quantity) VALUES ('PN-BAD', '壞料', 'X-1', 'abc')`); err != nil {
			t.Fatalf("seed bad row: %v", err)
		}
		rec := httptest.NewRecorder()
		ListInventory(rec, httptest.NewRequest("GET", "/inventory", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "壞料") {
			t.Error("row that fails Scan should be skipped")
		}
	})
}

func TestBatchKSearchInventoryAPI(t *testing.T) {
	owner := model.User{ID: 1, Role: "owner"}
	tech := model.User{ID: 2, Role: "technician"}
	counter := model.User{ID: 3, Role: "counter"}

	t.Run("empty q", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		SearchInventoryAPI(rec, httptest.NewRequest("GET", "/api/inventory/search", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
	})

	for _, tc := range []struct {
		name     string
		user     model.User
		wantCost bool
	}{
		{"owner sees cost", owner, true},
		{"technician hides cost", tech, false},
		{"counter hides cost", counter, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newMigratedTestDB(t)
			batchKSeedInventorySet(t)
			rec := httptest.NewRecorder()
			SearchInventoryAPI(rec, httptest.NewRequest("GET", "/api/inventory/search?q=煞車", nil), tc.user)
			if rec.Code != 200 {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, "$800") {
				t.Error("selling price should stay visible")
			}
			if got := strings.Contains(body, "$500"); got != tc.wantCost {
				t.Errorf("cost visible = %t, want %t; body=%s", got, tc.wantCost, body)
			}
		})
	}

	t.Run("query error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		batchKDropView(t, "active_inventory_items", "")
		rec := httptest.NewRecorder()
		SearchInventoryAPI(rec, httptest.NewRequest("GET", "/api/inventory/search?q=煞車", nil), owner)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500", rec.Code)
		}
	})

	t.Run("closed db", func(t *testing.T) {
		d := newMigratedTestDB(t)
		if err := d.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		rec := httptest.NewRecorder()
		SearchInventoryAPI(rec, httptest.NewRequest("GET", "/api/inventory/search?q=煞車", nil), owner)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500", rec.Code)
		}
	})

	t.Run("scan error skips row", func(t *testing.T) {
		newMigratedTestDB(t)
		if _, err := db.DB.Exec(`INSERT INTO inventory_items
			(part_number, name, bin_location, stock_quantity) VALUES ('PN-BAD', '壞料', 'X-1', 'abc')`); err != nil {
			t.Fatalf("seed bad row: %v", err)
		}
		rec := httptest.NewRecorder()
		SearchInventoryAPI(rec, httptest.NewRequest("GET", "/api/inventory/search?q=壞", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "壞料") {
			t.Error("row that fails Scan should be skipped")
		}
	})
}

func TestBatchKSearchPartsAPI(t *testing.T) {
	t.Run("empty q htmx recent items", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		req := httptest.NewRequest("GET", "/api/parts/search", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, req)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("content-type=%q", ct)
		}
		if !strings.Contains(rec.Body.String(), "煞車皮") {
			t.Error("recent items should include seeded part")
		}
	})

	t.Run("empty q htmx db error renders empty fragment", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		batchKDropView(t, "active_inventory_items", "")
		req := httptest.NewRequest("GET", "/api/parts/search", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, req)
		if rec.Code != 200 {
			t.Fatalf("code=%d want 200", rec.Code)
		}
	})

	t.Run("empty q htmx scan error skips row", func(t *testing.T) {
		newMigratedTestDB(t)
		if _, err := db.DB.Exec(`INSERT INTO inventory_items
			(part_number, name, bin_location, stock_quantity) VALUES ('PN-BAD', '壞料', 'X-1', 'abc')`); err != nil {
			t.Fatalf("seed bad row: %v", err)
		}
		req := httptest.NewRequest("GET", "/api/parts/search", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, req)
		if rec.Code != 200 {
			t.Fatalf("code=%d want 200", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "壞料") {
			t.Error("row that fails Scan should be skipped")
		}
	})

	t.Run("empty q json", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, httptest.NewRequest("GET", "/api/parts/search", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
			t.Errorf("body=%q want []", got)
		}
	})

	t.Run("query htmx fragment", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		req := httptest.NewRequest("GET", "/api/parts/search?q=煞車", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, req)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "煞車皮") || !strings.Contains(body, "$800") {
			t.Errorf("fragment missing part/price: %s", body)
		}
	})

	t.Run("query json", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, httptest.NewRequest("GET", "/api/parts/search?q=煞車", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "煞車皮") || !strings.Contains(body, `"price":800`) {
			t.Errorf("json missing part/price: %s", body)
		}
	})

	t.Run("query htmx db error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		batchKDropView(t, "active_inventory_items", "")
		req := httptest.NewRequest("GET", "/api/parts/search?q=煞車", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500", rec.Code)
		}
	})

	t.Run("query json db error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedInventorySet(t)
		batchKDropView(t, "active_inventory_items", "")
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, httptest.NewRequest("GET", "/api/parts/search?q=煞車", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500", rec.Code)
		}
	})

	t.Run("scan error skips row", func(t *testing.T) {
		newMigratedTestDB(t)
		if _, err := db.DB.Exec(`INSERT INTO inventory_items
			(part_number, name, bin_location, stock_quantity) VALUES ('PN-BAD', '壞料', 'X-1', 'abc')`); err != nil {
			t.Fatalf("seed bad row: %v", err)
		}
		rec := httptest.NewRecorder()
		SearchPartsAPI(rec, httptest.NewRequest("GET", "/api/parts/search?q=壞", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "壞料") {
			t.Error("row that fails Scan should be skipped")
		}
	})
}

func TestBatchKSupplierOptions(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedSupplier(t, 1, "正廠")
		batchKSeedSupplier(t, 2, "副廠")
		got := supplierOptions()
		if !strings.Contains(got, "正廠") || !strings.Contains(got, "副廠") {
			t.Errorf("supplierOptions missing names: %q", got)
		}
		sel := supplierOptionsSel(2)
		if !strings.Contains(sel, `value="2" selected`) {
			t.Errorf("supplierOptionsSel should mark selected: %q", sel)
		}
	})

	t.Run("query error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedSupplier(t, 1, "正廠")
		batchKDropView(t, "active_suppliers", "")
		if got := supplierOptions(); got != "" {
			t.Errorf("supplierOptions = %q want empty", got)
		}
	})

	t.Run("scan error skips row", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedSupplier(t, 1, "正廠")
		batchKDropView(t, "active_suppliers", `SELECT 'abc' AS id, name FROM suppliers`)
		if got := supplierOptions(); got != "" {
			t.Errorf("supplierOptions = %q want empty after scan error", got)
		}
	})

	t.Run("supplierOptionsSel errors", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedSupplier(t, 1, "正廠")
		batchKDropView(t, "active_suppliers", "")
		if got := supplierOptionsSel(1); got != "" {
			t.Errorf("supplierOptionsSel query error = %q want empty", got)
		}
		batchKDropView(t, "active_suppliers", `SELECT 'abc' AS id, name FROM suppliers`)
		if got := supplierOptionsSel(1); got != "" {
			t.Errorf("supplierOptionsSel scan error = %q want empty", got)
		}
	})

	t.Run("inventoryOptions errors", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedItem(t, 1, nil, "PN-001", "BC-001", "煞車皮", "A-1", "制動", 500, 800, 2, 5, "part")
		if got := inventoryOptions(); !strings.Contains(got, "煞車皮") {
			t.Errorf("inventoryOptions happy = %q", got)
		}
		batchKDropView(t, "active_inventory_items", "")
		if got := inventoryOptions(); got != "" {
			t.Errorf("inventoryOptions query error = %q want empty", got)
		}
		batchKDropView(t, "active_inventory_items",
			`SELECT 'abc' AS id, name, part_number, 0 AS selling_price, 0 AS stock_quantity FROM inventory_items`)
		if got := inventoryOptions(); got != "" {
			t.Errorf("inventoryOptions scan error = %q want empty", got)
		}
	})
}

// ---------- phrases.go ----------

func TestBatchKPhrases(t *testing.T) {
	owner := model.User{ID: 1, Role: "owner"}

	t.Run("list happy", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9001, "列表專用人片語", "煞車", 1, 1)
		rec := httptest.NewRecorder()
		ListPhrases(rec, httptest.NewRequest("GET", "/phrases", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "列表專用人片語") {
			t.Error("list should include seeded phrase")
		}
	})

	t.Run("list query error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9001, "煞車皮剩2mm", "煞車", 1, 1)
		batchKDropView(t, "active_technician_phrases", "")
		rec := httptest.NewRecorder()
		ListPhrases(rec, httptest.NewRequest("GET", "/phrases", nil), owner)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d want 400", rec.Code)
		}
	})

	t.Run("list scan error skips row", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9001, "掃瞄錯誤片語", "煞車", 1, 1)
		batchKDropView(t, "active_technician_phrases",
			`SELECT id, phrase, category, sort_order, 'abc' AS active, created_at FROM technician_phrases`)
		rec := httptest.NewRecorder()
		ListPhrases(rec, httptest.NewRequest("GET", "/phrases", nil), owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d want 200", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "掃瞄錯誤片語") {
			t.Error("row that fails Scan should be skipped")
		}
	})

	t.Run("create happy", func(t *testing.T) {
		newMigratedTestDB(t)
		rec := httptest.NewRecorder()
		CreatePhrase(rec, postReq(t, "/phrases", url.Values{
			"phrase": {"測試片語"}, "category": {"煞車"}, "sort_order": {"5"},
		}), owner)
		if rec.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var phrase string
		var sortOrder int
		if err := db.DB.QueryRow(`SELECT phrase, sort_order FROM technician_phrases WHERE category='煞車' ORDER BY id DESC LIMIT 1`).
			Scan(&phrase, &sortOrder); err != nil {
			t.Fatalf("phrase not inserted: %v", err)
		}
		if phrase != "測試片語" || sortOrder != 5 {
			t.Errorf("stored phrase=%q sort=%d", phrase, sortOrder)
		}
	})

	t.Run("create empty phrase htmx", func(t *testing.T) {
		newMigratedTestDB(t)
		req := postReq(t, "/phrases", url.Values{"category": {"煞車"}})
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		CreatePhrase(rec, req, owner)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d want 400", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "片語內容為必填") {
			t.Errorf("body=%q", rec.Body.String())
		}
	})

	t.Run("create db error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKDropTable(t, "technician_phrases")
		rec := httptest.NewRecorder()
		CreatePhrase(rec, postReq(t, "/phrases", url.Values{
			"phrase": {"測試片語"}, "category": {"煞車"},
		}), owner)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d want 400", rec.Code)
		}
	})

	t.Run("edit form 404", func(t *testing.T) {
		newMigratedTestDB(t)
		req := httptest.NewRequest("GET", "/phrases/99999/edit", nil)
		req.SetPathValue("id", "99999")
		rec := httptest.NewRecorder()
		EditPhraseForm(rec, req, owner)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code=%d want 404", rec.Code)
		}
	})

	t.Run("edit form happy", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9002, "機油滲漏", "引擎", 10, 1)
		req := httptest.NewRequest("GET", "/phrases/9002/edit", nil)
		req.SetPathValue("id", "9002")
		rec := httptest.NewRecorder()
		EditPhraseForm(rec, req, owner)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "機油滲漏") {
			t.Error("edit form should include phrase")
		}
	})

	t.Run("update happy active off", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9003, "舊片語", "煞車", 1, 1)
		req := postReq(t, "/phrases/9003/update", url.Values{
			"phrase": {"新片語"}, "category": {"引擎"}, "sort_order": {"7"}, "active": {"0"},
		})
		req.SetPathValue("id", "9003")
		rec := httptest.NewRecorder()
		UpdatePhrase(rec, req, owner)
		if rec.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var phrase, category string
		var sortOrder, active int
		if err := db.DB.QueryRow(`SELECT phrase, category, sort_order, active FROM technician_phrases WHERE id=9003`).
			Scan(&phrase, &category, &sortOrder, &active); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if phrase != "新片語" || category != "引擎" || sortOrder != 7 || active != 0 {
			t.Errorf("stored %+v", []interface{}{phrase, category, sortOrder, active})
		}
	})

	t.Run("update happy default active", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9004, "停用中", "煞車", 1, 0)
		req := postReq(t, "/phrases/9004/update", url.Values{
			"phrase": {"啟用回"}, "category": {"煞車"},
		})
		req.SetPathValue("id", "9004")
		rec := httptest.NewRecorder()
		UpdatePhrase(rec, req, owner)
		if rec.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var active int
		if err := db.DB.QueryRow(`SELECT active FROM technician_phrases WHERE id=9004`).Scan(&active); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if active != 1 {
			t.Errorf("active=%d want 1", active)
		}
	})

	t.Run("update db error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKDropTable(t, "technician_phrases")
		req := postReq(t, "/phrases/9003/update", url.Values{"phrase": {"新片語"}})
		req.SetPathValue("id", "9003")
		rec := httptest.NewRecorder()
		UpdatePhrase(rec, req, owner)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d want 400", rec.Code)
		}
	})

	t.Run("delete happy", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9005, "待刪片語", "煞車", 1, 1)
		req := httptest.NewRequest("POST", "/phrases/9005/delete", nil)
		req.SetPathValue("id", "9005")
		rec := httptest.NewRecorder()
		DeletePhrase(rec, req, owner)
		if rec.Code != http.StatusFound {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		var deletedAt interface{}
		if err := db.DB.QueryRow(`SELECT deleted_at FROM technician_phrases WHERE id=9005`).Scan(&deletedAt); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if deletedAt == nil {
			t.Error("deleted_at should be set")
		}
	})

	t.Run("delete db error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKDropTable(t, "technician_phrases")
		req := httptest.NewRequest("POST", "/phrases/9005/delete", nil)
		req.SetPathValue("id", "9005")
		rec := httptest.NewRecorder()
		DeletePhrase(rec, req, owner)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("code=%d want 400", rec.Code)
		}
	})

	t.Run("search json with q and category", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9006, "搜尋片語甲", "煞車", 1, 1)
		batchKSeedPhrase(t, 9007, "搜尋片語乙", "引擎", 10, 1)
		rec := httptest.NewRecorder()
		SearchPhrasesAPI(rec, httptest.NewRequest("GET", "/api/phrases/search?q=搜尋&category=煞車", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "搜尋片語甲") || strings.Contains(body, "搜尋片語乙") {
			t.Errorf("search filter wrong: %s", body)
		}
	})

	t.Run("search htmx fragment", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9006, "搜尋片語甲", "煞車", 1, 1)
		req := httptest.NewRequest("GET", "/api/phrases/search?q=搜尋", nil)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		SearchPhrasesAPI(rec, req)
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "搜尋片語甲") {
			t.Errorf("fragment missing phrase: %s", rec.Body.String())
		}
	})

	t.Run("search query error", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9006, "煞車皮剩2mm", "煞車", 1, 1)
		batchKDropView(t, "active_technician_phrases", "")
		rec := httptest.NewRecorder()
		SearchPhrasesAPI(rec, httptest.NewRequest("GET", "/api/phrases/search?q=煞車", nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("code=%d want 500", rec.Code)
		}
	})

	t.Run("search scan error skips row", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9006, "掃瞄錯誤片語", "煞車", 1, 1)
		batchKDropView(t, "active_technician_phrases",
			`SELECT 'abc' AS id, phrase, category, sort_order, active, deleted_at FROM technician_phrases`)
		rec := httptest.NewRecorder()
		SearchPhrasesAPI(rec, httptest.NewRequest("GET", "/api/phrases/search?q=掃瞄", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d want 200", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "掃瞄錯誤片語") {
			t.Error("row that fails Scan should be skipped")
		}
	})

	t.Run("estimate items html", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9006, "估價專用人片語", "煞車", 1, 1)
		batchKSeedItem(t, 10, nil, "PN-900", "BC-900", "估價專用零件", "A-9", "制動", 500, 800, 3, 5, "part")
		rec := httptest.NewRecorder()
		SearchEstimateItemsAPI(rec, httptest.NewRequest("GET", "/api/estimate/items?q=估價", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "估價專用人片語") || !strings.Contains(body, "$800") {
			t.Errorf("estimate items missing phrase/part: %s", body)
		}
	})

	t.Run("estimate items db error still renders", func(t *testing.T) {
		newMigratedTestDB(t)
		batchKSeedPhrase(t, 9006, "煞車皮剩2mm", "煞車", 1, 1)
		batchKDropView(t, "active_technician_phrases", "")
		batchKDropView(t, "active_inventory_items", "")
		rec := httptest.NewRecorder()
		SearchEstimateItemsAPI(rec, httptest.NewRequest("GET", "/api/estimate/items?q=煞車", nil))
		if rec.Code != 200 {
			t.Fatalf("code=%d want 200", rec.Code)
		}
	})
}

func TestBatchKGeneratePhrasesHTML(t *testing.T) {
	got := generatePhrasesHTML([]model.TechnicianPhrase{
		{ID: 1, Phrase: "煞車皮剩2mm", Category: "煞車", SortOrder: 1, Active: 1},
		{ID: 2, Phrase: "機油滲漏", Category: "引擎", SortOrder: 2, Active: 0},
	})
	for _, want := range []string{"煞車皮剩2mm", "機油滲漏", "啟用", "停用", "/phrases/1/edit", "/phrases/2/delete"} {
		if !strings.Contains(got, want) {
			t.Errorf("html missing %q", want)
		}
	}
}
