package cron

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"autorepairos/internal/db"
	_ "modernc.org/sqlite"
)

func backupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	originalDB := db.DB
	originalCfg := backupCfg
	t.Setenv("BACKUP_DIR", "")
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	db.DB = d
	if _, err := d.Exec(`CREATE TABLE backup_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT, backup_date TEXT, db_backup_path TEXT,
		db_size INTEGER, photo_count INTEGER, photo_size INTEGER, status TEXT, error_message TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.DB = originalDB
		backupCfg = originalCfg
		_ = d.Close()
	})
	return d
}

func TestDailyBackupIncludesPhotosAndExpiresAfterThirtyDays(t *testing.T) {
	d := backupTestDB(t)
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	photoDir := filepath.Join(root, "photos")
	if err := os.MkdirAll(filepath.Join(photoDir, "pending"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photoDir, "repair.png"), []byte("photo-data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(photoDir, "pending", "queued.bin"), []byte("queued-photo"), 0644); err != nil {
		t.Fatal(err)
	}
	backupCfg = BackupConfig{BackupDir: backupDir, PhotoDir: photoDir}
	oldBackup := filepath.Join(backupDir, "autorepair_20000101_010101.tar.gz")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldBackup, []byte("expired"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(oldBackup, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if err := DailyBackup(); err != nil {
		t.Fatalf("DailyBackup: %v", err)
	}
	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Fatalf("backup older than 30 days was retained, stat error=%v", err)
	}
	var path string
	var photoCount int
	var status string
	if err := d.QueryRow(`SELECT db_backup_path, photo_count, status FROM backup_records ORDER BY id DESC LIMIT 1`).
		Scan(&path, &photoCount, &status); err != nil {
		t.Fatal(err)
	}
	if status != "success" || photoCount != 2 {
		t.Fatalf("backup status=%q photo_count=%d; want success and 2", status, photoCount)
	}
	if !strings.HasSuffix(path, ".tar.gz") {
		t.Fatalf("backup path=%q; expected gzip tar archive", path)
	}
	if err := verifyBackupArchive(path); err != nil {
		t.Fatalf("archive restore validation: %v", err)
	}
}

func TestDailyBackupRecordsPhotoArchiveFailure(t *testing.T) {
	d := backupTestDB(t)
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	photoPath := filepath.Join(root, "photos-is-a-file")
	if err := os.WriteFile(photoPath, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}
	backupCfg = BackupConfig{BackupDir: backupDir, PhotoDir: photoPath}

	if err := DailyBackup(); err == nil {
		t.Fatal("DailyBackup unexpectedly succeeded with a non-directory photo source")
	}
	var status, reason string
	if err := d.QueryRow(`SELECT status, error_message FROM backup_records ORDER BY id DESC LIMIT 1`).
		Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(reason, "照片") {
		t.Fatalf("backup status=%q error=%q; expected recorded photo backup failure", status, reason)
	}
}

func verifyBackupArchive(path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	foundDB, foundPhoto, foundPending := false, false, false
	restoreDir, err := os.MkdirTemp("", "autorepairos-restore-check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(restoreDir)
	var restoredDB string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(header.Name, "autorepair_") && strings.HasSuffix(header.Name, ".db"):
			foundDB = true
			restoredDB = filepath.Join(restoreDir, filepath.FromSlash(header.Name))
		case header.Name == "photos/repair.png":
			foundPhoto = true
		case header.Name == "photos/pending/queued.bin":
			foundPending = true
		}
		destination := filepath.Join(restoreDir, filepath.FromSlash(header.Name))
		if !strings.HasPrefix(destination, restoreDir+string(os.PathSeparator)) {
			return fmt.Errorf("invalid archive path %q", header.Name)
		}
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(destination, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		out, err := os.Create(destination)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if !foundDB || !foundPhoto || !foundPending {
		return fmt.Errorf("archive members present: db=%t photo=%t pending=%t", foundDB, foundPhoto, foundPending)
	}
	for relativePath, expected := range map[string]string{
		"photos/repair.png":         "photo-data",
		"photos/pending/queued.bin": "queued-photo",
	} {
		data, err := os.ReadFile(filepath.Join(restoreDir, filepath.FromSlash(relativePath)))
		if err != nil {
			return err
		}
		if string(data) != expected {
			return fmt.Errorf("restored content mismatch for %s", relativePath)
		}
	}
	restored, err := sql.Open("sqlite", restoredDB)
	if err != nil {
		return err
	}
	defer restored.Close()
	var integrity string
	if err := restored.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("restored database integrity check: %s", integrity)
	}
	return nil
}
