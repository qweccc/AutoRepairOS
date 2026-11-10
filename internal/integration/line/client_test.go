package line

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------- NewClient / APIError ----------

func TestNewClientNil(t *testing.T) {
	c := NewClient(nil)
	if c == nil || c.cfg == nil {
		t.Fatal("NewClient(nil) 回傳 nil")
	}
	if c.cfg.BaseURL != "https://api.line.me" {
		t.Errorf("BaseURL = %q, want https://api.line.me", c.cfg.BaseURL)
	}
	if c.httpClient == nil {
		t.Error("httpClient 未設定")
	}
}

func TestNewClientWithCfg(t *testing.T) {
	cfg := &Config{ChannelID: "id", BaseURL: "http://x"}
	c := NewClient(cfg)
	if c.cfg != cfg {
		t.Error("應保留傳入的 cfg")
	}
}

func TestAPIErrorString(t *testing.T) {
	e := &APIError{StatusCode: 400, Response: "bad"}
	if got := e.Error(); got != "推播狀態 400: bad" {
		t.Errorf("Error() = %q", got)
	}
}

// ---------- VerifyEventSignature ----------

func sign(t *testing.T, secret string, body []byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return mac.Sum(nil)
}

func TestVerifyEventSignature(t *testing.T) {
	body := []byte(`{"destination":"x","events":[]}`)
	secret := "testsecret"
	good := &Config{ChannelID: "id", ChannelSecret: secret}
	sum := sign(t, secret, body)

	cases := []struct {
		name string
		cfg  *Config
		sig  string
		want bool
	}{
		{"hex", good, hex.EncodeToString(sum), true},
		{"hex prefix", good, "signature=" + hex.EncodeToString(sum), true},
		{"hex whitespace", good, "  " + hex.EncodeToString(sum) + "  ", true},
		{"base64 std", good, base64.StdEncoding.EncodeToString(sum), true},
		{"base64 url", good, base64.URLEncoding.EncodeToString(sum), true},
		{"base64 raw std", good, base64.RawStdEncoding.EncodeToString(sum), true},
		{"base64 raw url", good, base64.RawURLEncoding.EncodeToString(sum), true},
		{"wrong", good, hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")), false},
		{"empty sig", good, "", false},
		{"only prefix", good, "signature=", false},
		{"no channel id", &Config{ChannelSecret: secret}, hex.EncodeToString(sum), false},
		{"no secret", &Config{ChannelID: "id"}, hex.EncodeToString(sum), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(tc.cfg)
			ok, raw := c.VerifyEventSignature(body, tc.sig)
			if ok != tc.want {
				t.Errorf("ok = %v, want %v", ok, tc.want)
			}
			if string(raw) != string(body) {
				t.Error("應回傳原始 body")
			}
		})
	}
}

// ---------- do() ----------

func TestDoMarshalError(t *testing.T) {
	c := NewClient(&Config{BaseURL: "http://127.0.0.1:1"})
	_, _, err := c.do(http.MethodPost, "/x", map[string]interface{}{"f": func() {}}, nil)
	if err == nil {
		t.Fatal("不可序列化 body 應回錯誤")
	}
}

func TestDoNewRequestError(t *testing.T) {
	c := NewClient(&Config{BaseURL: "http://exa mple.com"})
	_, _, err := c.do(http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("非法 URL 應回錯誤")
	}
}

func TestDoConnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // 連線被拒
	c := NewClient(&Config{BaseURL: url})
	_, _, err := c.do(http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("連線失敗應回錯誤")
	}
}

func TestDoSuccess(t *testing.T) {
	var gotAuth, gotCT, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		var v map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&v)
		gotBody = fmt.Sprint(v["k"])
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := NewClient(&Config{BaseURL: srv.URL})
	resp, data, err := c.do(http.MethodPost, "/v2/bot/message/push", map[string]interface{}{"k": "v"}, map[string]string{"Authorization": "Bearer tok"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || string(data) != "ok" {
		t.Errorf("resp = %d %q", resp.StatusCode, data)
	}
	if gotAuth != "Bearer tok" || gotCT != "application/json" || gotBody != "v" {
		t.Errorf("headers/body = %q %q %q", gotAuth, gotCT, gotBody)
	}
}

// ---------- Push / Reply / Flex ----------

func validCfg(baseURL string) *Config {
	return &Config{ChannelID: "id", ChannelSecret: "sec", ChannelAccessToken: "tok", BaseURL: baseURL}
}

func TestPushMessageMock(t *testing.T) {
	c := NewClient(&Config{}) // 無 token → Mock
	if err := c.PushMessage("U123", "hello"); err != nil {
		t.Errorf("mock push 應為 nil: %v", err)
	}
}

func TestPushMessageOK(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := NewClient(validCfg(srv.URL))
	if err := c.PushMessage("U123", "hello"); err != nil {
		t.Fatal(err)
	}
	if path != "/v2/bot/message/push" {
		t.Errorf("path = %q", path)
	}
}

func TestPushMessageAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()
	c := NewClient(validCfg(srv.URL))
	err := c.PushMessage("U123", "hello")
	ae, ok := err.(*APIError)
	if !ok || ae.StatusCode != 429 || ae.Response != "rate limited" {
		t.Errorf("err = %#v", err)
	}
}

func TestPushMessageConnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient(validCfg(url))
	if err := c.PushMessage("U123", "hello"); err == nil {
		t.Fatal("連線失敗應回錯誤")
	}
}

func TestSendFlexMessageMock(t *testing.T) {
	c := NewClient(&Config{})
	err := c.SendFlexMessage("U1", "估價單", "明細", []ItemLine{{Desc: "機油", Total: 100}}, 100, "http://a")
	if err != nil {
		t.Errorf("mock flex 應為 nil: %v", err)
	}
}

func TestSendFlexMessageOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&v)
		msgs := v["messages"].([]interface{})
		m0 := msgs[0].(map[string]interface{})
		if m0["type"] != "flex" || m0["altText"] != "估價單" {
			t.Errorf("flex payload 不正確: %v", m0)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := NewClient(validCfg(srv.URL))
	err := c.SendFlexMessage("U1", "估價單", "明細", []ItemLine{{Desc: "機油", Total: 100}}, 100, "http://a")
	if err != nil {
		t.Fatal(err)
	}
}

func TestSendFlexMessageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte("bad flex"))
	}))
	defer srv.Close()
	c := NewClient(validCfg(srv.URL))
	err := c.SendFlexMessage("U1", "t", "x", nil, 0, "")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("err = %v", err)
	}
}

func TestSendFlexMessageConnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient(validCfg(url))
	if err := c.SendFlexMessage("U1", "t", "x", nil, 0, ""); err == nil {
		t.Fatal("連線失敗應回錯誤")
	}
}

func TestReplyMessageMock(t *testing.T) {
	c := NewClient(&Config{})
	if err := c.ReplyMessage("rt", "hi"); err != nil {
		t.Errorf("mock reply 應為 nil: %v", err)
	}
}

func TestReplyMessageOK(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := NewClient(validCfg(srv.URL))
	if err := c.ReplyMessage("rt", "hi"); err != nil {
		t.Fatal(err)
	}
	if path != "/v2/bot/message/reply" {
		t.Errorf("path = %q", path)
	}
}

func TestReplyMessageRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte("invalid reply token"))
	}))
	defer srv.Close()
	c := NewClient(validCfg(srv.URL))
	if err := c.ReplyMessage("rt", "hi"); err == nil {
		t.Fatal("被拒應回錯誤")
	}
}

func TestReplyMessageConnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient(validCfg(url))
	if err := c.ReplyMessage("rt", "hi"); err == nil {
		t.Fatal("連線失敗應回錯誤")
	}
}

// ---------- 小工具 ----------

func TestHelpers(t *testing.T) {
	if got := truncate("abcdef", 3); got != "abc..." {
		t.Errorf("truncate long = %q", got)
	}
	if got := truncate("ab", 3); got != "ab" {
		t.Errorf("truncate short = %q", got)
	}
	if got := Money(1234.5); got != "1234.50" {
		t.Errorf("Money = %q", got)
	}
	if got := money(0); got != "0.00" {
		t.Errorf("money = %q", got)
	}
	c := NewClient(&Config{ChannelAccessToken: "tok"})
	if got := c.authHeader(); got != "Bearer tok" {
		t.Errorf("authHeader = %q", got)
	}
	if err := mockPushLog("U", "x"); err != nil {
		t.Error(err)
	}
	if err := mockFlexLog("U", "t", 1, "u"); err != nil {
		t.Error(err)
	}
	if err := mockReplyLog("r", "x"); err != nil {
		t.Error(err)
	}
	resp, data := c.DebugLastResp()
	if resp != nil || data != nil {
		t.Error("DebugLastResp 應回 nil,nil")
	}
	scheme, host, err := c.URLParser()
	if err != nil || scheme != "" || host != "" {
		t.Errorf("URLParser empty = %q %q %v", scheme, host, err)
	}
	c2 := NewClient(&Config{BaseURL: "https://api.line.me"})
	scheme, host, err = c2.URLParser()
	if err != nil || scheme != "https" || host != "api.line.me" {
		t.Errorf("URLParser = %q %q %v", scheme, host, err)
	}
	c3 := NewClient(&Config{BaseURL: "http://example.com/%zz"})
	if _, _, err := c3.URLParser(); err == nil {
		t.Error("非法 URL 應回錯誤")
	}
}
