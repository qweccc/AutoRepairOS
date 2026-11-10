package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"autorepairos/internal/config"
)

func initTestDB(t *testing.T, name string, wal bool) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, name), EnableWAL: wal}
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() { Close() })
}

func TestImmediateTxQueryRowRollback(t *testing.T) {
	initTestDB(t, "tx.db", false)
	tx, err := BeginImmediate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("CREATE TABLE t (id INTEGER, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO t VALUES (1, 'a')"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query("SELECT name FROM t WHERE id=1")
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if rows.Next() {
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
	}
	_ = rows.Close()
	if name != "a" {
		t.Fatalf("name=%q", name)
	}
	var n int
	if err := tx.QueryRow("SELECT COUNT(*) FROM t").Scan(&n); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	// double rollback / commit after rollback -> ErrTxDone
	if err := tx.Rollback(); err != sql.ErrTxDone {
		t.Fatalf("want ErrTxDone got %v", err)
	}
	if err := tx.Commit(); err != sql.ErrTxDone {
		t.Fatalf("want ErrTxDone got %v", err)
	}
	// rolled back table must not exist
	var cnt int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='t'`).Scan(&cnt); err != nil || cnt != 0 {
		t.Fatalf("cnt=%d err=%v", cnt, err)
	}
}

func TestImmediateTxCommitError(t *testing.T) {
	initTestDB(t, "txerr.db", false)
	tx, err := BeginImmediate()
	if err != nil {
		t.Fatal(err)
	}
	// Close underlying conn so COMMIT exec fails
	_ = tx.conn.Close()
	if err := tx.Commit(); err == nil {
		t.Fatal("expected commit error")
	}
	// Commit error path sets closed=true; second commit -> ErrTxDone
	if err := tx.Commit(); err != sql.ErrTxDone {
		t.Fatalf("want ErrTxDone got %v", err)
	}
}

func TestBeginImmediateClosedDB(t *testing.T) {
	initTestDB(t, "closed.db", false)
	_ = DB.Close()
	if _, err := BeginImmediate(); err == nil {
		t.Fatal("expected conn error")
	}
}

func TestInitDSNBranches(t *testing.T) {
	t.Run("file prefix DSN", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.Config{DBPath: "file:" + filepath.Join(dir, "dsn.db"), EnableWAL: false}
		if err := Init(cfg); err != nil {
			t.Fatal(err)
		}
		defer Close()
		if Path() == "" {
			t.Fatal("empty path")
		}
	})
	t.Run("relative path joins data dir", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		cfg := &config.Config{DBPath: "rel.db", EnableWAL: false}
		if err := Init(cfg); err != nil {
			t.Fatal(err)
		}
		defer Close()
		if Path() != "data/autorepair.db" && Path() != `data\autorepair.db` {
			t.Fatalf("path=%q", Path())
		}
	})
	t.Run("WAL on memory db mismatch log branch", func(t *testing.T) {
		cfg := &config.Config{DBPath: "file::memory:?cache=shared", EnableWAL: true}
		if err := Init(cfg); err != nil {
			t.Fatal(err)
		}
		defer Close()
	})
	t.Run("WAL enabled on file db", func(t *testing.T) {
		initTestDB(t, "wal.db", true)
	})
	t.Run("ping fails when path is directory", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &config.Config{DBPath: dir, EnableWAL: false}
		if err := Init(cfg); err == nil {
			t.Fatal("expected ping/open error")
		}
	})
	t.Run("mkdirall fails when data is a file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "data"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		cfg := &config.Config{DBPath: "x.db", EnableWAL: false}
		if err := Init(cfg); err == nil {
			t.Fatal("expected mkdir error")
		}
	})
}

func TestJournalModeUnknownAndMustExecError(t *testing.T) {
	initTestDB(t, "jm.db", false)
	_ = DB.Close()
	if got := journalMode(); got != "unknown" {
		t.Fatalf("got %q", got)
	}
	// MustExec with bad SQL must not panic
	MustExec("THIS IS NOT SQL")
}

func TestCloseAfterDBClosed(t *testing.T) {
	initTestDB(t, "closeclosed.db", false)
	_ = DB.Close()
	Close() // checkpoint + close errors are logged, must not panic
}

func TestCloseNilDB(t *testing.T) {
	saved := DB
	DB = nil
	defer func() { DB = saved }()
	Close() // must not panic
}

func TestMigrateTwiceAndClosedError(t *testing.T) {
	initTestDB(t, "mig.db", false)
	if err := Migrate(); err != nil {
		t.Fatal(err)
	}
	// second run: all versions already applied -> skip branch
	if err := Migrate(); err != nil {
		t.Fatal(err)
	}
	_ = DB.Close()
	if err := Migrate(); err == nil {
		t.Fatal("expected appliedVersions error on closed DB")
	}
}

func TestExecScriptErrorDirect(t *testing.T) {
	initTestDB(t, "script.db", false)
	tx, err := DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := execScript(tx, "CREATE TABLE x (id INTEGER);\nBOGUS SQL HERE;"); err == nil {
		t.Fatal("expected execScript error")
	}
}

func TestAppliedVersionsDirect(t *testing.T) {
	initTestDB(t, "av.db", false)
	if _, err := appliedVersions(); err == nil {
		t.Fatal("expected error before schema_migrations exists")
	}
	if err := Migrate(); err != nil {
		t.Fatal(err)
	}
	applied, err := appliedVersions()
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 {
		t.Fatal("expected applied versions")
	}
}
