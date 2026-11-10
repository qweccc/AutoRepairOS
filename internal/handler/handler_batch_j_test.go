package handler

// handler_batch_j_test.go — 提升 photos.go / photo_markup.go 覆蓋率。
// 只新增此檔，不改 production 或其他測試檔。

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"autorepairos/internal/db"
	"autorepairos/internal/model"
)

var batchJPngMagic = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// batchJMultipartRequest 建立 multipart 上傳請求；filename 為空表示不附檔案欄位。
func batchJMultipartRequest(t *testing.T, filename string, content []byte, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if filename != "" {
		file, err := writer.CreateFormFile("photo", filename)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := file.Write(content); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/work-orders/10/photos", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.SetPathValue("id", "10")
	return r
}

// batchJSeedQueue 插入一筆照片佇列記錄；pending 非 nil 時同時寫入暫存檔。
func batchJSeedQueue(t *testing.T, woID, itemID int64, fileName, mime, storedName string, size int64, status string, pending []byte) int64 {
	t.Helper()
	res, err := db.DB.Exec(`INSERT INTO photo_upload_queue
		(work_order_id, item_id, file_name, mime_type, file_size, stored_name, photo_type, note, status)
		VALUES (?, ?, ?, ?, ?, ?, 'damage', '', ?)`, woID, itemID, fileName, mime, size, storedName, status)
	if err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("queue id: %v", err)
	}
	if pending != nil {
		dir := filepath.Join(photoDir, "pending")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("pending dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, storedName), pending, 0644); err != nil {
			t.Fatalf("pending file: %v", err)
		}
	}
	return id
}

func batchJQueueStatus(t *testing.T, id int64) string {
	t.Helper()
	var status string
	if err := db.DB.QueryRow("SELECT status FROM photo_upload_queue WHERE id = ?", id).Scan(&status); err != nil {
		t.Fatalf("queue status: %v", err)
	}
	return status
}

// batchJDeferredFKDB 建立含遞延外鍵的測試 DB，用來觸發 COMMIT 失敗。
func batchJDeferredFKDB(t *testing.T) {
	t.Helper()
	origDB := db.DB
	origDir := photoDir
	t.Cleanup(func() { db.DB = origDB; photoDir = origDir })

	d, err := openTestDB(t)
	if err != nil {
		t.Fatal(err)
	}
	db.DB = d
	photoDir = filepath.Join(t.TempDir(), "photos")

	statements := []string{
		`CREATE TABLE work_orders (id INTEGER PRIMARY KEY, technician_id INTEGER, deleted_at TEXT)`,
		`CREATE VIEW active_work_orders AS SELECT * FROM work_orders WHERE deleted_at IS NULL`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY, work_order_id INTEGER NOT NULL)`,
		`CREATE TABLE work_order_photos (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER NOT NULL, item_id INTEGER,
			filename TEXT NOT NULL, original_name TEXT, mime_type TEXT, file_size INTEGER,
			photo_type TEXT, note TEXT, uploaded_by INTEGER, created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			deleted_at TEXT)`,
		`CREATE VIEW active_work_order_photos AS SELECT * FROM work_order_photos WHERE deleted_at IS NULL`,
		`CREATE TABLE photo_parent (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE photo_upload_queue (
			id INTEGER PRIMARY KEY AUTOINCREMENT, work_order_id INTEGER NOT NULL, item_id INTEGER,
			file_name TEXT NOT NULL, mime_type TEXT NOT NULL, file_size INTEGER NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending', retry_count INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '', stored_name TEXT NOT NULL,
			photo_type TEXT NOT NULL DEFAULT 'damage', note TEXT NOT NULL DEFAULT '',
			photo_id INTEGER, created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(work_order_id, item_id, file_name),
			FOREIGN KEY (photo_id) REFERENCES photo_parent(id) DEFERRABLE INITIALLY DEFERRED)`,
		`INSERT INTO work_orders (id) VALUES (10)`,
	}
	for _, stmt := range statements {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("setup failed: %v\nSQL: %s", err, stmt)
		}
	}
	// 遞延外鍵只在明確開啟時才檢查。
	if _, err := d.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
}

// batchJErrWriter 是一個寫入必定失敗的 ResponseWriter，用來涵蓋 Encode 錯誤分支。
type batchJErrWriter struct {
	writes int
}

func (w *batchJErrWriter) Header() http.Header { return http.Header{} }
func (w *batchJErrWriter) Write(p []byte) (int, error) {
	w.writes++
	return 0, errors.New("batchJ: write failed")
}
func (w *batchJErrWriter) WriteHeader(int) {}

func batchJJSONRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

// ============================================================
// UploadWorkOrderPhoto
// ============================================================

func TestBatchJUploadWorkOrderPhotoValidation(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) *http.Request
		want  int
	}{
		{
			name: "missing file field",
			build: func(t *testing.T) *http.Request {
				return batchJMultipartRequest(t, "", nil, map[string]string{"photo_type": "repair"})
			},
			want: http.StatusBadRequest,
		},
		{
			name: "empty file",
			build: func(t *testing.T) *http.Request {
				return batchJMultipartRequest(t, "empty.png", nil, map[string]string{"item_id": "501"})
			},
			want: http.StatusBadRequest,
		},
		{
			name: "file over 8MB",
			build: func(t *testing.T) *http.Request {
				big := bytes.Repeat(batchJPngMagic, 2_200_000)
				return batchJMultipartRequest(t, "big.png", big, map[string]string{"item_id": "501"})
			},
			want: http.StatusBadRequest,
		},
		{
			name: "unsupported mime",
			build: func(t *testing.T) *http.Request {
				return batchJMultipartRequest(t, "note.txt", []byte("hello world"), map[string]string{"item_id": "501"})
			},
			want: http.StatusBadRequest,
		},
		{
			name: "invalid item id",
			build: func(t *testing.T) *http.Request {
				return batchJMultipartRequest(t, "a.png", batchJPngMagic, map[string]string{"item_id": "abc"})
			},
			want: http.StatusBadRequest,
		},
		{
			name: "negative item id",
			build: func(t *testing.T) *http.Request {
				return batchJMultipartRequest(t, "a.png", batchJPngMagic, map[string]string{"item_id": "-4"})
			},
			want: http.StatusBadRequest,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupPhotoUploadTestDB(t)
			w := httptest.NewRecorder()
			UploadWorkOrderPhoto(w, tc.build(t))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%q", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestBatchJUploadWorkOrderPhotoDefaults(t *testing.T) {
	setupPhotoUploadTestDB(t)
	r := batchJMultipartRequest(t, ".", batchJPngMagic, map[string]string{"photo_type": "bogus", "item_id": "501"})
	w := httptest.NewRecorder()
	UploadWorkOrderPhoto(w, r)
	if w.Code != http.StatusFound {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	var originalName, photoType string
	if err := db.DB.QueryRow("SELECT original_name, photo_type FROM work_order_photos").Scan(&originalName, &photoType); err != nil {
		t.Fatalf("query photo: %v", err)
	}
	if originalName != "photo.png" {
		t.Fatalf("original_name=%q want photo.png", originalName)
	}
	if photoType != "damage" {
		t.Fatalf("photo_type=%q want damage", photoType)
	}
}

func TestBatchJUploadWorkOrderPhotoDBErrors(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T)
		want    int
	}{
		{
			name: "item validation query error",
			prepare: func(t *testing.T) {
				mustExec(t, "DROP TABLE work_order_items")
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "queue lookup error",
			prepare: func(t *testing.T) {
				mustExec(t, "DROP TABLE photo_upload_queue")
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "duplicate photo lookup error",
			prepare: func(t *testing.T) {
				mustExec(t, "DROP TABLE work_order_photos")
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "duplicate photo conflict",
			prepare: func(t *testing.T) {
				mustExec(t, `INSERT INTO work_order_photos
					(work_order_id, item_id, filename, original_name, mime_type, file_size, photo_type)
					VALUES (10, 501, 'wo10_dup.png', 'dup.png', 'image/png', 8, 'damage')`)
			},
			want: http.StatusConflict,
		},
		{
			name: "pending dir not creatable",
			prepare: func(t *testing.T) {
				blocker := filepath.Join(t.TempDir(), "blocker")
				if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
				photoDir = blocker
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "queue insert unique violation",
			prepare: func(t *testing.T) {
				mustExec(t, `CREATE TRIGGER batchj_unique_insert BEFORE INSERT ON photo_upload_queue
					BEGIN SELECT RAISE(ABORT, 'UNIQUE constraint failed: photo_upload_queue'); END`)
			},
			want: http.StatusConflict,
		},
		{
			name: "queue insert generic error",
			prepare: func(t *testing.T) {
				mustExec(t, `CREATE TRIGGER batchj_generic_insert BEFORE INSERT ON photo_upload_queue
					BEGIN SELECT RAISE(ABORT, 'disk I/O error'); END`)
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "closed database",
			prepare: func(t *testing.T) {
				_ = db.DB.Close()
			},
			want: http.StatusNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupPhotoUploadTestDB(t)
			tc.prepare(t)
			w := httptest.NewRecorder()
			UploadWorkOrderPhoto(w, batchJMultipartRequest(t, "dup.png", batchJPngMagic, map[string]string{"item_id": "501"}))
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%q", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestBatchJUploadWorkOrderPhotoExistingQueueRow(t *testing.T) {
	t.Run("pending row that fails processing", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		mustExec(t, `INSERT INTO photo_upload_queue
			(work_order_id, item_id, file_name, mime_type, file_size, stored_name, photo_type, note, status)
			VALUES (10, 501, 'queue.png', 'image/png', 8, 'missing.png', 'damage', '', 'pending')`)
		w := httptest.NewRecorder()
		UploadWorkOrderPhoto(w, batchJMultipartRequest(t, "queue.png", batchJPngMagic, map[string]string{"item_id": "501"}))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusServiceUnavailable, w.Body.String())
		}
	})

	t.Run("pending row that processes", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		dir := filepath.Join(photoDir, "pending")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ok.png"), batchJPngMagic, 0644); err != nil {
			t.Fatal(err)
		}
		mustExec(t, `INSERT INTO photo_upload_queue
			(work_order_id, item_id, file_name, mime_type, file_size, stored_name, photo_type, note, status)
			VALUES (10, 501, 'queue.png', 'image/png', 8, 'ok.png', 'damage', '', 'pending')`)
		w := httptest.NewRecorder()
		UploadWorkOrderPhoto(w, batchJMultipartRequest(t, "queue.png", batchJPngMagic, map[string]string{"item_id": "501"}))
		if w.Code != http.StatusFound {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusFound, w.Body.String())
		}
		var status string
		if err := db.DB.QueryRow("SELECT status FROM photo_upload_queue WHERE file_name = 'queue.png'").Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "done" {
			t.Fatalf("queue status=%q want done", status)
		}
	})
}

// ============================================================
// processPhotoUpload
// ============================================================

func TestBatchJProcessPhotoUploadBranches(t *testing.T) {
	t.Run("queue row missing", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		if err := processPhotoUpload(99999); err == nil {
			t.Fatal("want error for missing queue row")
		}
	})

	t.Run("claim update error", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		mustExec(t, `CREATE TRIGGER batchj_block_claim BEFORE UPDATE ON photo_upload_queue
			BEGIN SELECT RAISE(ABORT, 'claim failed'); END`)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "pending", batchJPngMagic)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error when claim update fails")
		}
	})

	t.Run("status uploading is rejected", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "uploading", batchJPngMagic)
		err := processPhotoUpload(id)
		if err == nil || !strings.Contains(err.Error(), "uploading") {
			t.Fatalf("err=%v want uploading status error", err)
		}
	})

	t.Run("status done short-circuits", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "done", batchJPngMagic)
		if err := processPhotoUpload(id); err != nil {
			t.Fatalf("err=%v want nil", err)
		}
	})

	t.Run("status lookup error", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "pending", batchJPngMagic)
		mustExec(t, `CREATE TRIGGER batchj_delete_on_claim BEFORE UPDATE ON photo_upload_queue
			BEGIN DELETE FROM photo_upload_queue WHERE id = OLD.id; END`)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error when status lookup fails")
		}
	})

	t.Run("pending file missing", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "missing.png", 8, "pending", nil)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error for missing pending file")
		}
		if got := batchJQueueStatus(t, id); got != "failed" {
			t.Fatalf("status=%q want failed", got)
		}
	})

	t.Run("failure status update also fails", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "missing.png", 8, "pending", nil)
		mustExec(t, `CREATE TRIGGER batchj_block_failed BEFORE UPDATE ON photo_upload_queue
			WHEN NEW.status = 'failed' BEGIN SELECT RAISE(ABORT, 'failed update blocked'); END`)
		err := processPhotoUpload(id)
		if err == nil || !strings.Contains(err.Error(), "更新失敗狀態也失敗") {
			t.Fatalf("err=%v want wrapped failure-update error", err)
		}
	})

	t.Run("pending size mismatch", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 999, "pending", batchJPngMagic)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error for size mismatch")
		}
	})

	t.Run("unsupported mime", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		id := batchJSeedQueue(t, 10, 501, "a.png", "application/pdf", "a.png", 8, "pending", batchJPngMagic)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error for unsupported mime")
		}
	})

	t.Run("existing photo is linked", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		mustExec(t, `INSERT INTO work_order_photos
			(work_order_id, item_id, filename, original_name, mime_type, file_size, photo_type)
			VALUES (10, 501, 'wo10_old.png', 'a.png', 'image/png', 8, 'damage')`)
		var photoID int64
		if err := db.DB.QueryRow("SELECT id FROM work_order_photos WHERE original_name = 'a.png'").Scan(&photoID); err != nil {
			t.Fatal(err)
		}
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "pending", batchJPngMagic)
		if err := processPhotoUpload(id); err != nil {
			t.Fatalf("err=%v want nil", err)
		}
		if got := batchJQueueStatus(t, id); got != "done" {
			t.Fatalf("status=%q want done", got)
		}
		var linked int64
		if err := db.DB.QueryRow("SELECT photo_id FROM photo_upload_queue WHERE id = ?", id).Scan(&linked); err != nil {
			t.Fatal(err)
		}
		if linked != photoID {
			t.Fatalf("photo_id=%d want %d", linked, photoID)
		}
	})

	t.Run("existing photo link update fails", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		mustExec(t, `INSERT INTO work_order_photos
			(work_order_id, item_id, filename, original_name, mime_type, file_size, photo_type)
			VALUES (10, 501, 'wo10_old.png', 'a.png', 'image/png', 8, 'damage')`)
		mustExec(t, `CREATE TRIGGER batchj_block_done BEFORE UPDATE ON photo_upload_queue
			WHEN NEW.status = 'done' BEGIN SELECT RAISE(ABORT, 'done update blocked'); END`)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "pending", batchJPngMagic)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error when linking existing photo fails")
		}
	})

	t.Run("existing photo lookup error", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		mustExec(t, `ALTER TABLE work_order_photos RENAME TO work_order_photos_base`)
		mustExec(t, `CREATE VIEW work_order_photos AS SELECT * FROM missing_table`)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "pending", batchJPngMagic)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error when existing photo lookup fails")
		}
	})

	t.Run("final file path is a directory", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "pending", batchJPngMagic)
		finalPath := filepath.Join(photoDir, fmt.Sprintf("wo%d_q%d%s", 10, id, ".png"))
		if err := os.MkdirAll(finalPath, 0755); err != nil {
			t.Fatal(err)
		}
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error when final path is a directory")
		}
	})

	t.Run("queue done update fails", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		mustExec(t, `CREATE TRIGGER batchj_block_done2 BEFORE UPDATE ON photo_upload_queue
			WHEN NEW.status = 'done' BEGIN SELECT RAISE(ABORT, 'done update blocked'); END`)
		id := batchJSeedQueue(t, 10, 501, "a.png", "image/png", "a.png", 8, "pending", batchJPngMagic)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want error when queue done update fails")
		}
	})

	t.Run("closed database", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		_ = db.DB.Close()
		if err := processPhotoUpload(1); err == nil {
			t.Fatal("want error on closed database")
		}
	})
}

func TestBatchJProcessPhotoUploadCommitErrors(t *testing.T) {
	seedOrphan := func(t *testing.T) int64 {
		res, err := db.DB.Exec(`INSERT INTO photo_upload_queue
			(work_order_id, item_id, file_name, mime_type, file_size, stored_name, status)
			VALUES (999, 501, 'a.png', 'image/png', 8, 'a.png', 'pending')`)
		if err != nil {
			t.Fatalf("seed orphan: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("orphan id: %v", err)
		}
		dir := filepath.Join(photoDir, "pending")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.png"), batchJPngMagic, 0644); err != nil {
			t.Fatal(err)
		}
		return id
	}

	t.Run("main path commit fails", func(t *testing.T) {
		batchJDeferredFKDB(t)
		id := seedOrphan(t)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want commit error on main path")
		}
	})

	t.Run("existing photo path commit fails", func(t *testing.T) {
		batchJDeferredFKDB(t)
		mustExec(t, `INSERT INTO work_order_photos
			(work_order_id, item_id, filename, original_name, mime_type, file_size, photo_type)
			VALUES (999, 501, 'wo999_old.png', 'a.png', 'image/png', 8, 'damage')`)
		id := seedOrphan(t)
		if err := processPhotoUpload(id); err == nil {
			t.Fatal("want commit error on existing photo path")
		}
	})
}

// ============================================================
// DeleteWorkOrderPhoto
// ============================================================

func TestBatchJDeleteWorkOrderPhoto(t *testing.T) {
	seedPhoto := func(t *testing.T) {
		mustExec(t, `INSERT INTO work_order_photos
			(id, work_order_id, item_id, filename, original_name, mime_type, file_size, photo_type)
			VALUES (55, 10, 501, 'wo10_55.png', 'a.png', 'image/png', 8, 'damage')`)
	}
	deleteReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/work-orders/10/photos/55/delete", nil)
		r.SetPathValue("id", "10")
		r.SetPathValue("photo_id", "55")
		return r
	}

	t.Run("success", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		seedPhoto(t)
		w := httptest.NewRecorder()
		DeleteWorkOrderPhoto(w, deleteReq())
		if w.Code != http.StatusFound {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusFound, w.Body.String())
		}
		var deleted sql.NullString
		if err := db.DB.QueryRow("SELECT deleted_at FROM work_order_photos WHERE id = 55").Scan(&deleted); err != nil {
			t.Fatal(err)
		}
		if !deleted.Valid || strings.TrimSpace(deleted.String) == "" {
			t.Fatalf("deleted_at=%v want non-empty", deleted)
		}
	})

	t.Run("update error", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		seedPhoto(t)
		mustExec(t, `CREATE TRIGGER batchj_block_photo_update BEFORE UPDATE ON work_order_photos
			BEGIN SELECT RAISE(ABORT, 'update blocked'); END`)
		w := httptest.NewRecorder()
		DeleteWorkOrderPhoto(w, deleteReq())
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("rows affected zero", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		seedPhoto(t)
		mustExec(t, `CREATE TRIGGER batchj_skip_photo_delete BEFORE UPDATE ON work_order_photos
			BEGIN SELECT RAISE(IGNORE); END`)
		w := httptest.NewRecorder()
		DeleteWorkOrderPhoto(w, deleteReq())
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusNotFound, w.Body.String())
		}
	})

	t.Run("closed database", func(t *testing.T) {
		setupPhotoUploadTestDB(t)
		_ = db.DB.Close()
		w := httptest.NewRecorder()
		DeleteWorkOrderPhoto(w, deleteReq())
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusNotFound, w.Body.String())
		}
	})
}

// ============================================================
// photo_markup.go
// ============================================================

func TestBatchJAddPhotoAnnotation(t *testing.T) {
	user := model.User{ID: 1, Role: "counter"}

	t.Run("invalid json", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/api/photos/100/annotations", "not-json")
		r.SetPathValue("photo_id", "100")
		w := httptest.NewRecorder()
		AddPhotoAnnotation(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("missing type renders form error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/api/photos/100/annotations", `{"x1":1}`)
		r.SetPathValue("photo_id", "100")
		w := httptest.NewRecorder()
		AddPhotoAnnotation(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
		}
		if !strings.Contains(w.Body.String(), "標記類型為必填") {
			t.Fatalf("body=%q want message", w.Body.String())
		}
	})

	t.Run("unknown photo is 404", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/api/photos/999/annotations", `{"type":"line"}`)
		r.SetPathValue("photo_id", "999")
		w := httptest.NewRecorder()
		AddPhotoAnnotation(w, r, user)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("photo lookup error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE work_order_photos")
		r := batchJJSONRequest(t, http.MethodPost, "/api/photos/100/annotations", `{"type":"line"}`)
		r.SetPathValue("photo_id", "100")
		w := httptest.NewRecorder()
		AddPhotoAnnotation(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("non-positive item id", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/api/photos/100/annotations", `{"type":"line","item_id":0}`)
		r.SetPathValue("photo_id", "100")
		w := httptest.NewRecorder()
		AddPhotoAnnotation(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("item lookup error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE work_order_items")
		r := batchJJSONRequest(t, http.MethodPost, "/api/photos/100/annotations", `{"type":"line","item_id":501}`)
		r.SetPathValue("photo_id", "100")
		w := httptest.NewRecorder()
		AddPhotoAnnotation(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("insert error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE photo_annotations")
		r := batchJJSONRequest(t, http.MethodPost, "/api/photos/100/annotations", `{"type":"line"}`)
		r.SetPathValue("photo_id", "100")
		w := httptest.NewRecorder()
		AddPhotoAnnotation(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("happy path with default color", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/api/photos/100/annotations",
			`{"type":"rect","x1":1,"y1":2,"x2":3,"y2":4,"note":"n"}`)
		r.SetPathValue("photo_id", "100")
		w := httptest.NewRecorder()
		AddPhotoAnnotation(w, r, user)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusOK, w.Body.String())
		}
		var color, note string
		var createdBy int64
		if err := db.DB.QueryRow("SELECT color, note, created_by FROM photo_annotations ORDER BY id DESC LIMIT 1").
			Scan(&color, &note, &createdBy); err != nil {
			t.Fatal(err)
		}
		if color != "#ff0000" {
			t.Fatalf("color=%q want #ff0000", color)
		}
		if note != "n" || createdBy != 1 {
			t.Fatalf("note=%q created_by=%d", note, createdBy)
		}
	})
}

func TestBatchJListPhotoAnnotations(t *testing.T) {
	user := model.User{ID: 1, Role: "counter"}
	listReq := func(photoID string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/photos/"+photoID+"/annotations", nil)
		r.SetPathValue("photo_id", photoID)
		return r
	}

	t.Run("unknown photo is 404", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		w := httptest.NewRecorder()
		ListPhotoAnnotations(w, listReq("999"), user)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("photo lookup error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE work_order_photos")
		w := httptest.NewRecorder()
		ListPhotoAnnotations(w, listReq("100"), user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("query error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE photo_annotations")
		w := httptest.NewRecorder()
		ListPhotoAnnotations(w, listReq("100"), user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, `INSERT INTO photo_annotations
			(photo_id, work_order_id, annotation_type, x1, y1, x2, y2, color, note, created_by)
			VALUES (100, 10, 'line', 'abc', 0, 0, 0, '#fff', '', 1)`)
		w := httptest.NewRecorder()
		ListPhotoAnnotations(w, listReq("100"), user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("encode error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, `INSERT INTO photo_annotations
			(photo_id, work_order_id, annotation_type, x1, y1, x2, y2, color, note, created_by)
			VALUES (100, 10, 'line', 1, 2, 3, 4, '#fff', '', 1)`)
		bw := &batchJErrWriter{}
		ListPhotoAnnotations(bw, listReq("100"), user)
		if bw.writes == 0 {
			t.Fatal("want encoder to attempt a write")
		}
	})

	t.Run("happy path returns rows", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, `INSERT INTO photo_annotations
			(photo_id, work_order_id, annotation_type, x1, y1, x2, y2, color, note, created_by)
			VALUES (100, 10, 'line', 1, 2, 3, 4, '#fff', 'n', 1)`)
		w := httptest.NewRecorder()
		ListPhotoAnnotations(w, listReq("100"), user)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusOK, w.Body.String())
		}
		var out []model.PhotoAnnotation
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(out) != 1 || out[0].AnnotationType != "line" {
			t.Fatalf("out=%+v", out)
		}
	})
}

func TestBatchJDeletePhotoAnnotation(t *testing.T) {
	user := model.User{ID: 1, Role: "counter"}

	t.Run("exec error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE photo_annotations")
		r := httptest.NewRequest(http.MethodPost, "/api/annotations/1/delete", nil)
		r.SetPathValue("annotation_id", "1")
		w := httptest.NewRecorder()
		DeletePhotoAnnotation(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("success", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, `INSERT INTO photo_annotations
			(photo_id, work_order_id, annotation_type, x1, y1, x2, y2, color, note, created_by)
			VALUES (100, 10, 'line', 1, 2, 3, 4, '#fff', '', 1)`)
		var id int64
		if err := db.DB.QueryRow("SELECT id FROM photo_annotations LIMIT 1").Scan(&id); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/annotations/"+fmtInt(id)+"/delete", nil)
		r.SetPathValue("annotation_id", fmtInt(id))
		w := httptest.NewRecorder()
		DeletePhotoAnnotation(w, r, user)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusOK, w.Body.String())
		}
		var count int
		if err := db.DB.QueryRow("SELECT COUNT(1) FROM active_photo_annotations").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("active annotations=%d want 0", count)
		}
	})
}

func TestBatchJAddWalkaroundMark(t *testing.T) {
	user := model.User{ID: 1, Role: "counter"}

	t.Run("invalid json", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/work-orders/10/walkaround-marks", "not-json")
		r.SetPathValue("id", "10")
		w := httptest.NewRecorder()
		AddWalkaroundMark(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("missing mark type renders form error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/work-orders/10/walkaround-marks", `{"x":1}`)
		r.SetPathValue("id", "10")
		w := httptest.NewRecorder()
		AddWalkaroundMark(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
		}
		if !strings.Contains(w.Body.String(), "舊傷類型為必填") {
			t.Fatalf("body=%q want message", w.Body.String())
		}
	})

	t.Run("unknown work order is 404", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/work-orders/999/walkaround-marks", `{"mark_type":"dent"}`)
		r.SetPathValue("id", "999")
		w := httptest.NewRecorder()
		AddWalkaroundMark(w, r, user)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("work order lookup error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE work_orders")
		r := batchJJSONRequest(t, http.MethodPost, "/work-orders/10/walkaround-marks", `{"mark_type":"dent"}`)
		r.SetPathValue("id", "10")
		w := httptest.NewRecorder()
		AddWalkaroundMark(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("insert error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE walkaround_marks")
		r := batchJJSONRequest(t, http.MethodPost, "/work-orders/10/walkaround-marks", `{"mark_type":"dent"}`)
		r.SetPathValue("id", "10")
		w := httptest.NewRecorder()
		AddWalkaroundMark(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("happy path", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		r := batchJJSONRequest(t, http.MethodPost, "/work-orders/10/walkaround-marks",
			`{"mark_type":"scratch","x":10,"y":20,"note":"刮痕","photo_id":100}`)
		r.SetPathValue("id", "10")
		w := httptest.NewRecorder()
		AddWalkaroundMark(w, r, user)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusOK, w.Body.String())
		}
		var markType, note string
		var vehicleID, photoID, createdBy int64
		if err := db.DB.QueryRow("SELECT mark_type, vehicle_id, note, photo_id, created_by FROM walkaround_marks ORDER BY id DESC LIMIT 1").
			Scan(&markType, &vehicleID, &note, &photoID, &createdBy); err != nil {
			t.Fatal(err)
		}
		if markType != "scratch" || vehicleID != 7 || note != "刮痕" || photoID != 100 || createdBy != 1 {
			t.Fatalf("row=%q veh=%d note=%q photo=%d by=%d", markType, vehicleID, note, photoID, createdBy)
		}
	})
}

func TestBatchJListWalkaroundMarks(t *testing.T) {
	user := model.User{ID: 1, Role: "counter"}
	listReq := func(woID string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/work-orders/"+woID+"/walkaround-marks", nil)
		r.SetPathValue("id", woID)
		return r
	}

	t.Run("unknown work order is 404", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		w := httptest.NewRecorder()
		ListWalkaroundMarks(w, listReq("999"), user)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusNotFound)
		}
	})

	t.Run("work order lookup error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE work_orders")
		w := httptest.NewRecorder()
		ListWalkaroundMarks(w, listReq("10"), user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("query error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE walkaround_marks")
		w := httptest.NewRecorder()
		ListWalkaroundMarks(w, listReq("10"), user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("scan error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, `INSERT INTO walkaround_marks
			(work_order_id, vehicle_id, mark_type, x, y, note, photo_id, created_by)
			VALUES (10, 7, 'dent', 'abc', 0, '', NULL, 1)`)
		w := httptest.NewRecorder()
		ListWalkaroundMarks(w, listReq("10"), user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("encode error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, `INSERT INTO walkaround_marks
			(work_order_id, vehicle_id, mark_type, x, y, note, photo_id, created_by)
			VALUES (10, 7, 'dent', 1, 2, '', NULL, 1)`)
		bw := &batchJErrWriter{}
		ListWalkaroundMarks(bw, listReq("10"), user)
		if bw.writes == 0 {
			t.Fatal("want encoder to attempt a write")
		}
	})

	t.Run("happy path returns rows", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, `INSERT INTO walkaround_marks
			(work_order_id, vehicle_id, mark_type, x, y, note, photo_id, created_by)
			VALUES (10, 7, 'dent', 1, 2, '刮痕', 100, 1)`)
		w := httptest.NewRecorder()
		ListWalkaroundMarks(w, listReq("10"), user)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusOK, w.Body.String())
		}
		var out []model.WalkaroundMark
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(out) != 1 || out[0].MarkType != "dent" {
			t.Fatalf("out=%+v", out)
		}
	})
}

func TestBatchJDeleteWalkaroundMark(t *testing.T) {
	user := model.User{ID: 1, Role: "counter"}

	t.Run("exec error", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, "DROP TABLE walkaround_marks")
		r := httptest.NewRequest(http.MethodPost, "/work-orders/10/walkaround-marks/1/delete", nil)
		r.SetPathValue("id", "10")
		r.SetPathValue("mark_id", "1")
		w := httptest.NewRecorder()
		DeleteWalkaroundMark(w, r, user)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusBadRequest, w.Body.String())
		}
	})

	t.Run("success", func(t *testing.T) {
		setupPhotoMarkupTestDB(t)
		mustExec(t, `INSERT INTO walkaround_marks
			(work_order_id, vehicle_id, mark_type, x, y, note, photo_id, created_by)
			VALUES (10, 7, 'dent', 1, 2, '', NULL, 1)`)
		var id int64
		if err := db.DB.QueryRow("SELECT id FROM walkaround_marks LIMIT 1").Scan(&id); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/work-orders/10/walkaround-marks/"+fmtInt(id)+"/delete", nil)
		r.SetPathValue("id", "10")
		r.SetPathValue("mark_id", fmtInt(id))
		w := httptest.NewRecorder()
		DeleteWalkaroundMark(w, r, user)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusOK, w.Body.String())
		}
		var count int
		if err := db.DB.QueryRow("SELECT COUNT(1) FROM active_walkaround_marks").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("active marks=%d want 0", count)
		}
	})
}

// ============================================================
// 路由層：權限（403）與 HTTP method（405）
// ============================================================

func batchJRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Post("/work-orders/{id}/photos", CounterUp(UploadWorkOrderPhoto))
	r.Post("/work-orders/{id}/photos/{photo_id}/delete", CounterUp(DeleteWorkOrderPhoto))
	r.Post("/work-orders/{id}/walkaround-marks", AllStaff(AddWalkaroundMark))
	r.Get("/work-orders/{id}/walkaround-marks", AllStaff(ListWalkaroundMarks))
	r.Post("/work-orders/{id}/walkaround-marks/{mark_id}/delete", AllStaff(DeleteWalkaroundMark))
	r.Post("/api/photos/{photo_id}/annotations", AllStaff(AddPhotoAnnotation))
	r.Get("/api/photos/{photo_id}/annotations", AllStaff(ListPhotoAnnotations))
	r.Post("/api/annotations/{annotation_id}/delete", AllStaff(DeletePhotoAnnotation))
	return r
}

func TestBatchJRouterPermissionAndMethod(t *testing.T) {
	setupPhotoMarkupTestDB(t)
	mustExec(t, `CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, role TEXT, name TEXT, active INTEGER)`)
	mustExec(t, `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id INTEGER, csrf_token TEXT, created_at TEXT)`)
	mustExec(t, `INSERT INTO users (id, username, role, name, active) VALUES
		(1, 'owner', 'owner', '老闆', 1),
		(2, 'counter', 'counter', '櫃檯', 1),
		(3, 'tech', 'technician', '技師', 1)`)

	techToken, _ := startTestSession(t, 3)
	counterToken, _ := startTestSession(t, 2)

	do := func(method, path, token, contentType, body string) *httptest.ResponseRecorder {
		var reader *strings.Reader
		if body == "" {
			reader = strings.NewReader("")
		} else {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, path, reader)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
		}
		w := httptest.NewRecorder()
		batchJRouter().ServeHTTP(w, req)
		return w
	}

	t.Run("technician forbidden on counter-only route", func(t *testing.T) {
		w := do(http.MethodPost, "/work-orders/10/photos", techToken, "application/octet-stream", "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusForbidden, w.Body.String())
		}
	})

	t.Run("wrong method is 405", func(t *testing.T) {
		w := do(http.MethodGet, "/work-orders/10/photos", counterToken, "", "")
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusMethodNotAllowed, w.Body.String())
		}
	})

	t.Run("unauthenticated redirects to login", func(t *testing.T) {
		w := do(http.MethodPost, "/work-orders/10/photos", "", "application/octet-stream", "")
		if w.Code != http.StatusFound {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusFound, w.Body.String())
		}
	})

	t.Run("all staff role reaches handler", func(t *testing.T) {
		w := do(http.MethodPost, "/api/photos/100/annotations", techToken, "application/json", `{"type":"line"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%q", w.Code, http.StatusOK, w.Body.String())
		}
	})
}
