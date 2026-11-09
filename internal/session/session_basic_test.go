package session

import (
	"path/filepath"
	"testing"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
)

func TestStartLookupDelete(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "sess.db"), EnableWAL: false}
	if err := db.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	token, err := Start(1)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	uid, err := Lookup(token)
	if err != nil || uid != 1 {
		t.Fatalf("Lookup: %v uid=%d", err, uid)
	}
	Delete(token)
	if _, err := Lookup(token); err == nil {
		t.Fatal("expected error after delete")
	}
}