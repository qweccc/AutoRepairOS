package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func loadTestDicts(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	zh := `{"workorder.title":"工單管理","common.save":"儲存"}`
	en := `{"workorder.title":"Work Order Management"}`
	if err := os.WriteFile(filepath.Join(dir, "zh-TW.json"), []byte(zh), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "en.json"), []byte(en), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Load(dir); err != nil {
		t.Fatal(err)
	}
}

func TestTByLocale(t *testing.T) {
	loadTestDicts(t)
	if got := T(WithLang(context.Background(), "zh-TW"), "workorder.title"); got != "工單管理" {
		t.Fatalf("zh-TW = %q", got)
	}
	if got := T(WithLang(context.Background(), "en"), "workorder.title"); got != "Work Order Management" {
		t.Fatalf("en = %q", got)
	}
	// en missing key falls back to default (zh-TW) dictionary
	if got := T(WithLang(context.Background(), "en"), "common.save"); got != "儲存" {
		t.Fatalf("fallback = %q", got)
	}
	// unknown key returns the key itself
	if got := T(context.Background(), "no.such.key"); got != "no.such.key" {
		t.Fatalf("missing = %q", got)
	}
	// no context -> default locale
	if got := T(context.Background(), "workorder.title"); got != "工單管理" {
		t.Fatalf("default = %q", got)
	}
	if got := T(nil, "workorder.title"); got != "工單管理" {
		t.Fatalf("nil ctx = %q", got)
	}
}

func TestNormalize(t *testing.T) {
	loadTestDicts(t)
	for in, want := range map[string]string{
		"en": "en", "EN": "en", "en-US": "en", "zh-TW": "zh-TW",
		"zh": "", "fr": "", "": "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFromRequestCookieAndHeader(t *testing.T) {
	loadTestDicts(t)
	// cookie wins
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "en"})
	r.Header.Set("Accept-Language", "zh-TW")
	if got := FromRequest(r); got != "en" {
		t.Fatalf("cookie = %q", got)
	}
	// header used when cookie missing
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-TW;q=0.8")
	if got := FromRequest(r); got != "en" {
		t.Fatalf("header = %q", got)
	}
	// unsupported everything -> default
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "fr-FR")
	if got := FromRequest(r); got != DefaultLang {
		t.Fatalf("default = %q", got)
	}
	if got := FromRequest(nil); got != DefaultLang {
		t.Fatalf("nil req = %q", got)
	}
}

func TestMiddlewareInjectsLang(t *testing.T) {
	loadTestDicts(t)
	var got string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = LangOf(r.Context())
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "en"})
	r.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	var theme string
	next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = LangOf(r.Context())
		theme = ThemeOf(r.Context())
	})
	Middleware(next).ServeHTTP(httptest.NewRecorder(), r)
	if got != "en" || theme != "light" {
		t.Fatalf("ctx lang=%q theme=%q", got, theme)
	}
}

func TestThemeFromRequest(t *testing.T) {
	if got := ThemeFromRequest(nil); got != "dark" {
		t.Fatalf("nil = %q", got)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	if got := ThemeFromRequest(r); got != "light" {
		t.Fatalf("cookie = %q", got)
	}
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "theme", Value: "sepia"})
	if got := ThemeFromRequest(r); got != "dark" {
		t.Fatalf("invalid cookie = %q", got)
	}
}

func TestLoadErrors(t *testing.T) {
	if err := Load(t.TempDir()); err == nil {
		t.Fatal("empty dir should error")
	}
	if err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing dir should error")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "en.json"), []byte("{bad json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Load(dir); err == nil {
		t.Fatal("bad json should error")
	}
}

func TestSupportedAndContext(t *testing.T) {
	loadTestDicts(t)
	if !Supported("en") || !Supported("zh-TW") || Supported("fr") {
		t.Fatal("Supported")
	}
	ctx := WithLang(context.Background(), "en")
	if LangOf(ctx) != "en" {
		t.Fatal("LangOf")
	}
	if LangOf(context.Background()) != DefaultLang {
		t.Fatal("LangOf default")
	}
	if ThemeOf(ctx) != "dark" {
		t.Fatal("ThemeOf default")
	}
	ctx = WithTheme(ctx, "light")
	if ThemeOf(ctx) != "light" || LangOf(ctx) != "en" {
		t.Fatal("WithTheme should preserve lang")
	}
	if ThemeOf(nil) != "dark" || ThemeOf(WithTheme(context.Background(), "sepia")) != "dark" {
		t.Fatal("ThemeOf invalid/nil")
	}
}
