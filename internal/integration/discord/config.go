// Package discord 提供 Discord bot 的外送通知與客戶私訊（DM）回覆能力。
//
// 設計原則（與 internal/integration/telegram 同構）：
//   - 憑證全部從環境變數／.env 讀取，不寫死；缺 token 時自動進入 Mock 模式
//   - Mock 模式只寫 log、不實際外送，讓開發與測試不需要真憑證
//   - 只記錄變數名稱與長度，絕不把 token 寫進 log
//
// Discord 的定位：客戶聊天 channel（客戶自助回覆走使用者 DM，
// 由 SendDirectMessage 先建 DM channel 再送訊息）＋內部通知 channel
// （固定 ChannelID，由 SendMessage 發送）。
//
// 出站訊息走 REST API v10；入站訊息走 Gateway WebSocket（純出站連線，
// 容器或 exe 都能直接跑，不需要任何對外公開網址——與 Telegram
// getUpdates 輪詢相同的離線區網優勢）。
package discord

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Config 為 Discord Bot 連線設定，全部從環境變數讀取。
type Config struct {
	// BotToken：Discord Developer Portal 發的 bot token。
	BotToken string
	// ChannelID：預設內部通知頻道。可為空，表示每則通知自行指定。
	ChannelID string
	// BaseURL 預設官方端點；可指向自架 proxy（測試用 httptest 也走這裡）。
	BaseURL string

	// Mock 為 true 時（無 token）不實際發送，只 logging。
	Mock bool
}

const envFile = ".env"

// repoEnvPaths 找出 .env 候選路徑：repo 根優先，其次 CWD。
func repoEnvPaths() []string {
	paths := []string{envFile}
	if _, file, _, _ := runtime.Caller(0); file != "" {
		repoRoot := filepath.Join(filepath.Dir(file), "..", "..", "..")
		paths = append([]string{filepath.Join(repoRoot, ".env")}, paths...)
	}
	return paths
}

// loadFromEnvFile 從 .env 補讀尚未設定的變數（Windows 的 os.Getenv 不會自動載入）。
// 只記錄 KEY 名稱，不記錄值。
func loadFromEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.Trim(line, "\"'")
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}

// Load 從環境變數與 .env 載入配置。缺 token 時進入 Mock 模式。
func Load() (*Config, bool) {
	for _, p := range repoEnvPaths() {
		if _, err := os.Stat(p); err == nil {
			loadFromEnvFile(p)
			break
		}
	}

	cfg := &Config{
		BotToken:  strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")),
		ChannelID: strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_ID")),
		BaseURL:   strings.TrimSpace(os.Getenv("DISCORD_BASE_URL")),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://discord.com"
	}
	cfg.Mock = cfg.BotToken == ""

	if cfg.Mock {
		fmt.Printf("[discord] 未設定 DISCORD_BOT_TOKEN，進入 MOCK 模式（只記錄不外送）\n")
	} else {
		fmt.Printf("[discord] 已載入設定：token 長度=%d, channel_id=%s\n",
			len(cfg.BotToken), maskChannel(cfg.ChannelID))
	}
	return cfg, !cfg.Mock
}

// Valid 憑證是否足以實際外送。
func (c *Config) Valid() bool { return c != nil && c.BotToken != "" }

// maskChannel 只顯示 channel id 的尾四碼，避免 log 洩漏。
func maskChannel(s string) string {
	if s == "" {
		return "(未設定)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

// ============================================================
// 共用實例
// ============================================================

var (
	sharedOnce   sync.Once
	sharedCfg    *Config
	sharedClient *Client
)

// Shared 取得共用的設定與客戶端（延遲載入，只讀一次環境變數）。
func Shared() (*Config, *Client) {
	sharedOnce.Do(func() {
		sharedCfg, _ = Load()
		sharedClient = NewClient(sharedCfg)
	})
	return sharedCfg, sharedClient
}

// ResetSharedForTest 清除共用快取，讓下一個呼叫重新讀取環境變數。
// 僅供測試使用。
func ResetSharedForTest() {
	sharedOnce = sync.Once{}
	sharedCfg = nil
	sharedClient = nil
}
