package cron

import (
	"os"
	"strings"
	"testing"

	"autorepairos/internal/config"
	"autorepairos/internal/db"
	"autorepairos/internal/integration/line"
)

// TestSchedulerLineDeliveryReal 用真實 .env 變數跑排程器外送，證明排程器能否真發 LINE。
func TestSchedulerLineDeliveryReal(t *testing.T) {
	if err := db.Init(config.Load()); err != nil {
		t.Fatalf("db.Init: %v", err)
	}
	defer db.Close()

	// 從 .env 讀取 LINE 變數設進 os 環境，模擬服務啟動。
	cfg, _ := os.ReadFile(".env")
	content := string(cfg)
	for _, key := range []string{"LINE_CHANNEL_ID", "LINE_CHANNEL_SECRET", "LINE_CHANNEL_TOKEN", "LINE_BASE_URL"} {
		if line := firstLine(content, key); line != "" {
			k := strings.SplitN(line, "=", 2)[0]
			os.Setenv(k, strings.TrimPrefix(line, k+"="))
		}
	}

	line.ResetSharedForTest()
	cfgLine, _ := line.Shared()
	t.Logf("排程器外送 cfg.Valid() = %v", cfgLine.Valid())

	if _, err := processPendingNotifications(); err != nil {
		t.Logf("processPendingNotifications 結果: %v", err)
	}
}

func firstLine(content, key string) string {
	for _, l := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), key+"=") {
			return l
		}
	}
	return ""
}
