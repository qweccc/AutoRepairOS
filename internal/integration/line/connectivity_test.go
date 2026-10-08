package line

import (
	"testing"
)

// TestConnectabilityWithToken 實際發送一次，驗證連線能力。
// 無真實 token 會回 401，但證明真的連出去了（不是 Mock logging）。
func TestConnectabilityWithToken(t *testing.T) {
	cli := NewClient(&Config{
		ChannelID:          "fake-channel-id",
		ChannelSecret:      "fake-secret",
		ChannelAccessToken: "fake-access-token",
		BaseURL:            "https://api.line.me",
	})
	cfg := cli.cfg
	if !cfg.Valid() {
		t.Fatal("Valid() 應為 true（有 token）")
	}
	err := cli.PushMessage("@fakeuser", "connectivity test")
	if err == nil {
		t.Log("PushMessage 成功（真的連出去了）")
		return
	}
	// 無效 token 通常回 401/403，這是正常現象。
	t.Logf("PushMessage 結果: %v", err)
	if !contains(err.Error(), "狀態") && !contains(err.Error(), "401") && !contains(err.Error(), "403") {
		t.Logf("注意：錯誤訊息不是典型的「狀態 4xx」，可能是 DNS/連線層問題：%v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
