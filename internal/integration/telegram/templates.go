package telegram

import (
	"fmt"
	"os"
	"strings"
)

func appName() string {
	if v := os.Getenv("APP_NAME"); v != "" {
		return v
	}
	return "AutoRepair OS"
}

// 內部通知的訊息格式。
//
// 為什麼要獨立一組模板：內部通知（給老闆/員工看）與客戶通知（給車主看）
// 的語氣與內容完全不同 —— 客戶看的是「您的車該保養了」，
// 老闆看的是「今天 3 張完工待結帳」。混用會讓兩邊都彆扭。

// InternalAlert 系統告警（給老闆）。
type InternalAlert struct {
	Title  string
	Detail string
	Action string // 建議動作，可空
}

// Format 產生告警訊息（Telegram HTML 格式）。
func (a InternalAlert) Format() string {
	var b strings.Builder
	b.WriteString("🔔 <b>" + escapeHTML(a.Title) + "</b>\n")
	if a.Detail != "" {
		b.WriteString(escapeHTML(a.Detail) + "\n")
	}
	if a.Action != "" {
		b.WriteString("\n👉 " + escapeHTML(a.Action))
	}
	return strings.TrimRight(b.String(), "\n")
}

// DailyDigest 每日營運摘要（給老闆）。
type DailyDigest struct {
	Date         string
	NewOrders    int
	PendingPay   int     // 待結帳
	Revenue      float64 // 今日營收
	LowStock     int     // 低庫存件數
	NegativeStok int     // 負庫存件數（舊資料待補正）
	MaintDue     int     // 保養到期台數
}

// Format 產生摘要訊息。
func (d DailyDigest) Format() string {
	var b strings.Builder
	b.WriteString("📊 <b>" + appName() + " 每日摘要</b>\n")
	b.WriteString("<code>" + escapeHTML(d.Date) + "</code>\n\n")
	b.WriteString(fmt.Sprintf("新工單　　<b>%d</b> 張\n", d.NewOrders))
	b.WriteString(fmt.Sprintf("待結帳　　<b>%d</b> 張\n", d.PendingPay))
	b.WriteString(fmt.Sprintf("今日營收　<b>NT$ %s</b>\n", comma(d.Revenue)))
	if d.LowStock > 0 {
		b.WriteString(fmt.Sprintf("\n⚠️ 低庫存　<b>%d</b> 件\n", d.LowStock))
	}
	if d.NegativeStok > 0 {
		b.WriteString(fmt.Sprintf("⚠️ 負庫存　<b>%d</b> 筆待補正\n", d.NegativeStok))
	}
	if d.MaintDue > 0 {
		b.WriteString(fmt.Sprintf("🔧 保養到期 <b>%d</b> 台\n", d.MaintDue))
	}
	return b.String()
}

// escapeHTML Telegram HTML parse_mode 只認 <b> <i> <code> <a> 等少數標籤，
// 內文若含 & < > 必須轉義，否則 API 會回 400。
func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// comma 千分位（金額可讀性）。
func comma(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	n := int64(v + 0.5)
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
