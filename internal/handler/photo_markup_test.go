package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

// setupPhotoMarkupTestDB 建立測試用 DB（最小 schema）。
func setupPhotoMarkupTestDB(t *testing.T) {
	t.Helper()
	orig := db.DB
	t.Cleanup(func() { db.DB = orig })

	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d

	stmts := []string{
		`CREATE TABLE work_orders (id INTEGER PRIMARY KEY, vehicle_id INTEGER, customer_id INTEGER, status TEXT, total_amount REAL DEFAULT 0, updated_at TEXT, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER NOT NULL)`,
		`CREATE TABLE work_order_photos (id INTEGER PRIMARY KEY, work_order_id INTEGER, item_id INTEGER, filename TEXT, original_name TEXT, mime_type TEXT, file_size INTEGER, photo_type TEXT, note TEXT, uploaded_by INTEGER, created_at TEXT, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_work_order_photos AS SELECT * FROM work_order_photos WHERE deleted_at IS NULL`,
		`CREATE TABLE photo_annotations (id INTEGER PRIMARY KEY, photo_id INTEGER, work_order_id INTEGER, annotation_type TEXT, x1 REAL, y1 REAL, x2 REAL, y2 REAL, color TEXT, note TEXT, created_by INTEGER, created_at TEXT, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_photo_annotations AS SELECT * FROM photo_annotations WHERE deleted_at IS NULL`,
		`CREATE TABLE walkaround_marks (id INTEGER PRIMARY KEY, work_order_id INTEGER, vehicle_id INTEGER, mark_type TEXT, x REAL, y REAL, note TEXT, photo_id INTEGER, created_by INTEGER, created_at TEXT, deleted_at TEXT)`,
		`CREATE VIEW IF NOT EXISTS active_walkaround_marks AS SELECT * FROM walkaround_marks WHERE deleted_at IS NULL`,
		`INSERT INTO work_orders (id, vehicle_id, customer_id, status, total_amount) VALUES (10, 7, 1, 'in_progress', 125)`,
		`INSERT INTO work_orders (id, vehicle_id, customer_id, status, total_amount) VALUES (11, 8, 1, 'in_progress', 0)`,
		`INSERT INTO work_order_items (id, work_order_id) VALUES (501, 10), (502, 11)`,
		`INSERT INTO work_order_photos (id, work_order_id, item_id, filename, photo_type) VALUES
			(100, 10, 501, 'test.jpg', 'damage'), (200, 11, 502, 'other.jpg', 'damage')`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("setup: %v\nSQL: %s", err, s)
		}
	}
}

func TestPhotoAnnotationCRUD(t *testing.T) {
	setupPhotoMarkupTestDB(t)
	user := model.User{ID: 1, Role: "counter"}

	// 新增標記
	body := `{"type":"line","x1":10,"y1":20,"x2":50,"y2":60,"color":"#ff0000","note":"測試標記"}`
	r := httptest.NewRequest("POST", "/api/photos/100/annotations", strings.NewReader(body))
	r.SetPathValue("photo_id", "100")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	AddPhotoAnnotation(w, r, user)
	if w.Code != http.StatusOK && w.Code != http.StatusFound {
		t.Fatalf("AddPhotoAnnotation code=%d body=%q", w.Code, w.Body.String())
	}

	// 查詢標記
	r2 := httptest.NewRequest("GET", "/api/photos/100/annotations", nil)
	r2.SetPathValue("photo_id", "100")
	w2 := httptest.NewRecorder()
	ListPhotoAnnotations(w2, r2, user)
	if w2.Code != http.StatusOK {
		t.Fatalf("ListPhotoAnnotations code=%d", w2.Code)
	}
	var annotations []map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &annotations); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(annotations) != 1 {
		t.Fatalf("want 1 annotation, got %d", len(annotations))
	}
	if annotations[0]["annotation_type"] != "line" {
		t.Fatalf("type=%v want line", annotations[0]["annotation_type"])
	}
	if annotations[0]["work_order_id"] != float64(10) {
		t.Fatalf("work_order_id=%v want 10", annotations[0]["work_order_id"])
	}
}

func TestPhotoAnnotationRejectsOtherWorkOrderPhotoAndItem(t *testing.T) {
	setupPhotoMarkupTestDB(t)
	user := model.User{ID: 1, Role: "counter"}

	tests := []struct {
		name string
		body string
	}{
		{name: "other work order photo with expected work order", body: `{"type":"line","work_order_id":10}`},
		{name: "item from another work order", body: `{"type":"line","item_id":501}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/photos/200/annotations", strings.NewReader(tc.body))
			r.SetPathValue("photo_id", "200")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			AddPhotoAnnotation(w, r, user)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("AddPhotoAnnotation code=%d want %d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
			}
			var count int
			if err := db.DB.QueryRow("SELECT COUNT(1) FROM photo_annotations").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("unexpected annotation rows written: %d", count)
			}
		})
	}
}

func TestWalkaroundMarkCRUD(t *testing.T) {
	setupPhotoMarkupTestDB(t)
	user := model.User{ID: 1, Role: "counter"}

	// 新增舊傷標記
	body := `{"mark_type":"scratch","x":150,"y":100,"note":"左前刮痕"}`
	r := httptest.NewRequest("POST", "/work-orders/10/walkaround-marks", strings.NewReader(body))
	r.SetPathValue("id", "10")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	AddWalkaroundMark(w, r, user)
	if w.Code != http.StatusOK && w.Code != http.StatusFound {
		t.Fatalf("AddWalkaroundMark code=%d body=%q", w.Code, w.Body.String())
	}

	// 查詢標記
	r2 := httptest.NewRequest("GET", "/work-orders/10/walkaround-marks", nil)
	r2.SetPathValue("id", "10")
	w2 := httptest.NewRecorder()
	ListWalkaroundMarks(w2, r2, user)
	if w2.Code != http.StatusOK {
		t.Fatalf("ListWalkaroundMarks code=%d", w2.Code)
	}
	var marks []map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &marks); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("want 1 mark, got %d", len(marks))
	}
	if marks[0]["mark_type"] != "scratch" {
		t.Fatalf("mark_type=%v want scratch", marks[0]["mark_type"])
	}
}

func TestPhotoAnnotationDelete(t *testing.T) {
	setupPhotoMarkupTestDB(t)
	user := model.User{ID: 1, Role: "counter"}

	// 新增標記
	body := `{"type":"rect","x1":0,"y1":0,"x2":100,"y2":100}`
	r := httptest.NewRequest("POST", "/api/photos/100/annotations", strings.NewReader(body))
	r.SetPathValue("photo_id", "100")
	w := httptest.NewRecorder()
	AddPhotoAnnotation(w, r, user)

	var id int64
	db.DB.QueryRow("SELECT id FROM photo_annotations WHERE deleted_at IS NULL LIMIT 1").Scan(&id)

	// 刪除標記
	r2 := httptest.NewRequest("POST", "/api/annotations/"+fmt.Sprintf("%d", id)+"/delete", nil)
	r2.SetPathValue("annotation_id", fmtInt(id))
	w2 := httptest.NewRecorder()
	DeletePhotoAnnotation(w2, r2, user)
	if w2.Code != http.StatusOK && w2.Code != http.StatusFound {
		t.Fatalf("DeletePhotoAnnotation code=%d", w2.Code)
	}

	// 確認已刪除
	var count int
	db.DB.QueryRow("SELECT COUNT(1) FROM active_photo_annotations WHERE photo_id = 100").Scan(&count)
	if count != 0 {
		t.Fatalf("want 0 annotations after delete, got %d", count)
	}
}

func TestPhotoAnnotationDeleteMissingReturnsNotFound(t *testing.T) {
	setupPhotoMarkupTestDB(t)
	r := httptest.NewRequest("POST", "/api/annotations/999/delete", nil)
	r.SetPathValue("annotation_id", "999")
	w := httptest.NewRecorder()
	DeletePhotoAnnotation(w, r, model.User{ID: 1, Role: "counter"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("DeletePhotoAnnotation code=%d want %d", w.Code, http.StatusNotFound)
	}
}

func fmtInt(n int64) string {
	return fmt.Sprintf("%d", n)
}
