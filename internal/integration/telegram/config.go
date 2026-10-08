// Package telegram 提供 Telegram Bot API 的通知外送能力。
//
// 設計原則（與 internal/integration/line 同構）：
//   - 憑證全部從環境變數／.env 讀取，不寫死；缺 token 時自動進入 Mock 模式
//   - Mock 模式只寫 log、不實際外送，讓開發與測試不需要真憑證
//   - 只記錄變數名稱與長度，绝不把 token 寫進 log
//
// 為什麼預設走 Telegram：設定成本最低（@BotFather 對話 2 分鐘拿 token，
// 不需商業驗證、不需專用電話號碼、不需訊息範本審核），且免費無則數上限。
// 適合「內部通知」——系統告警、每日營收、待辦提醒給老闆與員工看。
//
// 客戶通知仍以 LINE 為主（台灣車行客戶 LINE 覆蓋率 >90%），
// 兩者共用 notifications 表的 channel 欄位分流。
package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Config 為 Telegram Bot 連線設定，全部從環境變數讀取。
type Config struct {
	// BotToken：@BotFather 發的 token，格式 <bot_id>:<hash>。
	BotToken string
	// ChatID：預設收件人（老闆的 chat id）。可為空，表示每則通知自行指定。
	ChatID string
	// BaseURL 預設官方端點；可指向自架 proxy。
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
		BotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		ChatID:   strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")),
		BaseURL:  strings.TrimSpace(os.Getenv("TELEGRAM_BASE_URL")),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.telegram.org"
	}
	cfg.Mock = cfg.BotToken == ""

	if cfg.Mock {
		fmt.Printf("[telegram] 未設定 TELEGRAM_BOT_TOKEN，進入 MOCK 模式（只記錄不外送）\n")
	} else {
		fmt.Printf("[telegram] 已載入設定：token 長度=%d, chat_id=%s\n",
			len(cfg.BotToken), maskChat(cfg.ChatID))
	}
	return cfg, !cfg.Mock
}

// Valid 憑證是否足以實際外送。
func (c *Config) Valid() bool { return c != nil && c.BotToken != "" }

// maskChat 只顯示 chat id 的尾四碼，避免 log 洩漏。
func maskChat(s string) string {
	if s == "" {
		return "(未設定)"
	}
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

// Client Telegram Bot API 客戶端。
type Client struct {
	cfg *Config
	hc  *http.Client
}

// NewClient 建立客戶端。
func NewClient(cfg *Config) *Client {
	return &Client{
		cfg: cfg,
		hc:  &http.Client{Timeout: 15 * time.Second},
	}
}

// SendMessage 送出純文字訊息到指定 chat。
//
// chatID 為空時用設定中的預設 ChatID（內部通知的常見情境：
// 全部告警都送到老闆的對話）。
func (c *Client) SendMessage(chatID, text string) error {
	if c.cfg == nil || c.cfg.Mock {
		return mockLog(chatID, text)
	}
	if chatID == "" {
		chatID = c.cfg.ChatID
	}
	if chatID == "" {
		return fmt.Errorf("未指定 chat id，且未設定 TELEGRAM_CHAT_ID")
	}

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
		// 關閉連結預覽，通知訊息不需要預覽卡片
		"link_preview_options": map[string]interface{}{"is_disabled": true},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/bot%s/sendMessage", strings.TrimRight(c.cfg.BaseURL, "/"), c.cfg.BotToken)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("telegram 連線失敗: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram 回應 %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	// Telegram 的錯誤是 HTTP 200 + ok:false
	var result struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil && !result.OK {
		return fmt.Errorf("telegram 拒絕: %s", result.Description)
	}
	return nil
}

// mockLog Mock 模式：只寫 log，不實際外送。
func mockLog(chatID, text string) error {
	if chatID == "" {
		chatID = "(預設)"
	}
	fmt.Printf("[telegram][MOCK] to=%s len=%d text=%s\n", chatID, len(text), truncate(text, 120))
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
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
