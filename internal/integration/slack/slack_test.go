package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------- Config ----------

func TestLoadWithTokens(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_APP_TOKEN", "xapp-test")
	t.Setenv("SLACK_CHANNEL_ID", "C123")
	t.Setenv("SLACK_BASE_URL", "http://custom:9000")
	cfg, ok := Load()
	if !ok || cfg.Mock {
		t.Error("有 bot token 應非 Mock")
	}
	if cfg.BotToken != "xoxb-test" || cfg.AppToken != "xapp-test" || cfg.ChannelID != "C123" || cfg.BaseURL != "http://custom:9000" {
		t.Errorf("cfg = %+v", cfg)
	}
	if !cfg.Valid() || !cfg.SocketReady() {
		t.Error("雙 token 應 Valid 且 SocketReady")
	}
}

func TestLoadDefaultBaseURL(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_BASE_URL", "")
	cfg, _ := Load()
	if cfg.BaseURL == "" {
		t.Error("BaseURL 不應為空")
	}
}

func TestLoadMockWithoutToken(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "")
	t.Setenv("SLACK_APP_TOKEN", "")
	cfg, ok := Load()
	if ok || !cfg.Mock {
		t.Error("無 token 應為 Mock")
	}
	if cfg.Valid() || cfg.SocketReady() {
		t.Error("無 token 不應 Valid/SocketReady")
	}
}

func TestValidAndSocketReady(t *testing.T) {
	var nilCfg *Config
	if nilCfg.Valid() || nilCfg.SocketReady() {
		t.Error("nil Config 不應 Valid/SocketReady")
	}
	if (&Config{}).Valid() || (&Config{}).SocketReady() {
		t.Error("空 Config 不應 Valid/SocketReady")
	}
	botOnly := &Config{BotToken: "xoxb-x"}
	if !botOnly.Valid() {
		t.Error("有 bot token 應 Valid")
	}
	if botOnly.SocketReady() {
		t.Error("只有 bot token 不應 SocketReady")
	}
	appOnly := &Config{AppToken: "xapp-x"}
	if appOnly.Valid() || appOnly.SocketReady() {
		t.Error("只有 app token 不應 Valid/SocketReady")
	}
	both := &Config{BotToken: "xoxb-x", AppToken: "xapp-x"}
	if !both.Valid() || !both.SocketReady() {
		t.Error("雙 token 應 Valid 且 SocketReady")
	}
}

func TestSharedAndReset(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-shared")
	ResetSharedForTest()
	t.Cleanup(ResetSharedForTest)
	cfg1, cli1 := Shared()
	cfg2, cli2 := Shared()
	if cfg1 != cfg2 || cli1 != cli2 {
		t.Error("Shared 應回同一份")
	}
	if cfg1.BotToken != "xoxb-shared" {
		t.Errorf("cfg = %+v", cfg1)
	}
	ResetSharedForTest()
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-2")
	cfg3, _ := Shared()
	if cfg3.BotToken != "xoxb-2" {
		t.Errorf("Reset 後應重讀: %+v", cfg3)
	}
}

// ---------- SendMessage ----------

func slCfg(base string) *Config {
	return &Config{BotToken: "xoxb-test", ChannelID: "C42", BaseURL: base}
}

func TestSendMessageMockNoNetwork(t *testing.T) {
	// BaseURL 指向已關閉的伺服器：若 mock 仍發網路請求就會出錯
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := NewClient(&Config{BaseURL: url}) // 無 token → mock
	if err := c.SendMessage("C1", "hi"); err != nil {
		t.Errorf("mock 應為 nil: %v", err)
	}
	c2 := NewClient(nil)
	if err := c2.SendMessage("", "hi"); err != nil {
		t.Errorf("nil cfg mock 應為 nil: %v", err)
	}
}

func TestSendMessageOK(t *testing.T) {
	var gotPath, gotAuth, gotChannel, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var v map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&v)
		gotChannel, _ = v["channel"].(string)
		gotText, _ = v["text"].(string)
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C42","ts":"1.2"}`))
	}))
	defer srv.Close()

	c := NewClient(slCfg(srv.URL))
	if err := c.SendMessage("", "hello"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/chat.postMessage" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer xoxb-test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotChannel != "C42" {
		t.Errorf("channel = %q, want 預設 C42", gotChannel)
	}
	if gotText != "hello" {
		t.Errorf("text = %q", gotText)
	}
}

func TestSendMessageOKFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slack 錯誤是 HTTP 200 + ok:false
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()
	c := NewClient(slCfg(srv.URL))
	err := c.SendMessage("C1", "x")
	if err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("err = %v", err)
	}
}

func TestSendMessageHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte("invalid_auth"))
	}))
	defer srv.Close()
	c := NewClient(slCfg(srv.URL))
	if err := c.SendMessage("C1", "x"); err == nil {
		t.Fatal("非 200 應回錯誤")
	}
}

func TestSendMessageNoChannel(t *testing.T) {
	c := NewClient(&Config{BotToken: "xoxb-test", BaseURL: "https://slack.com"})
	if err := c.SendMessage("", "x"); err == nil {
		t.Fatal("無 channel id 應回錯誤")
	}
}

// ---------- handleSocketEnvelope ----------

func envelopeJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func imEnvelope(envelopeID, user, text string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "events_api",
		"envelope_id": envelopeID,
		"payload": map[string]interface{}{
			"event": map[string]interface{}{
				"type":         "message",
				"channel_type": "im",
				"user":         user,
				"text":         text,
			},
		},
	}
}

func TestHandleEnvelopeIMMessage(t *testing.T) {
	var acked []string
	var gotUser, gotText string
	reconnect := handleSocketEnvelope(
		envelopeJSON(t, imEnvelope("env-1", "U1", "你好")),
		func(id string) { acked = append(acked, id) },
		func(u, txt string) { gotUser, gotText = u, txt },
	)
	if reconnect {
		t.Error("events_api 不應要求重連")
	}
	if len(acked) != 1 || acked[0] != "env-1" {
		t.Errorf("ack = %v, want [env-1]", acked)
	}
	if gotUser != "U1" || gotText != "你好" {
		t.Errorf("onMessage = %q %q", gotUser, gotText)
	}
}

func TestHandleEnvelopeBotSkipped(t *testing.T) {
	env := imEnvelope("env-2", "U1", "bot 訊息")
	env["payload"].(map[string]interface{})["event"].(map[string]interface{})["bot_id"] = "B1"
	var acked, called int
	handleSocketEnvelope(envelopeJSON(t, env),
		func(string) { acked++ },
		func(string, string) { called++ },
	)
	if acked != 1 {
		t.Errorf("仍應先 ack: %d", acked)
	}
	if called != 0 {
		t.Error("bot_id 非空應跳過 onMessage")
	}
}

func TestHandleEnvelopeChannelSkipped(t *testing.T) {
	env := imEnvelope("env-3", "U1", "頻道訊息")
	env["payload"].(map[string]interface{})["event"].(map[string]interface{})["channel_type"] = "channel"
	var called int
	handleSocketEnvelope(envelopeJSON(t, env),
		func(string) {},
		func(string, string) { called++ },
	)
	if called != 0 {
		t.Error("channel_type 非 im 應跳過")
	}
}

func TestHandleEnvelopeSubtypeSkipped(t *testing.T) {
	for _, subtype := range []string{"message_replied", "message_changed", "bot_message"} {
		env := imEnvelope("env-4", "U1", "x")
		env["payload"].(map[string]interface{})["event"].(map[string]interface{})["subtype"] = subtype
		var called int
		handleSocketEnvelope(envelopeJSON(t, env),
			func(string) {},
			func(string, string) { called++ },
		)
		if called != 0 {
			t.Errorf("subtype=%s 應跳過", subtype)
		}
	}
}

func TestHandleEnvelopeEmptyTextSkipped(t *testing.T) {
	var called int
	handleSocketEnvelope(envelopeJSON(t, imEnvelope("env-5", "U1", "  ")),
		func(string) {},
		func(string, string) { called++ },
	)
	if called != 0 {
		t.Error("空白訊息應跳過")
	}
}

func TestHandleEnvelopeHelloDisconnect(t *testing.T) {
	if reconnect := handleSocketEnvelope([]byte(`{"type":"hello"}`), nil, nil); reconnect {
		t.Error("hello 不應要求重連")
	}
	if reconnect := handleSocketEnvelope([]byte(`{"type":"disconnect","reason":"refresh"}`), nil, nil); !reconnect {
		t.Error("disconnect 應要求重連")
	}
}

func TestHandleEnvelopeMalformedNoPanic(t *testing.T) {
	var called int
	for _, raw := range []string{`{invalid`, ``, `null`, `{"type":123}`} {
		handleSocketEnvelope([]byte(raw), func(string) { called++ }, func(string, string) { called++ })
	}
	if called != 0 {
		t.Error("malformed 封包不應觸發 ack/onMessage")
	}
}

// ---------- RunSocketMode（httptest WebSocket 整合測試） ----------

func TestRunSocketModeEndToEnd(t *testing.T) {
	ackCh := make(chan string, 1)
	upgrader := websocket.Upgrader{}
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))
		env, _ := json.Marshal(imEnvelope("env-e2e", "U9", "客戶訊息"))
		_ = conn.WriteMessage(websocket.TextMessage, env)
		// 讀 ack
		_, raw, err := conn.ReadMessage()
		if err == nil {
			var ack struct {
				EnvelopeID string `json:"envelope_id"`
			}
			_ = json.Unmarshal(raw, &ack)
			ackCh <- ack.EnvelopeID
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"disconnect"}`))
	}))
	defer wsSrv.Close()
	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")

	var gotAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/apps.connections.open" {
			w.WriteHeader(404)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "url": wsURL})
	}))
	defer apiSrv.Close()

	oldBase, oldBackoff := socketAPIBaseURL, socketBackoff
	socketAPIBaseURL, socketBackoff = apiSrv.URL, 10*time.Millisecond
	t.Cleanup(func() { socketAPIBaseURL, socketBackoff = oldBase, oldBackoff })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var gotUser, gotText string
	err := RunSocketMode(ctx, "xapp-test", func(u, txt string) {
		gotUser, gotText = u, txt
		cancel() // 收到訊息即結束，避免重連迴圈繼續跑
	})
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if gotUser != "U9" || gotText != "客戶訊息" {
		t.Errorf("onMessage = %q %q", gotUser, gotText)
	}
	if gotAuth != "Bearer xapp-test" {
		t.Errorf("apps.connections.open Authorization = %q", gotAuth)
	}
	select {
	case id := <-ackCh:
		if id != "env-e2e" {
			t.Errorf("ack envelope_id = %q", id)
		}
	default:
		t.Error("未收到 ack")
	}
}

func TestRunSocketModeNoAppToken(t *testing.T) {
	if err := RunSocketMode(context.Background(), "", nil); err == nil {
		t.Fatal("無 app token 應回錯誤")
	}
}

func TestOpenSocketConnectionOKFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer srv.Close()
	oldBase := socketAPIBaseURL
	socketAPIBaseURL = srv.URL
	t.Cleanup(func() { socketAPIBaseURL = oldBase })

	_, err := openSocketConnection(context.Background(), "xapp-bad")
	if err == nil || !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("err = %v", err)
	}
}
