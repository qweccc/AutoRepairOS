package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autorepairos/internal/db"
)

func setupPhotoUploadTestDB(t *testing.T) {
	t.Helper()
	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	originalPhotoDir := photoDir
	photoDir = filepath.Join(t.TempDir(), "photos")
	t.Cleanup(func() { photoDir = originalPhotoDir })

	statements := []string{
		`CREATE TABLE work_orders (id INTEGER PRIMARY KEY, deleted_at TEXT)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER NOT NULL)`,
		`CREATE TABLE work_order_photos (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER NOT NULL, item_id INTEGER,
			filename TEXT NOT NULL, original_name TEXT, mime_type TEXT, file_size INTEGER,
			photo_type TEXT, note TEXT, uploaded_by INTEGER, created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			deleted_at TEXT)`,
		`CREATE VIEW active_work_order_photos AS SELECT * FROM work_order_photos WHERE deleted_at IS NULL`,
		`CREATE TABLE photo_upload_queue (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER NOT NULL, item_id INTEGER,
			file_name TEXT NOT NULL, mime_type TEXT NOT NULL, file_size INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending', retry_count INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '', stored_name TEXT NOT NULL,
			photo_type TEXT NOT NULL DEFAULT 'damage', note TEXT NOT NULL DEFAULT '',
			photo_id INTEGER, created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(work_order_id, item_id, file_name))`,
		`INSERT INTO work_orders (id) VALUES (10)`,
		`INSERT INTO work_order_items (id, work_order_id) VALUES (501, 10)`,
	}
	for _, stmt := range statements {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("setup failed: %v\nSQL: %s", err, stmt)
		}
	}
}

func photoUploadRequest(t *testing.T, filename string, itemID string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if itemID != "" {
		if err := writer.WriteField("item_id", itemID); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.WriteField("photo_type", "repair"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("note", "維修紀錄"); err != nil {
		t.Fatal(err)
	}
	file, err := writer.CreateFormFile("photo", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/work-orders/10/photos", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.SetPathValue("id", "10")
	return r
}

func TestUploadWorkOrderPhotoSuccessAndDeduplicates(t *testing.T) {
	setupPhotoUploadTestDB(t)

	first := httptest.NewRecorder()
	UploadWorkOrderPhoto(first, photoUploadRequest(t, "repair.png", "501"))
	if first.Code != http.StatusFound {
		t.Fatalf("first upload status=%d body=%q", first.Code, first.Body.String())
	}
	var photos, done int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM work_order_photos").Scan(&photos); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM photo_upload_queue WHERE status='done'").Scan(&done); err != nil {
		t.Fatal(err)
	}
	if photos != 1 || done != 1 {
		t.Fatalf("photos=%d done=%d; want one of each", photos, done)
	}
	var filename string
	if err := db.DB.QueryRow("SELECT filename FROM work_order_photos").Scan(&filename); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(photoDir, filename)); err != nil {
		t.Fatalf("uploaded photo missing on disk: %v", err)
	}

	duplicate := httptest.NewRecorder()
	UploadWorkOrderPhoto(duplicate, photoUploadRequest(t, "repair.png", "501"))
	if duplicate.Code != http.StatusFound {
		t.Fatalf("duplicate status=%d body=%q", duplicate.Code, duplicate.Body.String())
	}
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM work_order_photos").Scan(&photos); err != nil {
		t.Fatal(err)
	}
	if photos != 1 {
		t.Fatalf("duplicate upload created another photo: count=%d", photos)
	}
}

func TestUploadWorkOrderPhotoFailureRetriesSuccessfully(t *testing.T) {
	setupPhotoUploadTestDB(t)
	if _, err := db.DB.Exec(`CREATE TRIGGER reject_photo_insert BEFORE INSERT ON work_order_photos
		BEGIN SELECT RAISE(ABORT, 'temporary database fault'); END`); err != nil {
		t.Fatal(err)
	}

	first := httptest.NewRecorder()
	UploadWorkOrderPhoto(first, photoUploadRequest(t, "retry.png", "501"))
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed upload status=%d body=%q", first.Code, first.Body.String())
	}
	var status, storedName string
	var retries int
	if err := db.DB.QueryRow("SELECT status, retry_count, stored_name FROM photo_upload_queue").Scan(&status, &retries, &storedName); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || retries != 1 {
		t.Fatalf("queue status=%q retries=%d; want failed and 1", status, retries)
	}
	if _, err := os.Stat(filepath.Join(photoDir, "pending", storedName)); err != nil {
		t.Fatalf("pending photo blob missing: %v", err)
	}
	if _, err := db.DB.Exec("DROP TRIGGER reject_photo_insert"); err != nil {
		t.Fatal(err)
	}

	retry := httptest.NewRequest(http.MethodPost, "/work-orders/10/photos?retry=1", nil)
	retry.SetPathValue("id", "10")
	retried := httptest.NewRecorder()
	UploadWorkOrderPhoto(retried, retry)
	if retried.Code != http.StatusOK || !strings.Contains(retried.Body.String(), `"processed":1`) {
		t.Fatalf("retry status=%d body=%q", retried.Code, retried.Body.String())
	}
	if err := db.DB.QueryRow("SELECT status FROM photo_upload_queue").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "done" {
		t.Fatalf("queue status=%q; want done", status)
	}
	var photos int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM work_order_photos").Scan(&photos); err != nil {
		t.Fatal(err)
	}
	if photos != 1 {
		t.Fatalf("retry produced %d photos; want 1", photos)
	}
}

func TestUploadWorkOrderPhotoRejectsUnknownWorkOrderOrItem(t *testing.T) {
	setupPhotoUploadTestDB(t)
	tests := []struct {
		name string
		id   string
		item string
		want int
	}{
		{name: "unknown work order", id: "999", item: "501", want: http.StatusNotFound},
		{name: "item belongs to another order", id: "10", item: "999", want: http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := photoUploadRequest(t, "invalid.png", tc.item)
			r.SetPathValue("id", tc.id)
			w := httptest.NewRecorder()
			UploadWorkOrderPhoto(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%q", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
