package db

import (
	"os"
	"path/filepath"
	"testing"

	"autorepairos/internal/config"
)

func TestPathBeforeInit(t *testing.T) {
	if Path() != "" {
		t.Fatalf("expected empty path before Init, got %q", Path())
	}
}

func TestInitAndPath(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "test.db"), EnableWAL: false}
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if Path() == "" {
		t.Fatal("expected non-empty path after Init")
	}
	defer func() { os.Remove(Path()) }()
	Close()
}

func TestBeginImmediate(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "test2.db"), EnableWAL: false}
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()
	tx, err := BeginImmediate()
	if err != nil {
		t.Fatalf("BeginImmediate: %v", err)
	}
	if _, err := tx.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestMigrate(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "test3.db"), EnableWAL: false}
	if err := Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()
	if err := Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}