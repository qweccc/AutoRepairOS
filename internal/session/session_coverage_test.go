package session

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"autorepairos/internal/db"
)

// newMemDB installs an in-memory SQLite DB as the package-global db.DB
// (session functions read it directly) and restores the previous value
// on cleanup. Session funcs only use QueryRow/Exec (no open Rows), so
// a single connection cannot deadlock here.
func newMemDB(t *testing.T) *sql.DB {
	t.Helper()
	orig := db.DB
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	if _, err := d.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		csrf_token TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}
	db.DB = d
	t.Cleanup(func() {
		db.DB = orig
		_ = d.Close()
	})
	return d
}

func TestCSRFContextRoundTrip(t *testing.T) {
	ctx := WithCSRFToken(context.Background(), "tok-123")
	if got := CSRFTokenFromContext(ctx); got != "tok-123" {
		t.Fatalf("got %q, want tok-123", got)
	}
	// No token attached.
	if got := CSRFTokenFromContext(context.Background()); got != "" {
		t.Fatalf("background ctx got %q, want empty", got)
	}
	// Wrong type stored under the key.
	wrong := context.WithValue(context.Background(), csrfContextKey{}, 42)
	if got := CSRFTokenFromContext(wrong); got != "" {
		t.Fatalf("wrong-type ctx got %q, want empty", got)
	}
	// Empty token round-trips as empty (stored, not missing).
	empty := WithCSRFToken(context.Background(), "")
	if got := CSRFTokenFromContext(empty); got != "" {
		t.Fatalf("empty token got %q, want empty", got)
	}
}

func TestTokenFromRequest(t *testing.T) {
	// No cookie at all.
	r := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	if got := TokenFromRequest(r); got != "" {
		t.Fatalf("no-cookie got %q, want empty", got)
	}
	// Malformed cookie header is ignored by r.Cookie -> empty.
	r2 := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	r2.Header.Set("Cookie", ";;;")
	if got := TokenFromRequest(r2); got != "" {
		t.Fatalf("malformed cookie got %q, want empty", got)
	}
	// A different cookie does not count.
	r3 := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	r3.AddCookie(&http.Cookie{Name: "other", Value: "x"})
	if got := TokenFromRequest(r3); got != "" {
		t.Fatalf("other cookie got %q, want empty", got)
	}
	// Real session cookie.
	r4 := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	r4.AddCookie(&http.Cookie{Name: cookieName, Value: "abc123"})
	if got := TokenFromRequest(r4); got != "abc123" {
		t.Fatalf("session cookie got %q, want abc123", got)
	}
	// Empty session cookie value.
	r5 := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	r5.AddCookie(&http.Cookie{Name: cookieName, Value: ""})
	if got := TokenFromRequest(r5); got != "" {
		t.Fatalf("empty session cookie got %q, want empty", got)
	}
}

func TestConstantTimeEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"same", "same", true},
		{"same", "diff", false},
		{"abc", "ab", false},
		{"", "", true},
		{"", "x", false},
		{"x", "", false},
		{"token-with-dashes_123", "token-with-dashes_123", true},
	}
	for _, tc := range tests {
		if got := ConstantTimeEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("ConstantTimeEqual(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestExpiredTable(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"garbage is not expired", "not-a-time", false},
		{"empty is not expired", "", false},
		{"old RFC3339", now.Add(-30 * 24 * time.Hour).Format(time.RFC3339), true},
		{"recent RFC3339", now.Add(-time.Hour).Format(time.RFC3339), false},
		{"old SQL format", now.Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05"), true},
		{"recent SQL format", now.Add(-time.Hour).Format("2006-01-02 15:04:05"), false},
		{"just inside TTL SQL", now.Add(-6 * 24 * time.Hour).Format("2006-01-02 15:04:05"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := expired(tc.in); got != tc.want {
				t.Fatalf("expired(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
	if _, err := parseTime("garbage"); err == nil {
		t.Fatal("parseTime(garbage) should error")
	}
}

func TestRequestIsSecureViaForwardedProto(t *testing.T) {
	tests := []struct {
		name    string
		proto   string
		wantSec bool
	}{
		{"https", "https", true},
		{"HTTPS upper", " HTTPS ", true},
		{"http", "http", false},
		{"empty", "", false},
		{"other", "ws", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			rec := httptest.NewRecorder()
			SetCookie(rec, r, "tok")
			cookies := rec.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Secure != tc.wantSec {
				t.Fatalf("Secure=%v want %v (%+v)", cookies, tc.wantSec, cookies)
			}
		})
	}
}

func TestLookupBranches(t *testing.T) {
	d := newMemDB(t)

	// Empty / whitespace tokens.
	for _, tok := range []string{"", "   "} {
		if _, err := Lookup(tok); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("Lookup(%q) err=%v want ErrNoRows", tok, err)
		}
	}
	// Not found.
	if _, err := Lookup("missing-token"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Lookup(missing) err=%v want ErrNoRows", err)
	}

	// Expired session: Lookup must delete it and return ErrNoRows.
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	if _, err := d.Exec(`INSERT INTO sessions (id,user_id,csrf_token,created_at) VALUES ('old',7,'c',?)`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup("old"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("Lookup(expired) err=%v want ErrNoRows", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id='old'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("expired row not deleted: n=%d err=%v", n, err)
	}

	// Unparseable created_at: conservatively valid.
	if _, err := d.Exec(`INSERT INTO sessions (id,user_id,csrf_token,created_at) VALUES ('weird',9,'c','garbage')`); err != nil {
		t.Fatal(err)
	}
	if uid, err := Lookup("weird"); err != nil || uid != 9 {
		t.Fatalf("Lookup(weird)=%d,%v want 9,nil", uid, err)
	}

	// Recent RFC3339 created_at: valid.
	recent := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.Exec(`INSERT INTO sessions (id,user_id,csrf_token,created_at) VALUES ('fresh',11,'c',?)`, recent); err != nil {
		t.Fatal(err)
	}
	if uid, err := Lookup("fresh"); err != nil || uid != 11 {
		t.Fatalf("Lookup(fresh)=%d,%v want 11,nil", uid, err)
	}
}

func TestCSRFTokenBranches(t *testing.T) {
	d := newMemDB(t)

	// Empty / whitespace.
	for _, tok := range []string{"", "  "} {
		if _, err := CSRFToken(tok); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("CSRFToken(%q) err=%v want ErrNoRows", tok, err)
		}
	}
	// Not found.
	if _, err := CSRFToken("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CSRFToken(missing) err=%v want ErrNoRows", err)
	}

	// Existing non-empty token: returned as-is, no generation.
	if _, err := d.Exec(`INSERT INTO sessions (id,user_id,csrf_token,created_at) VALUES ('has',1,'preset-csrf',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if got, err := CSRFToken("has"); err != nil || got != "preset-csrf" {
		t.Fatalf("CSRFToken(has)=%q,%v want preset-csrf", got, err)
	}

	// Empty csrf_token (pre-CSRF session): generates, persists, returns.
	if _, err := d.Exec(`INSERT INTO sessions (id,user_id,csrf_token,created_at) VALUES ('legacy',2,'',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	gen, err := CSRFToken("legacy")
	if err != nil || gen == "" {
		t.Fatalf("CSRFToken(legacy)=%q,%v want generated", gen, err)
	}
	var stored string
	if err := d.QueryRow(`SELECT csrf_token FROM sessions WHERE id='legacy'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != gen {
		t.Fatalf("generated token not persisted: stored=%q gen=%q", stored, gen)
	}
	// Second call returns the same (now non-empty) token.
	again, err := CSRFToken("legacy")
	if err != nil || again != gen {
		t.Fatalf("CSRFToken(legacy) again=%q,%v want %q", again, err, gen)
	}
}

func TestCSRFTokenUpdateFailure(t *testing.T) {
	d := newMemDB(t)
	if _, err := d.Exec(`INSERT INTO sessions (id,user_id,csrf_token,created_at) VALUES ('legacy',2,'',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TRIGGER reject_update BEFORE UPDATE ON sessions
		BEGIN SELECT RAISE(ABORT, 'no updates'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := CSRFToken("legacy"); err == nil || !strings.Contains(err.Error(), "no updates") {
		t.Fatalf("CSRFToken err=%v want update rejection", err)
	}
}

func TestCSRFTokenSecondQueryFailure(t *testing.T) {
	d := newMemDB(t)
	if _, err := d.Exec(`INSERT INTO sessions (id,user_id,csrf_token,created_at) VALUES ('legacy',2,'',CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	// After the UPDATE succeeds, delete the row so the re-SELECT finds
	// nothing and returns ErrNoRows.
	if _, err := d.Exec(`CREATE TRIGGER delete_after_update AFTER UPDATE ON sessions
		BEGIN DELETE FROM sessions WHERE id = NEW.id; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := CSRFToken("legacy"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CSRFToken err=%v want ErrNoRows", err)
	}
}

func TestStartInsertFailure(t *testing.T) {
	d := newMemDB(t)
	if _, err := d.Exec(`CREATE TRIGGER reject_insert BEFORE INSERT ON sessions
		BEGIN SELECT RAISE(ABORT, 'no inserts'); END`); err != nil {
		t.Fatal(err)
	}
	tok, err := Start(1)
	if tok != "" || err == nil || !strings.Contains(err.Error(), "no inserts") {
		t.Fatalf("Start=%q,%v want insert failure", tok, err)
	}
}

func TestStartCreatesSessionAndCSRF(t *testing.T) {
	d := newMemDB(t)
	tok, err := Start(42)
	if err != nil || tok == "" {
		t.Fatalf("Start=%q,%v", tok, err)
	}
	ct, err := CSRFToken(tok)
	if err != nil || ct == "" || ct == tok {
		t.Fatalf("CSRFToken=%q,%v", ct, err)
	}
	var uid int64
	var storedCT string
	if err := d.QueryRow(`SELECT user_id, csrf_token FROM sessions WHERE id=?`, tok).Scan(&uid, &storedCT); err != nil {
		t.Fatal(err)
	}
	if uid != 42 || storedCT != ct {
		t.Fatalf("stored uid=%d csrf=%q", uid, storedCT)
	}
	// Two sessions get different tokens.
	tok2, err := Start(42)
	if err != nil || tok2 == tok {
		t.Fatalf("second Start=%q,%v", tok2, err)
	}
}

func TestDeleteEmptyAndMissing(t *testing.T) {
	// Delete("") must be a no-op and not touch the DB (works even with nil DB).
	orig := db.DB
	db.DB = nil
	Delete("")
	db.DB = orig

	d := newMemDB(t)
	// Deleting a nonexistent token is a no-op (MustExec logs, no panic).
	Delete("does-not-exist")
	// DeleteForUser with no rows is a no-op.
	DeleteForUser(9999)
	// After DeleteForUser the user's sessions are gone.
	tok, err := Start(5)
	if err != nil {
		t.Fatal(err)
	}
	DeleteForUser(5)
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id=?`, tok).Scan(&n); err != nil || n != 0 {
		t.Fatalf("DeleteForUser left n=%d err=%v", n, err)
	}
}
