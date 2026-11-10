package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"autorepairos/internal/i18n"
)

func langPost(form string, hx bool) (*httptest.ResponseRecorder, *http.Request) {
	r := httptest.NewRequest("POST", "/set-lang", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Referer", "/customers")
	if hx {
		r.Header.Set("HX-Request", "true")
	}
	return httptest.NewRecorder(), r
}

func cookieVal(rec *httptest.ResponseRecorder, name string) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestSetLangCookie(t *testing.T) {
	// en dictionary must be loaded for en to be a supported locale.
	if err := i18n.Load("../../locales"); err != nil {
		t.Fatalf("load locales: %v", err)
	}
	rec, r := langPost("lang=zh-TW", false)
	SetLang(rec, r)
	if rec.Code != 302 || cookieVal(rec, "lang") != "zh-TW" {
		t.Fatalf("code=%d cookie=%q", rec.Code, cookieVal(rec, "lang"))
	}
	for _, form := range []string{"lang=en", "locale=en-US", "lang=EN"} {
		rec, r := langPost(form, false)
		SetLang(rec, r)
		if rec.Code != 302 || cookieVal(rec, "lang") != "en" {
			t.Fatalf("%s: code=%d cookie=%q", form, rec.Code, cookieVal(rec, "lang"))
		}
	}
	rec, r = langPost("lang=en", true)
	SetLang(rec, r)
	if rec.Code != 204 || cookieVal(rec, "lang") != "en" {
		t.Fatalf("hx code=%d", rec.Code)
	}
	rec, r = langPost("lang=fr", false)
	SetLang(rec, r)
	if rec.Code != 400 {
		t.Fatalf("invalid code=%d", rec.Code)
	}
}

func TestSetThemeCookie(t *testing.T) {
	for _, tc := range []struct {
		form string
		code int
	}{
		{"theme=light", 204},
		{"theme=dark", 204},
		{"theme=sepia", 400},
		{"", 400},
	} {
		r := httptest.NewRequest("POST", "/set-theme", strings.NewReader(tc.form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		SetTheme(rec, r)
		if rec.Code != tc.code {
			t.Fatalf("%s: code=%d want %d", tc.form, rec.Code, tc.code)
		}
	}
	if got := ThemeOf(httptest.NewRequest("GET", "/", nil)); got != "dark" {
		t.Fatalf("default theme = %q", got)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "theme", Value: "light"})
	if got := ThemeOf(r); got != "light" {
		t.Fatalf("cookie theme = %q", got)
	}
	if got := ThemeOf(nil); got != "dark" {
		t.Fatalf("nil req theme = %q", got)
	}
}
