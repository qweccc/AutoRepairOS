package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWithToken(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv("TELEGRAM_CHAT_ID", "999")
	t.Setenv("TELEGRAM_BASE_URL", "http://custom:9000")
	cfg, ok := Load()
	if !ok || cfg.Mock {
		t.Error("有 token 應非 Mock")
	}
	if cfg.BotToken != "123:abc" || cfg.ChatID != "999" || cfg.BaseURL != "http://custom:9000" {
		t.Errorf("cfg = %+v", cfg)
	}
	if !cfg.Valid() {
		t.Error("應 Valid")
	}
}

func TestLoadDefaultBaseURL(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:abc")
	t.Setenv("TELEGRAM_BASE_URL", "")
	cfg, _ := Load()
	// 注意：.env 若有 TELEGRAM_BASE_URL 會被載入；只斷言非空且 Valid
	if cfg.BaseURL == "" {
		t.Error("BaseURL 不應為空")
	}
	if !cfg.Valid() {
		t.Error("應 Valid")
	}
}

func TestValidNil(t *testing.T) {
	var c *Config
	if c.Valid() {
		t.Error("nil Config 不應 Valid")
	}
	if (&Config{}).Valid() {
		t.Error("空 Config 不應 Valid")
	}
	if (&Config{BotToken: "x"}).Valid() != true {
		t.Error("有 token 應 Valid")
	}
}

func TestMaskChat(t *testing.T) {
	cases := map[string]string{
		"":         "(未設定)",
		"ab":       "****",
		"1234":     "****",
		"12345678": "****5678",
	}
	for in, want := range cases {
		if got := maskChat(in); got != want {
			t.Errorf("maskChat(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepoEnvPaths(t *testing.T) {
	paths := repoEnvPaths()
	if len(paths) < 1 {
		t.Fatal("應至少有 .env")
	}
	found := false
	for _, p := range paths {
		if filepath.Base(p) == ".env" {
			found = true
		}
	}
	if !found {
		t.Errorf("paths = %v", paths)
	}
}

func TestLoadFromEnvFile(t *testing.T) {
	loadFromEnvFile(filepath.Join(t.TempDir(), "nope.env"))
	f := filepath.Join(t.TempDir(), "t.env")
	content := "# c\n\nTOK=fromfile\nQUOTED=\"q v\"\nNOEQ\n"
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TOK", "existing") // 已有值不被蓋
	loadFromEnvFile(f)
	if os.Getenv("TOK") != "existing" {
		t.Errorf("TOK 被蓋: %q", os.Getenv("TOK"))
	}
	if os.Getenv("QUOTED") != `"q v` {
		t.Errorf("QUOTED = %q", os.Getenv("QUOTED"))
	}
}

func TestSharedAndReset(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok-shared")
	ResetSharedForTest()
	t.Cleanup(ResetSharedForTest)
	cfg1, cli1 := Shared()
	cfg2, cli2 := Shared()
	if cfg1 != cfg2 || cli1 != cli2 {
		t.Error("Shared 應回同一份")
	}
	if cfg1.BotToken != "tok-shared" {
		t.Errorf("cfg = %+v", cfg1)
	}
	ResetSharedForTest()
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok-2")
	cfg3, _ := Shared()
	if cfg3.BotToken != "tok-2" {
		t.Errorf("Reset 後應重讀: %+v", cfg3)
	}
}

// ---------- SendMessage ----------

func tgCfg(base string) *Config {
	return &Config{BotToken: "123:abc", ChatID: "42", BaseURL: base}
}

func TestSendMessageMockAndNil(t *testing.T) {
	c := NewClient(&Config{Mock: true})
	if err := c.SendMessage("1", "hi"); err != nil {
		t.Errorf("mock 應為 nil: %v", err)
	}
	c2 := NewClient(nil)
	if err := c2.SendMessage("", "hi"); err != nil {
		t.Errorf("nil cfg mock 應為 nil: %v", err)
	}
}

func TestSendMessageOKDefaultChat(t *testing.T) {
	var gotChat, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var v map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&v)
		gotChat, _ = v["chat_id"].(string)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()
	c := NewClient(tgCfg(srv.URL))
	if err := c.SendMessage("", "hello"); err != nil {
		t.Fatal(err)
	}
	if gotChat != "42" {
		t.Errorf("chat_id = %q, want 預設 42", gotChat)
	}
	if gotPath != "/bot123:abc/sendMessage" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestSendMessageNoChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(&Config{BotToken: "t", BaseURL: srv.URL})
	if err := c.SendMessage("", "x"); err == nil {
		t.Fatal("無 chat id 應回錯誤")
	}
}

func TestSendMessageHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()
	c := NewClient(tgCfg(srv.URL))
	if err := c.SendMessage("1", "x"); err == nil {
		t.Fatal("非 200 應回錯誤")
	}
}

func TestSendMessageOKFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()
	c := NewClient(tgCfg(srv.URL))
	err := c.SendMessage("1", "x")
	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("err = %v", err)
	}
}

func TestSendMessageBadJSONBody(t *testing.T) {
	// HTTP 200 但 body 不是 JSON → 不視為錯誤（ok 檢查略過）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c := NewClient(tgCfg(srv.URL))
	if err := c.SendMessage("1", "x"); err != nil {
		t.Errorf("非 JSON body 不應有錯: %v", err)
	}
}

func TestSendMessageConnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient(tgCfg(url))
	if err := c.SendMessage("1", "x"); err == nil {
		t.Fatal("連線失敗應回錯誤")
	}
}

func TestSendMessageBadURL(t *testing.T) {
	c := NewClient(&Config{BotToken: "t", ChatID: "1", BaseURL: "http://exa mple.com"})
	if err := c.SendMessage("1", "x"); err == nil {
		t.Fatal("非法 URL 應回錯誤")
	}
}

func TestMockLogAndTruncate(t *testing.T) {
	if err := mockLog("", "hello"); err != nil {
		t.Error(err)
	}
	if err := mockLog("1", strings.Repeat("x", 300)); err != nil {
		t.Error(err)
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("你好世界你好", 3); got != "你好世..." {
		t.Errorf("truncate rune = %q", got)
	}
	if got := truncate("abcde", 5); got != "abcde" {
		t.Errorf("truncate exact = %q", got)
	}
}

// ---------- GetUpdates ----------

func TestGetUpdatesMock(t *testing.T) {
	c := NewClient(&Config{Mock: true})
	ups, err := c.GetUpdates(context.Background(), 0, 0)
	if err != nil || ups != nil {
		t.Errorf("mock 應回 nil,nil: %v %v", ups, err)
	}
	c2 := NewClient(nil)
	if _, err := c2.GetUpdates(context.Background(), 0, 0); err != nil {
		t.Errorf("nil cfg 應回 nil: %v", err)
	}
}

func TestGetUpdatesOK(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"message_id":1,"text":"hi","from":{"id":5,"is_bot":false,"username":"u"},"chat":{"id":9,"type":"private"}}}]}`))
	}))
	defer srv.Close()
	c := NewClient(tgCfg(srv.URL))
	ups, err := c.GetUpdates(context.Background(), 10, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(ups) != 1 || ups[0].UpdateID != 7 || ups[0].Message.Text != "hi" || ups[0].Message.From.ID != 5 || ups[0].Message.Chat.ID != 9 {
		t.Errorf("ups = %+v", ups)
	}
	if gotQuery != "offset=10&timeout=30" {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestGetUpdatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("oops"))
	}))
	defer srv.Close()
	c := NewClient(tgCfg(srv.URL))
	if _, err := c.GetUpdates(context.Background(), 0, 0); err == nil {
		t.Fatal("非 200 應回錯誤")
	}
}

func TestGetUpdatesOKFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"conflict"}`))
	}))
	defer srv.Close()
	c := NewClient(tgCfg(srv.URL))
	if _, err := c.GetUpdates(context.Background(), 0, 0); err == nil {
		t.Fatal("ok:false 應回錯誤")
	}
}

func TestGetUpdatesBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()
	c := NewClient(tgCfg(srv.URL))
	if _, err := c.GetUpdates(context.Background(), 0, 0); err == nil {
		t.Fatal("非法 JSON 應回錯誤")
	}
}

func TestGetUpdatesConnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient(tgCfg(url))
	if _, err := c.GetUpdates(context.Background(), 0, 0); err == nil {
		t.Fatal("連線失敗應回錯誤")
	}
}

func TestGetUpdatesBadURL(t *testing.T) {
	c := NewClient(&Config{BotToken: "t", BaseURL: "http://exa mple.com"})
	if _, err := c.GetUpdates(context.Background(), 0, 0); err == nil {
		t.Fatal("非法 URL 應回錯誤")
	}
}

func TestChatIDHelpers(t *testing.T) {
	if got := ParseChatID("  123  "); got != "123" {
		t.Errorf("ParseChatID = %q", got)
	}
	if got := ChatIDFromInt(456); got != "456" {
		t.Errorf("ChatIDFromInt = %q", got)
	}
	if got := ChatIDFromInt(-1); got != "-1" {
		t.Errorf("ChatIDFromInt neg = %q", got)
	}
}
