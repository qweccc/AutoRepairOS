package db

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func TestPhotoUploadQueueMigrationUpDown(t *testing.T) {
	original := DB
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	DB = d
	t.Cleanup(func() {
		DB = original
		_ = d.Close()
	})
	for _, stmt := range []string{
		`CREATE TABLE work_orders (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE work_order_items (id INTEGER PRIMARY KEY)`,
		`CREATE TABLE work_order_photos (id INTEGER PRIMARY KEY)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	up, err := migrationFS.ReadFile("migration/042_photo_upload_queue.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 2; run++ {
		tx, err := d.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := execScript(tx, string(up)); err != nil {
			_ = tx.Rollback()
			t.Fatalf("apply migration (run %d): %v", run+1, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var columns int
	if err := d.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('photo_upload_queue')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns < 15 {
		t.Fatalf("photo_upload_queue has %d columns; want at least 15", columns)
	}
	if _, err := d.Exec(`INSERT INTO work_orders (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO photo_upload_queue
		(work_order_id, file_name, mime_type, stored_name) VALUES (1, 'photo.png', 'image/png', 'one.pending')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO photo_upload_queue
		(work_order_id, file_name, mime_type, stored_name) VALUES (1, 'photo.png', 'image/png', 'two.pending')`); err == nil {
		t.Fatal("duplicate work order and file name was not rejected")
	}

	down, err := os.ReadFile("migration/042_photo_upload_queue.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := execScript(tx, string(down)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='photo_upload_queue'`).
		Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("down migration did not remove the photo upload queue")
	}
}
