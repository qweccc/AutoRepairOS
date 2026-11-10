// Package i18n provides lightweight locale loading and per-request
// translation lookup. Dictionaries are flat JSON maps ("workorder.title":
// "工單管理") loaded from a locales directory at startup.
package i18n

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	DefaultLang = "zh-TW"
	CookieName  = "lang"
)

var (
	mu    sync.RWMutex
	dicts = map[string]map[string]string{}
)

type ctxKey struct{}

type prefs struct {
	lang  string
	theme string
}

// Load reads every *.json file in dir (file basename = locale code, e.g.
// zh-TW.json -> "zh-TW") into memory. Missing files for a locale mean that
// locale falls back to the default dictionary at lookup time.
func Load(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read locales dir %s: %w", dir, err)
	}
	loaded := map[string]map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		loaded[lang] = m
	}
	if len(loaded) == 0 {
		return fmt.Errorf("no locale files in %s", dir)
	}
	mu.Lock()
	dicts = loaded
	mu.Unlock()
	return nil
}

// Supported reports whether lang has a dictionary loaded.
func Supported(lang string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := dicts[lang]
	return ok
}

// Normalize maps an Accept-Language / cookie value to a supported locale:
// exact match first (case-insensitive), then primary subtag ("en-US" -> "en").
func Normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	// The default locale is always valid, even before dictionaries load
	// (e.g. in handler unit tests that never call Load).
	if strings.EqualFold(value, DefaultLang) {
		return DefaultLang
	}
	mu.RLock()
	defer mu.RUnlock()
	for lang := range dicts {
		if strings.EqualFold(lang, value) {
			return lang
		}
	}
	if i := strings.IndexByte(value, '-'); i > 0 {
		primary := value[:i]
		for lang := range dicts {
			if strings.EqualFold(lang, primary) {
				return lang
			}
		}
	}
	return ""
}

// LangOf returns the locale stored in ctx (DefaultLang when absent).
func LangOf(ctx context.Context) string {
	if ctx == nil {
		return DefaultLang
	}
	if p, ok := ctx.Value(ctxKey{}).(prefs); ok && p.lang != "" {
		return p.lang
	}
	return DefaultLang
}

// ThemeOf returns the UI theme stored in ctx ("dark" default — the
// historic UI theme, so existing users see no change).
func ThemeOf(ctx context.Context) string {
	if ctx == nil {
		return "dark"
	}
	if p, ok := ctx.Value(ctxKey{}).(prefs); ok && (p.theme == "light" || p.theme == "dark") {
		return p.theme
	}
	return "dark"
}

// WithLang returns a context carrying lang.
func WithLang(ctx context.Context, lang string) context.Context {
	p := prefs{lang: lang}
	if old, ok := ctx.Value(ctxKey{}).(prefs); ok {
		p.theme = old.theme
	}
	return context.WithValue(ctx, ctxKey{}, p)
}

// WithTheme returns a context carrying theme.
func WithTheme(ctx context.Context, theme string) context.Context {
	p := prefs{theme: theme}
	if old, ok := ctx.Value(ctxKey{}).(prefs); ok {
		p.lang = old.lang
	}
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromRequest resolves the locale for r: "lang" cookie, then
// Accept-Language, then DefaultLang.
func FromRequest(r *http.Request) string {
	if r == nil {
		return DefaultLang
	}
	if c, err := r.Cookie(CookieName); err == nil {
		if lang := Normalize(c.Value); lang != "" {
			return lang
		}
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if lang := Normalize(tag); lang != "" {
			return lang
		}
	}
	return DefaultLang
}

// T returns the translation of key for the locale in ctx. Missing keys fall
// back to the default dictionary; still-missing keys return the key itself.
func T(ctx context.Context, key string) string {
	lang := LangOf(ctx)
	mu.RLock()
	defer mu.RUnlock()
	if v, ok := dicts[lang][key]; ok {
		return v
	}
	if v, ok := dicts[DefaultLang][key]; ok {
		return v
	}
	return key
}

// ThemeFromRequest resolves the UI theme for r: "theme" cookie, else "dark".
func ThemeFromRequest(r *http.Request) string {
	if r == nil {
		return "dark"
	}
	if c, err := r.Cookie("theme"); err == nil && (c.Value == "light" || c.Value == "dark") {
		return c.Value
	}
	return "dark"
}

// Middleware injects the request locale + theme into the context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithTheme(WithLang(r.Context(), FromRequest(r)), ThemeFromRequest(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
