package discord

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------- Config ----------

func TestLoadWithToken(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "tok-abc")
	t.Setenv("DISCORD_CHANNEL_ID", "999")
	t.Setenv("DISCORD_BASE_URL", "http://custom:9000")
	cfg, ok := Load()
	if !ok || cfg.Mock {
		t.Error("有 token 應非 Mock")
	}
	if cfg.BotToken != "tok-abc" || cfg.ChannelID != "999" || cfg.BaseURL != "http://custom:9000" {
		t.Errorf("cfg = %+v", cfg)
	}
	if !cfg.Valid() {
		t.Error("應 Valid")
	}
}

func TestLoadMockWhenUnset(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "")
	t.Setenv("DISCORD_CHANNEL_ID", "")
	cfg, ok := Load()
	if cfg.BotToken != "" {
		t.Skip("repo .env 已提供 DISCORD_BOT_TOKEN，無法測 unset 情境")
	}
	if ok || !cfg.Mock || cfg.Valid() {
		t.Errorf("無 token 應 Mock 且非 Valid: ok=%v cfg=%+v", ok, cfg)
	}
}

func TestLoadDefaultBaseURL(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "tok-abc")
	t.Setenv("DISCORD_BASE_URL", "")
	cfg, _ := Load()
	// 注意：.env 若有 DISCORD_BASE_URL 會被載入；只斷言非空且 Valid
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

func TestMaskChannel(t *testing.T) {
	cases := map[string]string{
		"":         "(未設定)",
		"ab":       "****",
		"1234":     "****",
		"12345678": "****5678",
	}
	for in, want := range cases {
		if got := maskChannel(in); got != want {
			t.Errorf("maskChannel(%q) = %q, want %q", in, got, want)
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
	content := "# c\n\nDISCORD_TEST_TOK=fromfile\nDISCORD_TEST_QUOTED=\"q v\"\nNOEQ\n"
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DISCORD_TEST_TOK", "existing") // 已有值不被蓋
	loadFromEnvFile(f)
	if os.Getenv("DISCORD_TEST_TOK") != "existing" {
		t.Errorf("TOK 被蓋: %q", os.Getenv("DISCORD_TEST_TOK"))
	}
	if os.Getenv("DISCORD_TEST_QUOTED") != `"q v` {
		t.Errorf("QUOTED = %q", os.Getenv("DISCORD_TEST_QUOTED"))
	}
}

func TestSharedAndReset(t *testing.T) {
	t.Setenv("DISCORD_BOT_TOKEN", "tok-shared")
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
	t.Setenv("DISCORD_BOT_TOKEN", "tok-2")
	cfg3, _ := Shared()
	if cfg3.BotToken != "tok-2" {
		t.Errorf("Reset 後應重讀: %+v", cfg3)
	}
}

// ---------- SendMessage ----------

func dcCfg(base string) *Config {
	return &Config{BotToken: "test-token", ChannelID: "C1", BaseURL: base}
}

func TestSendMessageMockAndNil(t *testing.T) {
	c := NewClient(&Config{}) // 無 token → mock，不連網路
	if err := c.SendMessage("1", "hi"); err != nil {
		t.Errorf("mock 應為 nil: %v", err)
	}
	c2 := NewClient(nil)
	if err := c2.SendMessage("", "hi"); err != nil {
		t.Errorf("nil cfg mock 應為 nil: %v", err)
	}
	c3 := NewClient(&Config{Mock: true})
	if err := c3.SendMessage("1", "hi"); err != nil {
		t.Errorf("Mock flag 應為 nil: %v", err)
	}
}

func TestSendMessageOK(t *testing.T) {
	var gotPath, gotAuth, gotCT, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		var v map[string]string
		_ = json.NewDecoder(r.Body).Decode(&v)
		gotContent = v["content"]
		_, _ = w.Write([]byte(`{"id":"M1"}`))
	}))
	defer srv.Close()
	c := NewClient(dcCfg(srv.URL))
	if err := c.SendMessage("C1", "hello"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v10/channels/C1/messages" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bot test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotContent != "hello" {
		t.Errorf("content = %q", gotContent)
	}
}

func TestSendMessageDefaultChannel(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"M1"}`))
	}))
	defer srv.Close()
	c := NewClient(dcCfg(srv.URL))
	if err := c.SendMessage("", "hi"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v10/channels/C1/messages" {
		t.Errorf("path = %q, want 預設 C1", gotPath)
	}
}

func TestSendMessageNoChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := NewClient(&Config{BotToken: "t", BaseURL: srv.URL})
	if err := c.SendMessage("", "x"); err == nil {
		t.Fatal("無 channel id 應回錯誤")
	}
}

func TestSendMessageTruncation(t *testing.T) {
	var gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]string
		_ = json.NewDecoder(r.Body).Decode(&v)
		gotContent = v["content"]
		_, _ = w.Write([]byte(`{"id":"M1"}`))
	}))
	defer srv.Close()
	c := NewClient(dcCfg(srv.URL))
	long := strings.Repeat("修", 3000)
	if err := c.SendMessage("C1", long); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(gotContent)); n > maxContentLen {
		t.Errorf("content 長度 = %d, want <= %d", n, maxContentLen)
	}
	if !strings.HasSuffix(gotContent, "...") {
		t.Errorf("截斷結尾應為 ...: %q", gotContent[len(gotContent)-10:])
	}
}

func TestSendMessageHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized"}`))
	}))
	defer srv.Close()
	c := NewClient(dcCfg(srv.URL))
	err := c.SendMessage("C1", "x")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("非 2xx 應回錯誤: %v", err)
	}
}

func TestSendMessageConnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient(dcCfg(url))
	if err := c.SendMessage("C1", "x"); err == nil {
		t.Fatal("連線失敗應回錯誤")
	}
}

func TestSendMessageBadURL(t *testing.T) {
	c := NewClient(&Config{BotToken: "t", ChannelID: "1", BaseURL: "http://exa mple.com"})
	if err := c.SendMessage("1", "x"); err == nil {
		t.Fatal("非法 URL 應回錯誤")
	}
}

// ---------- SendDirectMessage ----------

func TestSendDirectMessageMockAndNil(t *testing.T) {
	c := NewClient(&Config{})
	if err := c.SendDirectMessage("U1", "hi"); err != nil {
		t.Errorf("mock 應為 nil: %v", err)
	}
	c2 := NewClient(nil)
	if err := c2.SendDirectMessage("U1", "hi"); err != nil {
		t.Errorf("nil cfg mock 應為 nil: %v", err)
	}
}

func TestSendDirectMessageOK(t *testing.T) {
	var mu sync.Mutex
	var paths, auths []string
	var dmBody, msgBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]string
		_ = json.NewDecoder(r.Body).Decode(&v)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		switch r.URL.Path {
		case "/api/v10/users/@me/channels":
			mu.Lock()
			dmBody = v
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"DM9"}`))
		case "/api/v10/channels/DM9/messages":
			mu.Lock()
			msgBody = v
			mu.Unlock()
			_, _ = w.Write([]byte(`{"id":"M1"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	c := NewClient(&Config{BotToken: "test-token", BaseURL: srv.URL})
	if err := c.SendDirectMessage("U42", "hello customer"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 || paths[0] != "/api/v10/users/@me/channels" || paths[1] != "/api/v10/channels/DM9/messages" {
		t.Fatalf("呼叫順序錯誤: %v", paths)
	}
	if dmBody["recipient_id"] != "U42" {
		t.Errorf("recipient_id = %q", dmBody["recipient_id"])
	}
	if msgBody["content"] != "hello customer" {
		t.Errorf("content = %q", msgBody["content"])
	}
	for _, a := range auths {
		if a != "Bot test-token" {
			t.Errorf("Authorization = %q", a)
		}
	}
}

func TestSendDirectMessageNoUserID(t *testing.T) {
	c := NewClient(&Config{BotToken: "t", BaseURL: "http://127.0.0.1:0"})
	if err := c.SendDirectMessage("", "x"); err == nil {
		t.Fatal("無 user id 應回錯誤")
	}
}

func TestSendDirectMessageCreateFails(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"message":"Missing Access"}`))
	}))
	defer srv.Close()
	c := NewClient(&Config{BotToken: "t", BaseURL: srv.URL})
	err := c.SendDirectMessage("U1", "x")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("建 DM channel 失敗應回錯誤: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 1 {
		t.Errorf("建 channel 失敗後不應再送訊息: %v", paths)
	}
}

func TestSendDirectMessageBadChannelJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"noid":true}`))
	}))
	defer srv.Close()
	c := NewClient(&Config{BotToken: "t", BaseURL: srv.URL})
	if err := c.SendDirectMessage("U1", "x"); err == nil {
		t.Fatal("DM channel 回應缺 id 應回錯誤")
	}
}

// ---------- helpers ----------

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
	if got := clampContent(strings.Repeat("a", 2500)); len([]rune(got)) != maxContentLen {
		t.Errorf("clampContent 長度 = %d", len([]rune(got)))
	}
	if got := clampContent("短訊"); got != "短訊" {
		t.Errorf("clampContent short = %q", got)
	}
}

// ---------- handleGatewayPayload ----------

func TestHandleGatewayPayload(t *testing.T) {
	cases := []struct {
		name          string
		payload       string
		wantOp        int
		wantInterval  int
		wantReconnect bool
		wantMsgs      [][2]string
		wantSeq       int64
	}{
		{"hello", `{"op":10,"d":{"heartbeat_interval":45000}}`, 10, 45000, false, nil, 0},
		{"guild message", `{"op":0,"s":7,"t":"MESSAGE_CREATE","d":{"author":{"id":"U1","bot":false},"content":"hi","guild_id":"G1"}}`, 0, 0, false, [][2]string{{"U1", "hi"}}, 7},
		{"dm message", `{"op":0,"s":8,"t":"MESSAGE_CREATE","d":{"author":{"id":"U2"},"content":"dm"}}`, 0, 0, false, [][2]string{{"U2", "dm"}}, 8},
		{"bot skipped", `{"op":0,"s":9,"t":"MESSAGE_CREATE","d":{"author":{"id":"B1","bot":true},"content":"x"}}`, 0, 0, false, nil, 9},
		{"empty skipped", `{"op":0,"s":10,"t":"MESSAGE_CREATE","d":{"author":{"id":"U1"},"content":"   "}}`, 0, 0, false, nil, 10},
		{"other event", `{"op":0,"s":11,"t":"READY","d":{}}`, 0, 0, false, nil, 11},
		{"ack", `{"op":11,"d":null}`, 11, 0, false, nil, 0},
		{"heartbeat request", `{"op":1,"d":null}`, 1, 0, false, nil, 0},
		{"reconnect", `{"op":7,"d":null}`, 7, 0, true, nil, 0},
		{"invalid session", `{"op":9,"d":false}`, 9, 0, true, nil, 0},
		{"malformed", `not json`, -1, 0, false, nil, 0},
		{"bad message d", `{"op":0,"s":12,"t":"MESSAGE_CREATE","d":"oops"}`, 0, 0, false, nil, 12},
		{"hello null d", `{"op":10,"d":null}`, 10, 0, false, nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var seq int64
			var got [][2]string
			op, interval, reconnect := handleGatewayPayload([]byte(c.payload), &seq,
				func(userID, text string) { got = append(got, [2]string{userID, text}) })
			if op != c.wantOp || interval != c.wantInterval || reconnect != c.wantReconnect {
				t.Errorf("op/interval/reconnect = %d/%d/%v, want %d/%d/%v",
					op, interval, reconnect, c.wantOp, c.wantInterval, c.wantReconnect)
			}
			if seq != c.wantSeq {
				t.Errorf("seq = %d, want %d", seq, c.wantSeq)
			}
			if len(got) != len(c.wantMsgs) {
				t.Fatalf("msgs = %v, want %v", got, c.wantMsgs)
			}
			for i := range got {
				if got[i] != c.wantMsgs[i] {
					t.Errorf("msg[%d] = %v, want %v", i, got[i], c.wantMsgs[i])
				}
			}
		})
	}
}

func TestHandleGatewayPayloadNilSafety(t *testing.T) {
	// nil seq / nil callback 不可 panic
	payload := []byte(`{"op":0,"s":3,"t":"MESSAGE_CREATE","d":{"author":{"id":"U1"},"content":"x"}}`)
	if op, _, _ := handleGatewayPayload(payload, nil, nil); op != 0 {
		t.Errorf("op = %d", op)
	}
	if op, _, _ := handleGatewayPayload([]byte(`{broken`), nil, nil); op != -1 {
		t.Errorf("malformed op = %d", op)
	}
}

func TestRunGatewayEmptyToken(t *testing.T) {
	if err := RunGateway(context.Background(), "", nil); err == nil {
		t.Fatal("無 token 應回錯誤（不連網路）")
	}
}

// ---------- RunGateway 整合測試（httptest WebSocket server） ----------

func TestRunGatewayReconnectAndDispatch(t *testing.T) {
	var connCount, heartbeats atomic.Int32
	identifies := make(chan map[string]interface{}, 8)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		n := connCount.Add(1)
		_ = conn.WriteJSON(map[string]interface{}{"op": 10, "d": map[string]interface{}{"heartbeat_interval": 10}})
		// 等到 IDENTIFY（沿途忽略心跳）
		for {
			_, p, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var f struct {
				Op int             `json:"op"`
				D  json.RawMessage `json:"d"`
			}
			if json.Unmarshal(p, &f) != nil {
				continue
			}
			if f.Op == 1 {
				heartbeats.Add(1)
				continue
			}
			if f.Op == 2 {
				var d map[string]interface{}
				_ = json.Unmarshal(f.D, &d)
				identifies <- d
				break
			}
		}
		if n == 1 {
			// 第一條連線：guild 訊息 → op 7 要求重連
			_ = conn.WriteJSON(map[string]interface{}{"op": 0, "s": 1, "t": "MESSAGE_CREATE",
				"d": map[string]interface{}{"author": map[string]interface{}{"id": "U1"}, "content": "guild hello", "guild_id": "G1"}})
			_ = conn.WriteJSON(map[string]interface{}{"op": 7, "d": nil})
		} else {
			// 第二條連線：DM 訊息（無 guild_id）
			_ = conn.WriteJSON(map[string]interface{}{"op": 0, "s": 2, "t": "MESSAGE_CREATE",
				"d": map[string]interface{}{"author": map[string]interface{}{"id": "U2"}, "content": "dm hello"}})
		}
		for {
			_, p, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var f struct {
				Op int `json:"op"`
			}
			if json.Unmarshal(p, &f) == nil && f.Op == 1 {
				heartbeats.Add(1)
			}
		}
	}))
	defer srv.Close()

	oldURL, oldBackoff := gatewayURL, reconnectBackoff
	gatewayURL = "ws" + strings.TrimPrefix(srv.URL, "http")
	reconnectBackoff = 20 * time.Millisecond
	defer func() { gatewayURL, reconnectBackoff = oldURL, oldBackoff }()

	msgs := make(chan [2]string, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunGateway(ctx, "test-token", func(userID, text string) {
			msgs <- [2]string{userID, text}
		})
	}()

	for _, want := range [][2]string{{"U1", "guild hello"}, {"U2", "dm hello"}} {
		select {
		case got := <-msgs:
			if got != want {
				t.Errorf("訊息 = %v, want %v", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("未收到訊息 %v", want)
		}
	}

	// 兩條連線都應送出全新 IDENTIFY（token + intents 正確）
	for i := 0; i < 2; i++ {
		select {
		case d := <-identifies:
			if d["token"] != "test-token" {
				t.Errorf("IDENTIFY token 錯誤")
			}
			if intents, _ := d["intents"].(float64); int(intents) != gatewayIntents {
				t.Errorf("intents = %v, want %d", d["intents"], gatewayIntents)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("未收到 IDENTIFY")
		}
	}

	// 等心跳 goroutine 實際送出 op 1 再取消（interval=10ms，很快）
	deadline := time.Now().Add(2 * time.Second)
	for heartbeats.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 取消後 RunGateway 未結束")
	}
	if heartbeats.Load() == 0 {
		t.Error("心跳 goroutine 未送出任何 op 1")
	}
	if connCount.Load() < 2 {
		t.Errorf("連線數 = %d, want >= 2（op 7 應觸發重連）", connCount.Load())
	}
}
