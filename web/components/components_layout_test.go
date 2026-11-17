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

// TestLayoutHTMXErrorHandlerIsRegistered 守住「HTMX 失敗時要能在表單上顯示訊息」
// 這條路。這個 listener 以前寫成 document.body.addEventListener，但 script 位於
// <head>、body 還沒被剖析，於是每一頁載入都丟 TypeError，handler 從來沒裝上去
// （失敗訊息只進 console，使用者什麼都沒看到）。監聽對象必須是 head 時期就存在
// 的 document；這條測試防止它再被改回 body。
func TestLayoutHTMXErrorHandlerIsRegistered(t *testing.T) {
	var b strings.Builder
	if err := Layout("T", "home", model.User{Username: "u", Role: "owner"}, "App").
		Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if strings.Contains(s, "document.body.addEventListener('htmx:responseError'") {
		t.Fatal("htmx:responseError 不可掛在 document.body：script 在 <head>，那一刻 body 還不存在")
	}
	if !strings.Contains(s, "document.addEventListener('htmx:responseError'") {
		t.Fatal("缺少 htmx:responseError 監聽，HTMX 失敗時將無法顯示錯誤訊息")
	}
}
