package line

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLineEnvLoaderActual 用 runtime.Caller 找 repo 根，實際從 .env 讀 token，
// 驗證 env loader 是否真的生效。這是決定性測試。
func TestLineEnvLoaderActual(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..", "..")

	data, err := os.ReadFile(filepath.Join(repoRoot, ".env"))
	if err != nil {
		t.Skipf("無 repo 根 .env: %v", err)
	}
	content := string(data)

	tokenLine := ""
	for _, l := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "LINE_CHANNEL_TOKEN=") {
			tokenLine = strings.TrimSpace(l)
			break
		}
	}
	if tokenLine == "" {
		t.Skip("無 LINE_CHANNEL_TOKEN")
	}
	token := strings.TrimPrefix(tokenLine, "LINE_CHANNEL_TOKEN=")
	t.Logf("從 .env 讀到的 token 長度: %d", len(token))

	// 實際跑 config.Load，看能否讀到 token（模擬服務啟動）
	cfg, _ := Load()
	t.Logf("Load() 後 ChannelAccessToken 長度: %d, Valid(): %v", len(cfg.ChannelAccessToken), cfg.Valid())
	if len(cfg.ChannelAccessToken) == 0 {
		t.Error("確認：.env token 沒被 Load() 讀到 → 仍缺 env loader")
		return
	}

	// 再實際發一次，證明不只是讀到，還能連出去
	ResetSharedForTest()
	cfg2, client := Shared()
	if cfg2.Valid() {
		t.Logf("Shared() cfg.Valid() = %v → LINE 外送路徑通", cfg2.Valid())
	}
	if err := client.PushMessage("@test", "env loader probe"); err == nil {
		t.Logf("PushMessage 成功 → LINE 外送真的連出去了")
	} else {
		t.Logf("PushMessage 結果（無 token 會 mock，有 token 會回 LINE 狀態碼）: %v", err)
	}
}
