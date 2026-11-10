package components

import (
	"context"
	"strings"
	"testing"

	"autorepairos/internal/i18n"
	"autorepairos/internal/model"
)

func TestLayoutLangAndTheme(t *testing.T) {
	// default (no middleware): zh-TW + dark
	var b strings.Builder
	if err := Layout("T", "home", model.User{Username: "u", Role: "owner"}, "App").
		Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if !strings.Contains(s, `lang="zh-TW"`) || !strings.Contains(s, `data-theme="dark"`) {
		t.Fatalf("default html attrs missing: %.200s", s)
	}
	if !strings.Contains(s, `id="langSelect"`) || !strings.Contains(s, `id="themeSelect"`) || !strings.Contains(s, "/static/prefs.js") {
		t.Fatal("switcher UI missing")
	}
	// en + light via ctx
	ctx := i18n.WithTheme(i18n.WithLang(context.Background(), "en"), "light")
	b.Reset()
	if err := Layout("T", "home", model.User{Username: "u"}, "App").Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	s = b.String()
	if !strings.Contains(s, `lang="en"`) || !strings.Contains(s, `data-theme="light"`) {
		t.Fatalf("en/light html attrs missing: %.200s", s)
	}
	// the theme dropdown must show the current theme as selected
	if !strings.Contains(s, `value="light" selected`) {
		t.Fatalf("theme dropdown should mark light selected: %.300s", s)
	}
}
