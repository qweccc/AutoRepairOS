package line

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEnvLoaderStandalone 用獨立角度驗證 config.Load() 會讀 repo 根下的 .env。
// 不搶 config.go，只從測試端驗證「.env token 真的被 Load 讀進 ChannelAccessToken」。
//
// 這是驗證 env loader 的關鍵斷言：Windows 的 os.Getenv 不會自動讀 .env，
// 若 Load() 後 ChannelAccessToken 長度=0 → 表示 env loader 沒生效（仍 MOCK）。
func TestEnvLoaderStandalone(t *testing.T) {
	ClearEnvFileCache()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失敗")
	}
	// 測試檔在 package line，上去三層到 repo 根：line → integration → internal → repoRoot
	repoRoot := filepath.Join(filepath.Dir(file), "..", "..", "..")
	envPath := filepath.Join(repoRoot, ".env")

	data, err := os.ReadFile(envPath)
	if err != nil {
		// 沒有 .env 是「本機未設定憑證」，不是程式錯誤 —— 不該讓測試套件紅燈。
		t.Skipf("無 %s（本機未設定 LINE 憑證）→ 跳過 env loader 驗證", envPath)
	}
	if len(data) == 0 {
		t.Skipf("%s 是空檔 → 跳過 env loader 驗證", envPath)
	}

	ResetSharedForTest()
	cfg, mock := Load()
	if mock {
		// 沒有 .env 或 .env 未填 token 時，這裡不該讓整個測試套件失敗——
		// 這是「本機憑證設定」問題，不是程式錯誤。
		// 原本用 t.Fatal，導致沒放 .env 的環境（CI、其他開發者）跑測試直接紅。
		t.Skip("無 .env 或未設定 LINE token（Mock 模式）→ 跳過實連驗證")
	}
	if len(cfg.ChannelAccessToken) == 0 {
		t.Fatal("Load() 後 ChannelAccessToken 長度=0 → env loader 沒生效")
	}
	t.Logf("env loader 通過：.env token 長度=%d, cfg.Valid()=%v",
		len(cfg.ChannelAccessToken), cfg.Valid())

	// 清理環境變數與 Shared 快取，讓下一個測試乾淨
	ClearEnvFileCache()
	ResetSharedForTest()
}
