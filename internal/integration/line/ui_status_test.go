package line

import "testing"

// TestUIStatusDisplay 驗證 UI 狀態顯示：有 token 應顯示「已連線」而非 MOCK。
// 這代表 env loader 生效後，服務啟動時 UI 不會誤導性顯示 MOCK。
func TestUIStatusDisplay(t *testing.T) {
	ClearEnvFileCache()
	ResetSharedForTest()
	cfg, _ := Shared()
	if cfg.Mock {
		// 無 .env 或未填 token 的環境（CI、其他開發者）不該紅燈。
		t.Skip("無 .env 或未設定 LINE token（Mock 模式）→ 跳過 UI 狀態驗證")
	}
	if !cfg.Ready() {
		t.Fatal("Ready()=false → UI 會顯示「通知會失敗」")
	}
	status := cfg.StatusLine()
	if status == "MOCK 模式" || status == "憑證不完整" {
		t.Fatalf("UI StatusLine 誤導：%q", status)
	}
	missing := cfg.Missing()
	if len(missing) > 0 {
		t.Fatalf("UI 會列出未設憑證：%v", missing)
	}
	t.Logf("UI 狀態顯示正確：%q", status)

	ClearEnvFileCache()
	ResetSharedForTest()
}
