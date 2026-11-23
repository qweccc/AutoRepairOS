package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

func stPost(target string, form url.Values, paths map[string]string) *http.Request {
	req := httptest.NewRequest("POST", target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range paths {
		req.SetPathValue(k, v)
	}
	return req
}

func seedStocktakeParts(t *testing.T) {
	t.Helper()
	_, err := db.DB.Exec(`INSERT INTO inventory_items
		(id, part_number, name, category, bin_location, cost_price, selling_price, stock_quantity, min_safety_stock)
		VALUES (901,'P-A','零件A','耗材','A1',100,200,10,5),
		       (902,'P-B','零件B','耗材','A1',100,200,-39,5),
		       (903,'P-C','零件C','油品','B2',100,200,5,5)`)
	if err != nil {
		t.Fatal(err)
	}
}

func stocktakeItemIDs(t *testing.T, stID int64) map[int64]int64 {
	t.Helper()
	rows, err := db.DB.Query(`SELECT id, inventory_item_id FROM stocktake_items WHERE stocktake_id = ?`, stID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	m := map[int64]int64{}
	for rows.Next() {
		var id, inv int64
		if err := rows.Scan(&id, &inv); err != nil {
			t.Fatal(err)
		}
		m[inv] = id
	}
	return m
}

func TestStocktakeFullFlow(t *testing.T) {
	newMigratedTestDB(t)
	seedStocktakeParts(t)
	owner := model.User{ID: 1, Role: "owner"}

	// 建立全庫盤點單（currentUser 無 session → actor 0，不影響流程）。
	rec := httptest.NewRecorder()
	CreateStocktake(rec, stPost("/stocktakes", url.Values{"scope_type": {"all"}, "note": {"月度盤點"}}, nil))
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var stID int64
	var itemCount int
	if err := db.DB.QueryRow(`SELECT id, item_count FROM stocktakes ORDER BY id DESC LIMIT 1`).Scan(&stID, &itemCount); err != nil {
		t.Fatal(err)
	}
	if itemCount != 3 {
		t.Fatalf("item_count=%d want 3", itemCount)
	}
	// 快照含負庫存。
	var snap int
	if err := db.DB.QueryRow(`SELECT system_qty FROM stocktake_items WHERE stocktake_id=? AND inventory_item_id=902`, stID).Scan(&snap); err != nil || snap != -39 {
		t.Fatalf("snapshot=%d err=%v want -39", snap, err)
	}
	ids := stocktakeItemIDs(t, stID)
	sid := strconv.FormatInt(stID, 10)

	// 實盤：A=12（盤盈）、B=5（負庫存歸正）；C 不盤。
	rec = httptest.NewRecorder()
	UpdateStocktakeItem(rec, stPost("/x", url.Values{"counted_qty": {"12"}}, map[string]string{"id": sid, "item_id": strconv.FormatInt(ids[901], 10)}), owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("count A code=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	UpdateStocktakeItem(rec, stPost("/x", url.Values{"counted_qty": {"5"}}, map[string]string{"id": sid, "item_id": strconv.FormatInt(ids[902], 10)}), owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("count B code=%d", rec.Code)
	}
	// 負數實盤被拒。
	rec = httptest.NewRecorder()
	UpdateStocktakeItem(rec, stPost("/x", url.Values{"counted_qty": {"-1"}}, map[string]string{"id": sid, "item_id": strconv.FormatInt(ids[903], 10)}), owner)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative counted code=%d want 400", rec.Code)
	}

	// 完成盤點。
	rec = httptest.NewRecorder()
	CompleteStocktake(rec, stPost("/x", url.Values{}, map[string]string{"id": sid}))
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("complete code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 庫存校正：A 12、B 5、C 未盤保持 5。
	for _, tc := range []struct{ inv, want int64 }{{901, 12}, {902, 5}, {903, 5}} {
		var got int64
		if err := db.DB.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=?`, tc.inv).Scan(&got); err != nil || got != tc.want {
			t.Fatalf("inv %d stock=%d want %d err=%v", tc.inv, got, tc.want, err)
		}
	}
	// 異動：2 筆 stocktake（B 的 delta=44）。
	var movs int
	if err := db.DB.QueryRow(`SELECT COUNT(1) FROM inventory_movements WHERE source='stocktake'`).Scan(&movs); err != nil || movs != 2 {
		t.Fatalf("movements=%d want 2", movs)
	}
	var delta int
	if err := db.DB.QueryRow(`SELECT delta FROM inventory_movements WHERE inventory_item_id=902 AND source='stocktake'`).Scan(&delta); err != nil || delta != 44 {
		t.Fatalf("B delta=%d want 44", delta)
	}
	// 稽核與狀態。
	var audits int
	if err := db.DB.QueryRow(`SELECT COUNT(1) FROM audit_log WHERE action='stocktake_complete'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audits=%d want 1", audits)
	}
	var status string
	db.DB.QueryRow(`SELECT status FROM stocktakes WHERE id=?`, stID).Scan(&status)
	if status != "completed" {
		t.Fatalf("status=%s want completed", status)
	}
	// 已完成不能再 complete。
	rec = httptest.NewRecorder()
	CompleteStocktake(rec, stPost("/x", url.Values{}, map[string]string{"id": sid}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("re-complete code=%d want 400", rec.Code)
	}
}

func TestStocktakeScopeAndCancel(t *testing.T) {
	newMigratedTestDB(t)
	seedStocktakeParts(t)

	// 分類範圍只快照該分類。
	rec := httptest.NewRecorder()
	CreateStocktake(rec, stPost("/stocktakes", url.Values{"scope_type": {"category"}, "scope_value": {"耗材"}}, nil))
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var stID int64
	var n int
	db.DB.QueryRow(`SELECT id, item_count FROM stocktakes ORDER BY id DESC LIMIT 1`).Scan(&stID, &n)
	if n != 2 {
		t.Fatalf("category scope items=%d want 2", n)
	}
	// 空範圍值被拒。
	rec = httptest.NewRecorder()
	CreateStocktake(rec, stPost("/stocktakes", url.Values{"scope_type": {"category"}, "scope_value": {""}}, nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty scope code=%d want 400", rec.Code)
	}
	// 取消不影響庫存。
	rec = httptest.NewRecorder()
	CancelStocktake(rec, stPost("/x", url.Values{}, map[string]string{"id": strconv.FormatInt(stID, 10)}))
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("cancel code=%d", rec.Code)
	}
	var status string
	db.DB.QueryRow(`SELECT status FROM stocktakes WHERE id=?`, stID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("status=%s want cancelled", status)
	}
	var stock int
	db.DB.QueryRow(`SELECT stock_quantity FROM inventory_items WHERE id=902`).Scan(&stock)
	if stock != -39 {
		t.Fatalf("cancel changed stock: %d", stock)
	}
}

func TestStocktakeSearchFragment(t *testing.T) {
	newMigratedTestDB(t)
	seedStocktakeParts(t)
	rec := httptest.NewRecorder()
	CreateStocktake(rec, stPost("/stocktakes", url.Values{"scope_type": {"all"}}, nil))
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("create code=%d", rec.Code)
	}
	owner := model.User{ID: 1, Role: "owner"}
	req := httptest.NewRequest("GET", "/stocktakes/1?q=P-B", nil)
	req.SetPathValue("id", "1")
	req.Header.Set("HX-Request", "true")
	rec = httptest.NewRecorder()
	ShowStocktake(rec, req, owner)
	if rec.Code != http.StatusOK {
		t.Fatalf("search code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "零件B") || strings.Contains(body, "零件A") {
		t.Fatalf("search fragment wrong: %s", body)
	}
}

func TestStocktakeCSVImport(t *testing.T) {
	newMigratedTestDB(t)
	seedStocktakeParts(t)
	owner := model.User{ID: 1, Role: "owner"}

	// 與頁面匯出同格式：P-B 已填實盤 55、P-A 未填（待補）、Z-未知 應略過。
	csvContent := "\ufeff序號,類別,庫位,料號,品名,供應商,帳面數,缺口,實盤數,備註\n" +
		"1,,A1,P-A,零件A,,10,,,\n" +
		"2,,A1,P-B,零件B,,-39,39,55,\n" +
		"3,,X,Z-未知,不存在,,1,,,\n"
	ct, body := multipartCSVBody(t, "csv", "count.csv", []byte(csvContent))
	req := httptest.NewRequest("POST", "/stocktakes/import", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	ImportStocktakeCSV(rec, req, owner)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusFound {
		t.Fatalf("import code=%d body=%s", rec.Code, rec.Body.String())
	}
	var stID int64
	var scopeType, note string
	var itemCount int
	if err := db.DB.QueryRow(`SELECT id, scope_type, COALESCE(note,''), item_count
		FROM stocktakes ORDER BY id DESC LIMIT 1`).Scan(&stID, &scopeType, &note, &itemCount); err != nil {
		t.Fatal(err)
	}
	if scopeType != "csv" || note != "CSV 匯入" || itemCount != 2 {
		t.Fatalf("stocktake=%s/%s/%d want csv/CSV 匯入/2", scopeType, note, itemCount)
	}
	ids := stocktakeItemIDs(t, stID)
	if len(ids) != 2 {
		t.Fatalf("items=%d want 2", len(ids))
	}
	var pbc, pac interface{}
	db.DB.QueryRow(`SELECT counted_qty FROM stocktake_items WHERE stocktake_id=? AND inventory_item_id=902`, stID).Scan(&pbc)
	db.DB.QueryRow(`SELECT counted_qty FROM stocktake_items WHERE stocktake_id=? AND inventory_item_id=901`, stID).Scan(&pac)
	if pbc != int64(55) {
		t.Fatalf("P-B counted=%v want 55", pbc)
	}
	if pac != nil {
		t.Fatalf("P-A counted=%v want NULL", pac)
	}
	var audits int
	db.DB.QueryRow(`SELECT COUNT(1) FROM audit_log WHERE entity_type='stocktake' AND action='stocktake_csv_import'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("audit=%d want 1", audits)
	}

	// 缺「料號」欄 → 400，且不建立任何盤點單。
	bad := "序號,實盤數\r\n1,5\r\n"
	ct2, body2 := multipartCSVBody(t, "csv", "bad.csv", []byte(bad))
	req2 := httptest.NewRequest("POST", "/stocktakes/import", body2)
	req2.Header.Set("Content-Type", ct2)
	rec2 := httptest.NewRecorder()
	ImportStocktakeCSV(rec2, req2, owner)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("missing part header code=%d want 400", rec2.Code)
	}
	var n int
	db.DB.QueryRow(`SELECT COUNT(1) FROM stocktakes`).Scan(&n)
	if n != 1 {
		t.Fatalf("stocktakes=%d want 1 (no extra)", n)
	}
}

func multipartCSVBody(t *testing.T, field, filename string, content []byte) (string, io.Reader) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return w.FormDataContentType(), &b
}