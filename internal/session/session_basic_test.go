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

func TestCSRFToken(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "sess2.db"), EnableWAL: false}
	if err := db.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	token, err := Start(2)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ct, err := CSRFToken(token)
	if err != nil || ct == "" {
		t.Fatalf("CSRFToken: %v %q", err, ct)
	}
}

func TestDeleteForUser(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{DBPath: filepath.Join(dir, "sess3.db"), EnableWAL: false}
	if err := db.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	token, err := Start(3)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	DeleteForUser(3)
	if _, err := Lookup(token); err == nil {
		t.Fatal("expected error after DeleteForUser")
	}
}

func TestParseTimeRFC3339(t *testing.T) {
	if _, err := parseTime("2020-01-01T00:00:00Z"); err != nil {
		t.Fatalf("parseTime RFC3339: %v", err)
	}
	if _, err := parseTime("2020-01-01 00:00:00"); err != nil {
		t.Fatalf("parseTime SQL: %v", err)
	}
}
