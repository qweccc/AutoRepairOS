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