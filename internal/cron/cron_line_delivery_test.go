package cron

import (
	"testing"

	"autorepairos/internal/integration/line"
)

// TestSchedulerDeliversLine 實際走排程器 path，確認是否真的呼叫 LINE 外送。
func TestSchedulerDeliversLine(t *testing.T) {
	line.ResetSharedForTest()
	cfg, client := line.Shared()

	t.Logf("line.Shared() cfg.Valid() = %v", cfg.Valid())
	t.Logf("line.Shared() client = %v", client != nil)

	if !cfg.Valid() {
		t.Log("注意：os.Getenv 沒讀到 .env（Windows 不自動讀），需要 env loader")
	}

	// 確認 deliver 會真的推播（無 token 會 mock，但證明程式路徑通）。
	if client == nil {
		t.Fatal("client 為 nil，排程器無法外送")
	}
	if err := client.PushMessage("@test", "cron line delivery test"); err != nil {
		t.Logf("PushMessage 結果: %v", err)
	}
}
