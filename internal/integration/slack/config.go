// Package slack 提供 Slack 的客戶對話與內部通知能力（Socket Mode，純出站）。
//
// 設計原則（與 internal/integration/telegram 同構）：
//   - 憑證全部從環境變數／.env 讀取，不寫死；缺 token 時自動進入 Mock 模式
//   - Mock 模式只寫 log、不實際外送，讓開發與測試不需要真憑證
//   - 只記錄變數名稱與長度，絕不把 token 寫進 log
//
// 為什麼用 Socket Mode：
// 車行系統離線區網優先，沒有對外公開網址。Socket Mode 由本機主動以
// WebSocket 連到 Slack（apps.connections.open 取得 wss URL），不需要
// webhook／public endpoint，容器或 exe 都能直接跑。
// 一個 App 同時供「出站通知」（chat.postMessage，用 xoxb- bot token）
// 與「入站客戶訊息」（Socket Mode，用 xapp- app token）使用。
package slack

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Config 為 Slack 連線設定，全部從環境變數讀取。
type Config struct {
	// BotToken：Bot User OAuth Token，格式 xoxb-...，用於 chat.postMessage。
	BotToken string
	// AppToken：App-Level Token，格式 xapp-...（需 connections:write scope），
	// 用於 Socket Mode 的 apps.connections.open。
	AppToken string
	// ChannelID：預設通知頻道（內部告警用）。可為空，表示每則訊息自行指定。
	ChannelID string
	// BaseURL 預設官方端點；測試或自架 proxy 可覆寫。
	BaseURL string

	// Mock 為 true 時（無 bot token）不實際發送，只 logging。
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

// Load 從環境變數與 .env 載入配置。缺 bot token 時進入 Mock 模式。
func Load() (*Config, bool) {
	for _, p := range repoEnvPaths() {
		if _, err := os.Stat(p); err == nil {
			loadFromEnvFile(p)
			break
		}
	}

	cfg := &Config{
		BotToken:  strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN")),
		AppToken:  strings.TrimSpace(os.Getenv("SLACK_APP_TOKEN")),
		ChannelID: strings.TrimSpace(os.Getenv("SLACK_CHANNEL_ID")),
		BaseURL:   strings.TrimSpace(os.Getenv("SLACK_BASE_URL")),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://slack.com"
	}
	cfg.Mock = cfg.BotToken == ""

	if cfg.Mock {
		fmt.Printf("[slack] 未設定 SLACK_BOT_TOKEN，進入 MOCK 模式（只記錄不外送）\n")
	} else {
		fmt.Printf("[slack] 已載入設定：bot_token 長度=%d, app_token 長度=%d, channel_id=%s\n",
			len(cfg.BotToken), len(cfg.AppToken), maskChannel(cfg.ChannelID))
	}
	return cfg, !cfg.Mock
}

// Valid 憑證是否足以實際外送訊息（chat.postMessage 只需 bot token）。
func (c *Config) Valid() bool { return c != nil && c.BotToken != "" }

// SocketReady 是否足以啟動 Socket Mode 收入站訊息（需 bot + app token）。
func (c *Config) SocketReady() bool { return c != nil && c.BotToken != "" && c.AppToken != "" }

// maskChannel 只顯示 channel id 的尾四碼，避免 log 洩漏完整設定。
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
