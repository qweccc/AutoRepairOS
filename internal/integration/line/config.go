package line

import (
	"fmt"
	"log"
	"os"
	"strings"
)

// Config 為 LINE Messaging API 連線設定，全部從環境變數讀取，不寫死。
type Config struct {
	// ChannelID：識別頻道，驗簽 webhook 用。
	ChannelID string
	// ChannelSecret：驗簽 webhook 用。
	ChannelSecret string
	// ChannelAccessToken：主動推播/收訊用的 access token（long-lived）。
	ChannelAccessToken string

	// BaseURL 預設官方端點；無公開 IP 時可指向內網穿透（Cloudflare Tunnel）公開網址。
	BaseURL string

	// Mock 為 true 時（無 token）不實際發送，只 logging，方便開發測試。
	Mock bool
}

// Load 從環境變數載入配置。缺 token 時自動進入 Mock 模式。
func Load() (*Config, bool) {
	id := strings.TrimSpace(os.Getenv("LINE_CHANNEL_ID"))
	secret := strings.TrimSpace(os.Getenv("LINE_CHANNEL_SECRET"))
	token := strings.TrimSpace(os.Getenv("LINE_CHANNEL_TOKEN"))

	cfg := &Config{
		ChannelID:          id,
		ChannelSecret:      secret,
		ChannelAccessToken: token,
		BaseURL:            strings.TrimSpace(os.Getenv("LINE_BASE_URL")),
	}

	if token == "" {
		cfg.Mock = true
		log.Println("[line] 未設定 LINE_CHANNEL_TOKEN，進入 MOCK 模式（只 logging，不發送）")
	}

	if cfg.BaseURL == "" {
		// 官方端點：推播 https://api.line.me/version2/bot/message/push
		cfg.BaseURL = "https://api.line.me"
	}

	if cfg.Mock {
		log.Printf("[line] MOCK: ChannelID=%q Secret=%q 有效長度=%d/%d",
			cfg.ChannelID, cfg.ChannelSecret,
			len(cfg.ChannelID), len(secret))
	}
	return cfg, cfg.Mock
}

// Valid 判斷是否有足夠的憑證進行實際發送。
func (c *Config) Valid() bool {
	return c.ChannelAccessToken != "" && c.ChannelID != "" && c.ChannelSecret != ""
}

// ErrUnconfigured 回傳未配置時的錯誤。
func ErrUnconfigured() error {
	return fmt.Errorf("LINE integration not configured: set LINE_CHANNEL_ID, LINE_CHANNEL_SECRET, LINE_CHANNEL_TOKEN")
}
