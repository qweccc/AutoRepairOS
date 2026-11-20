package cron

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"autorepairos/internal/integration/discord"
	"autorepairos/internal/integration/slack"
	"autorepairos/internal/integration/telegram"
)

func TestDeliverInternalPushesToConfiguredChannels(t *testing.T) {
	var tgHits, dcHits, slHits int32
	var tgBody, dcBody, slBody string

	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tgHits, 1)
		b, _ := io.ReadAll(r.Body)
		tgBody = string(b)
		if !strings.Contains(r.URL.Path, "/bot") || !strings.Contains(r.URL.Path, "sendMessage") {
			t.Errorf("telegram path=%s", r.URL.Path)
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer tgSrv.Close()
	dcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dcHits, 1)
		b, _ := io.ReadAll(r.Body)
		dcBody = string(b)
		if r.Header.Get("Authorization") != "Bot test-token" {
			t.Errorf("discord auth=%s", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer dcSrv.Close()
	slSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&slHits, 1)
		b, _ := io.ReadAll(r.Body)
		slBody = string(b)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer slSrv.Close()

	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("TELEGRAM_CHAT_ID", "123456")
	t.Setenv("TELEGRAM_BASE_URL", tgSrv.URL)
	t.Setenv("DISCORD_BOT_TOKEN", "test-token")
	t.Setenv("DISCORD_CHANNEL_ID", "C1")
	t.Setenv("DISCORD_BASE_URL", dcSrv.URL)
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test")
	t.Setenv("SLACK_CHANNEL_ID", "C1")
	t.Setenv("SLACK_BASE_URL", slSrv.URL)
	telegram.ResetSharedForTest()
	discord.ResetSharedForTest()
	slack.ResetSharedForTest()
	defer func() {
		telegram.ResetSharedForTest()
		discord.ResetSharedForTest()
		slack.ResetSharedForTest()
	}()

	deliverInternal(notificationItem{subject: "新的 LINE 好友（待綁定）", body: "LINE user ID: U1 — 等待客戶報手機號碼"})

	if tgHits != 1 || dcHits != 1 || slHits != 1 {
		t.Fatalf("hits tg=%d dc=%d sl=%d, want 1/1/1", tgHits, dcHits, slHits)
	}
	for _, b := range []string{tgBody, dcBody, slBody} {
		if !strings.Contains(b, "新的 LINE 好友") || !strings.Contains(b, "U1") {
			t.Fatalf("push body missing subject/body: %s", b)
		}
	}
	if !strings.Contains(tgBody, "123456") {
		t.Fatalf("telegram chat_id missing: %s", tgBody)
	}
}

func TestDeliverInternalUnconfiguredIsSilent(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// 全未設定（正式 .env 目前無這些 token）：MOCK，只 log 不外送。
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	t.Setenv("TELEGRAM_BASE_URL", srv.URL)
	t.Setenv("DISCORD_BOT_TOKEN", "")
	t.Setenv("DISCORD_CHANNEL_ID", "")
	t.Setenv("DISCORD_BASE_URL", srv.URL)
	t.Setenv("SLACK_BOT_TOKEN", "")
	t.Setenv("SLACK_APP_TOKEN", "")
	t.Setenv("SLACK_CHANNEL_ID", "")
	t.Setenv("SLACK_BASE_URL", srv.URL)
	telegram.ResetSharedForTest()
	discord.ResetSharedForTest()
	slack.ResetSharedForTest()
	defer func() {
		telegram.ResetSharedForTest()
		discord.ResetSharedForTest()
		slack.ResetSharedForTest()
	}()

	deliverInternal(notificationItem{subject: "s", body: "b"})
	if hits != 0 {
		t.Fatalf("unconfigured internal push hit network %d times", hits)
	}
}