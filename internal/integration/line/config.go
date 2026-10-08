package line

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// envFile 預設 repo 根下的 .env（Cloudflare Tunnel 通常把 .env 放根目錄）。
const envFile = ".env"

// repoEnvPaths 列出要找的 .env 候選路徑：repo 根（runtime.Caller 算出）優先，
// 其次 CWD。這樣不管服務啟動時 CWD 在哪都能讀到 repo 根下的 .env。
func repoEnvPaths() []string {
	paths := []string{envFile}
	if _, file, _, _ := runtime.Caller(0); file != "" {
		repoRoot := filepath.Join(filepath.Dir(file), "..", "..", "..")
		paths = append([]string{filepath.Join(repoRoot, ".env")}, paths...)
	}
	return paths
}

// loadFromEnvFile 從 .env 讀取尚未在 os.Getenv 的變數。
// Windows 的 os.Getenv 不會自動載入 .env，所以這裡補一個 dotenv 式的讀取：
// 只取 KEY=VALUE 或 KEY="VALUE"/KEY='VALUE'，跳過註解、空白行、# 開頭的 comment。
// 只補尚未設的變數（若已透過環境變數設定則保留），避免測試時把 .env 的值蓋掉。
// 只記錄 KEY 名稱與長度，绝不把 token 寫進 log。
func loadFromEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // 沒有 .env 就不管
	}
	lines := strings.Split(string(data), "\n")
	for _, raw := range lines {
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
		// 用 LookupEnv 而非 Getenv()=="": 兩者差在「明確設成空字串」。
		// 測試會 os.Setenv(SECRET, "") 模擬未設定憑證，若用 Getenv 判空，
		// .env 的真實 secret 會被補進來，讓「未設定 secret 應回 503」測不出來。
		if _, present := os.LookupEnv(key); !present {
			_ = os.Setenv(key, val)
		}
	}
}

// Load 從環境變數與 .env 檔載入配置。缺 token 時自動進入 Mock 模式。
func Load() (*Config, bool) {
	// 先讀 .env（若存在），Windows 的 os.Getenv 不會自動載入 .env。
	// 按 repo 根與 CWD 的候選路徑逐一嘗試，找到即停。
	for _, p := range repoEnvPaths() {
		if _, err := os.Stat(p); err == nil {
			loadFromEnvFile(p)
			break
		}
	}

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

// Missing 列出缺少的環境變數名稱（不含值），供 UI 顯示設定指引。
func (c *Config) Missing() []string {
	var out []string
	if c.ChannelID == "" {
		out = append(out, "LINE_CHANNEL_ID")
	}
	if c.ChannelSecret == "" {
		out = append(out, "LINE_CHANNEL_SECRET")
	}
	if c.ChannelAccessToken == "" {
		out = append(out, "LINE_CHANNEL_TOKEN")
	}
	return out
}

// --- 共享 client ---

var (
	sharedOnce   sync.Once
	sharedCfg    *Config
	sharedClient *Client
)

// Shared 回傳全域共用的 Config 與 Client。
// 排程器與 HTTP handler 都用同一份，避免重複建立與連線。
func Shared() (*Config, *Client) {
	sharedOnce.Do(func() {
		cfg, _ := Load()
		sharedCfg = cfg
		sharedClient = NewClient(cfg)
	})
	return sharedCfg, sharedClient
}

// ResetSharedForTest 清除 Shared 的快取，讓下一個呼叫重新讀取環境變數。
// 僅供測試使用：同一個行程內要模擬不同憑證組合時必須能重載。
// 正式環境不應呼叫——設定在啟動時讀一次即可，改用環境變數即時切換語意。
func ResetSharedForTest() {
	sharedOnce = sync.Once{}
	sharedCfg = nil
	sharedClient = nil
}

// ClearEnvFileCache 測試用：清除 os.Setenv 對 LINE_CHANNEL_* 的覆蓋，
// 讓 Load 下次重新從 .env 重讀。
func ClearEnvFileCache() {
	for _, k := range []string{"LINE_CHANNEL_ID", "LINE_CHANNEL_SECRET", "LINE_CHANNEL_TOKEN", "LINE_BASE_URL"} {
		os.Unsetenv(k)
	}
}

// StatusLine 產生給 UI 顯示的一行狀態說明，不洩漏憑證內容。
func (c *Config) StatusLine() string {
	if c.Mock {
		missing := c.Missing()
		if len(missing) > 0 {
			return "MOCK 模式（未設定：" + strings.Join(missing, ", ") + "）— 通知只記錄不外送"
		}
		return "MOCK 模式 — 通知只記錄不外送"
	}
	if !c.Valid() {
		return "憑證不完整（缺少：" + strings.Join(c.Missing(), ", ") + "）— 通知會失敗"
	}
	return "已連線（Channel " + c.ChannelID + "）"
}

// Ready 表示可以實際外送。
func (c *Config) Ready() bool { return !c.Mock && c.Valid() }
