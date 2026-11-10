package cron

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSetBackupDir(t *testing.T) {
	orig := backupCfg
	t.Cleanup(func() { backupCfg = orig })
	SetBackupDir("")
	if backupCfg.BackupDir != orig.BackupDir {
		t.Fatal("empty dir should be a no-op")
	}
	SetBackupDir(filepath.Join(t.TempDir(), "x"))
	if backupCfg.BackupDir == orig.BackupDir {
		t.Fatal("non-empty dir should replace BackupDir")
	}
}

func TestBackupSrcPath(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want string
	}{
		{"file:", "data"},
		{"file:C:/tmp/sub/src.db?cache=shared", filepath.ToSlash(filepath.Dir("C:/tmp/sub/src.db"))},
		{"rel/dir/app.db?x=1", filepath.ToSlash(filepath.Dir("rel/dir/app.db"))},
	} {
		t.Setenv("DB_PATH", tc.env)
		if got := filepath.ToSlash(backupSrcPath()); got != tc.want {
			t.Errorf("DB_PATH=%q: got %q want %q", tc.env, got, tc.want)
		}
	}
}

func TestCopyFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, "dst.txt")
	n, err := copyFile(src, dst)
	if err != nil || n != 5 {
		t.Fatalf("copyFile n=%d err=%v", n, err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "hello" {
		t.Fatalf("copied content=%q", data)
	}
	if _, err := copyFile(filepath.Join(root, "missing"), dst); err == nil {
		t.Fatal("missing src should error")
	}
	if _, err := copyFile(src, filepath.Join(root, "no-dir", "dst.txt")); err == nil {
		t.Fatal("dst in missing dir should error")
	}
}

func TestDirStatsBranches(t *testing.T) {
	root := t.TempDir()
	// Missing dir → 0,0,nil.
	count, size, err := dirStats(filepath.Join(root, "missing"))
	if count != 0 || size != 0 || err != nil {
		t.Fatalf("missing dir: %d %d %v", count, size, err)
	}
	// Regular file → error.
	f := filepath.Join(root, "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := dirStats(f); err == nil {
		t.Fatal("file root should error")
	}
	// Dir with files and subdir.
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("12345"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("12"), 0644); err != nil {
		t.Fatal(err)
	}
	count, size, err = dirStats(root)
	if err != nil || count != 3 || size != 8 {
		t.Fatalf("dirStats=%d/%d/%v want 3/8/nil", count, size, err)
	}
}

func TestPruneSameDay(t *testing.T) {
	orig := backupCfg
	t.Cleanup(func() { backupCfg = orig })
	root := t.TempDir()
	backupCfg.BackupDir = root
	// Missing dir → silent return.
	backupCfg.BackupDir = filepath.Join(root, "missing")
	pruneSameDay("20260102", "keep")
	backupCfg.BackupDir = root
	files := map[string]bool{
		"autorepair_20260102_010101.tar.gz": false, // same day, removed
		"autorepair_20260102_020202.db":     false, // same day, removed
		"autorepair_20260103_010101.tar.gz": true,  // different day, kept
		"other.txt":                         true,  // wrong suffix, kept
		"autorepair_20260102_keep.tar.gz":   true,  // keepPath, kept
	}
	for name := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "autorepair_20260102_dir.tar.gz"), 0755); err != nil {
		t.Fatal(err)
	}
	pruneSameDay("20260102", filepath.Join(root, "autorepair_20260102_keep.tar.gz"))
	for name, want := range files {
		_, err := os.Stat(filepath.Join(root, name))
		if want && err != nil {
			t.Errorf("%s should be kept: %v", name, err)
		}
		if !want && !os.IsNotExist(err) {
			t.Errorf("%s should be removed, stat err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "autorepair_20260102_dir.tar.gz")); err != nil {
		t.Errorf("subdir should be kept: %v", err)
	}
}

func TestPruneBackupsMissingDir(t *testing.T) {
	orig := backupCfg
	t.Cleanup(func() { backupCfg = orig })
	backupCfg.BackupDir = filepath.Join(t.TempDir(), "missing")
	pruneBackups(time.Hour) // must not panic
}

func TestBackupDBVacuumFallbackError(t *testing.T) {
	backupTestDB(t)
	// dst in a nonexistent directory: VACUUM INTO fails, fallback copy also fails.
	dst := filepath.Join(t.TempDir(), "no-dir", "out.db")
	if _, err := backupDB(dst); err == nil {
		t.Fatal("backupDB with unreachable dst should error")
	}
}

func TestBackupDBExistingTarget(t *testing.T) {
	d := backupTestDB(t)
	if _, err := d.Exec(`CREATE TABLE t1 (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "snap.db")
	if _, err := backupDB(dst); err != nil {
		t.Fatalf("backupDB: %v", err)
	}
	// Second call to same dst: exercises VACUUM INTO failure or overwrite path.
	_, _ = backupDB(dst)
}

func TestCreateBackupArchiveBranches(t *testing.T) {
	root := t.TempDir()
	dbFile := filepath.Join(root, "snap.db")
	if err := os.WriteFile(dbFile, []byte("db-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	// Missing photo dir → archive with photos/ header only.
	archive := filepath.Join(root, "a.tar.gz")
	if err := createBackupArchive(dbFile, archive, filepath.Join(root, "no-photos")); err != nil {
		t.Fatal(err)
	}
	if err := verifyArchiveHas(archive, "photos/"); err != nil {
		t.Fatal(err)
	}
	// Photo dir is a file → error, archive removed.
	photoFile := filepath.Join(root, "photos-file")
	if err := os.WriteFile(photoFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	archive2 := filepath.Join(root, "b.tar.gz")
	if err := createBackupArchive(dbFile, archive2, photoFile); err == nil {
		t.Fatal("non-dir photo source should error")
	}
	if _, err := os.Stat(archive2); !os.IsNotExist(err) {
		t.Fatalf("failed archive should be removed, err=%v", err)
	}
	// Missing DB file → wrapped error, archive removed.
	archive3 := filepath.Join(root, "c.tar.gz")
	if err := createBackupArchive(filepath.Join(root, "missing.db"), archive3, filepath.Join(root, "no-photos")); err == nil {
		t.Fatal("missing db file should error")
	}
	if _, err := os.Stat(archive3); !os.IsNotExist(err) {
		t.Fatalf("failed archive should be removed, err=%v", err)
	}
	// Unwritable archive path → create error.
	if err := createBackupArchive(dbFile, filepath.Join(root, "no-dir", "x.tar.gz"), filepath.Join(root, "no-photos")); err == nil {
		t.Fatal("unwritable archive path should error")
	}
}

func verifyArchiveHas(path, member string) error {
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
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Name == member {
			return nil
		}
	}
	return &os.PathError{Op: "find", Path: member, Err: errNotFound}
}

var errNotFound = errString("member not found")

type errString string

func (e errString) Error() string { return string(e) }

func TestAddArchiveHelpersErrors(t *testing.T) {
	root := t.TempDir()
	// addArchiveFile with missing path → stat error.
	out, err := os.Create(filepath.Join(root, "x.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(out)
	if err := addArchiveFile(tw, filepath.Join(root, "missing"), "x.db"); err == nil {
		t.Fatal("missing file should error in addArchiveFile")
	}
	if err := addArchiveContents(tw, filepath.Join(root, "missing")); err == nil {
		t.Fatal("missing file should error in addArchiveContents")
	}
	_ = tw.Close()
	_ = out.Close()
	// Successful direct add.
	p := filepath.Join(root, "f.txt")
	if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	out2, err := os.Create(filepath.Join(root, "y.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(out2)
	tw2 := tar.NewWriter(gz)
	if err := addArchiveFile(tw2, p, "f.txt"); err != nil {
		t.Fatal(err)
	}
	_ = tw2.Close()
	_ = gz.Close()
	_ = out2.Close()
	if err := verifyArchiveHas(filepath.Join(root, "y.tar.gz"), "f.txt"); err != nil {
		t.Fatal(err)
	}
}

func TestDailyBackupMkdirFailure(t *testing.T) {
	d := backupTestDB(t)
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	backupCfg = BackupConfig{BackupDir: filepath.Join(blocker, "sub"), PhotoDir: filepath.Join(root, "photos")}
	if err := DailyBackup(); err == nil {
		t.Fatal("DailyBackup should fail when BackupDir cannot be created")
	}
	var status string
	if err := d.QueryRow(`SELECT status FROM backup_records ORDER BY id DESC LIMIT 1`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("status=%q", status)
	}
}

func TestRecordBackupFailureNested(t *testing.T) {
	d := backupTestDB(t)
	// Drop table so recordBackup itself fails.
	if _, err := d.Exec(`DROP TABLE backup_records`); err != nil {
		t.Fatal(err)
	}
	err := recordBackupFailure("", "cause-x")
	if err == nil || !strings.Contains(err.Error(), "記錄備份失敗也失敗") {
		t.Fatalf("err=%v", err)
	}
	// recordBackup error path directly
	if err := recordBackup("p", 1, 2, 3, ""); err == nil {
		t.Fatal("recordBackup should error without table")
	}
}
