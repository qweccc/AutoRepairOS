package telegram

import (
	"strings"
	"testing"
)

func TestAppName(t *testing.T) {
	t.Setenv("APP_NAME", "")
	if got := appName(); got != "AutoRepair OS" {
		t.Errorf("appName default = %q", got)
	}
	t.Setenv("APP_NAME", "MyGarage")
	if got := appName(); got != "MyGarage" {
		t.Errorf("appName = %q", got)
	}
}

func TestInternalAlertFormat(t *testing.T) {
	a := InternalAlert{Title: "低庫存", Detail: "機油剩 2 瓶", Action: "補貨"}
	s := a.Format()
	if !strings.Contains(s, "<b>低庫存</b>") || !strings.Contains(s, "機油剩 2 瓶") || !strings.Contains(s, "補貨") {
		t.Errorf("Format = %q", s)
	}
	// 無 Detail/Action
	a2 := InternalAlert{Title: "t"}
	s2 := a2.Format()
	if s2 != "🔔 <b>t</b>" {
		t.Errorf("Format minimal = %q", s2)
	}
	// 有 Detail 無 Action
	a3 := InternalAlert{Title: "t", Detail: "d"}
	if got := a3.Format(); got != "🔔 <b>t</b>\nd" {
		t.Errorf("Format detail = %q", got)
	}
	// HTML 轉義
	a4 := InternalAlert{Title: "<x>&"}
	if !strings.Contains(a4.Format(), "&lt;x&gt;&amp;") {
		t.Errorf("escape = %q", a4.Format())
	}
}

func TestDailyDigestFormat(t *testing.T) {
	d := DailyDigest{Date: "2026-11-09", NewOrders: 5, PendingPay: 3, Revenue: 1234567, LowStock: 2, NegativeStok: 1, MaintDue: 4}
	s := d.Format()
	for _, want := range []string{"每日摘要", "2026-11-09", "5", "1,234,567", "低庫存", "負庫存", "保養到期"} {
		if !strings.Contains(s, want) {
			t.Errorf("缺 %q: %q", want, s)
		}
	}
	// 全部為 0 時不應出現警告行
	d2 := DailyDigest{Date: "2026-11-09"}
	s2 := d2.Format()
	if strings.Contains(s2, "低庫存") || strings.Contains(s2, "負庫存") || strings.Contains(s2, "保養到期") {
		t.Errorf("零值不應有警告行: %q", s2)
	}
	if !strings.Contains(s2, "NT$ 0") {
		t.Errorf("營收 0 格式: %q", s2)
	}
}

func TestEscapeHTML(t *testing.T) {
	if got := escapeHTML("a<b>&c"); got != "a&lt;b&gt;&amp;c" {
		t.Errorf("escapeHTML = %q", got)
	}
	if got := escapeHTML("plain"); got != "plain" {
		t.Errorf("escapeHTML plain = %q", got)
	}
}

func TestComma(t *testing.T) {
	cases := map[float64]string{
		0:       "0",
		5:       "5",
		999:     "999",
		1000:    "1,000",
		1234567: "1,234,567",
		-1234:   "-1,234",
		1234.6:  "1,235",
		100:     "100",
		1000000: "1,000,000",
	}
	for in, want := range cases {
		if got := comma(in); got != want {
			t.Errorf("comma(%v) = %q, want %q", in, got, want)
		}
	}
}
