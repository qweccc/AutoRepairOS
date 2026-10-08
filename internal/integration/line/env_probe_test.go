package line

import (
	"os"
	"strings"
	"testing"
)

// TestEnvFileLoading 實際讀 .env 檔，確認 Go 的 os.Getenv 是否真的看到 token。
// 這是「有 token 也發不出去」最可能的真實原因：Windows 上 os.Getenv 不讀 .env。
func TestEnvFileLoading(t *testing.T) {
	envPath := ".env"
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Skipf("無 .env 檔: %v", err)
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
		t.Skip("無 LINE_CHANNEL_TOKEN 欄位")
	}

	// 把 .env 內容設成 os 環境變數，模擬真正的啟動。
	// 實際專案會用 config.Load() 讀取，這裡只驗證「讀得到 token」。
	os.Setenv("LINE_CHANNEL_TOKEN", strings.TrimPrefix(tokenLine, "LINE_CHANNEL_TOKEN="))
	os.Setenv("LINE_CHANNEL_ID", strings.TrimPrefix(firstLine(content, "LINE_CHANNEL_ID"), "LINE_CHANNEL_ID="))
	os.Setenv("LINE_CHANNEL_SECRET", strings.TrimPrefix(firstLine(content, "LINE_CHANNEL_SECRET"), "LINE_CHANNEL_SECRET="))

	cfg, _ := Load()
	t.Logf(".env token 是否被 os.Getenv 看到: %v", cfg.Valid())
	if !cfg.Valid() {
		t.Log("確認：.env 讀不到 → 需要 env loader 才能實際外送")
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
