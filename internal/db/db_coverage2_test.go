package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autorepairos/internal/config"
)

// fake driver: lets production code hit error branches that a healthy
// SQLite database never reaches (PRAGMA/BEGIN/commit/record failures).
type fakeConnector struct{ mode string }

func (c fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return fakeConn{mode: c.mode}, nil
}

// Connect signature must match driver.Connector exactly (context.Context).
func (c fakeConnector) Driver() driver.Driver { return fakeDriver{mode: c.mode} }

type fakeDriver struct{ mode string }

func (d fakeDriver) Open(string) (driver.Conn, error) { return fakeConn{mode: d.mode}, nil }

type fakeConn struct{ mode string }

func (c fakeConn) Prepare(query string) (driver.Stmt, error) {
	return fakeStmt{mode: c.mode, query: query}, nil
}
func (c fakeConn) Close() error {
	if c.mode == "closeerr" {
		return errors.New("fake conn close error")
	}
	return nil
}
func (c fakeConn) Begin() (driver.Tx, error) {
	if c.mode == "beginerr" {
		return nil, errors.New("fake begin error")
	}
	return fakeTx{mode: c.mode}, nil
}

type fakeStmt struct {
	mode, query string
}

func (s fakeStmt) Close() error  { return nil }
func (s fakeStmt) NumInput() int { return -1 }
func (s fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	switch s.mode {
	case "pragmafail":
		if strings.Contains(s.query, "PRAGMA busy_timeout") {
			return nil, errors.New("fake pragma error")
		}
	case "beginimmfail":
		if strings.Contains(s.query, "BEGIN IMMEDIATE") {
			return nil, errors.New("fake begin immediate error")
		}
	case "scriptfail":
		if !strings.Contains(s.query, "schema_migrations") {
			return nil, errors.New("fake script error")
		}
	case "recordfail":
		if strings.Contains(s.query, "INSERT OR IGNORE INTO schema_migrations") {
			return nil, errors.New("fake record error")
		}
	}
	return driver.RowsAffected(0), nil
}
func (s fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	if s.mode == "scanerr" {
		return &fakeRows{cols: nil, rows: 1}, nil
	}
	return &fakeRows{cols: []string{"version"}}, nil
}

type fakeRows struct {
	cols []string
	rows int
	done bool
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next([]driver.Value) error {
	if r.rows > 0 && !r.done {
		r.done = true
		return nil
	}
	return io.EOF
}

type fakeTx struct{ mode string }

func (t fakeTx) Commit() error {
	if t.mode == "commitfail" {
		return errors.New("fake commit error")
	}
	return nil
}
func (t fakeTx) Rollback() error { return nil }

func setFakeDB(t *testing.T, mode string) {
	t.Helper()
	saved := DB
	fake := sql.OpenDB(fakeConnector{mode: mode})
	DB = fake
	t.Cleanup(func() {
		_ = fake.Close()
		DB = saved
	})
}

func TestBeginImmediatePragmaError(t *testing.T) {
	setFakeDB(t, "pragmafail")
	if _, err := BeginImmediate(); err == nil {
		t.Fatal("expected pragma error")
	}
}

func TestBeginImmediateBeginError(t *testing.T) {
	setFakeDB(t, "beginimmfail")
	if _, err := BeginImmediate(); err == nil {
		t.Fatal("expected BEGIN IMMEDIATE error")
	}
}

func TestCloseDBCloseError(t *testing.T) {
	setFakeDB(t, "closeerr")
	Close() // checkpoint ok, DB.Close error is logged, must not panic
}

func TestMigrateBeginError(t *testing.T) {
	setFakeDB(t, "beginerr")
	err := Migrate()
	if err == nil || !strings.Contains(err.Error(), "開始 transaction 失敗") {
		t.Fatalf("err=%v", err)
	}
}

func TestMigrateExecScriptError(t *testing.T) {
	setFakeDB(t, "scriptfail")
	err := Migrate()
	if err == nil || !strings.Contains(err.Error(), "套用 migration") {
		t.Fatalf("err=%v", err)
	}
}

func TestMigrateRecordError(t *testing.T) {
	setFakeDB(t, "recordfail")
	err := Migrate()
	if err == nil || !strings.Contains(err.Error(), "記錄 migration") {
		t.Fatalf("err=%v", err)
	}
}

func TestMigrateCommitError(t *testing.T) {
	setFakeDB(t, "commitfail")
	err := Migrate()
	if err == nil || !strings.Contains(err.Error(), "提交 migration") {
		t.Fatalf("err=%v", err)
	}
}

func TestMigrateScanError(t *testing.T) {
	setFakeDB(t, "scanerr")
	if err := Migrate(); err == nil {
		t.Fatal("expected scan error via appliedVersions")
	}
}

func TestInitWALExecErrorReadOnly(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ro.db")
	if err := Init(&config.Config{DBPath: p, EnableWAL: false}); err != nil {
		t.Fatal(err)
	}
	MustExec("CREATE TABLE t (id INTEGER)")
	Close()
	// mode=ro makes PRAGMA journal_mode=WAL fail (readonly) -> log branch.
	if err := Init(&config.Config{DBPath: "file:" + p + "?mode=ro", EnableWAL: true}); err != nil {
		t.Fatal(err)
	}
	Close()
	_ = os.Chmod(p, 0o644) // cleanup on Windows
}
