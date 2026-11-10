package line

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setLineEnv(t *testing.T, id, secret, token, base string) {
	t.Helper()
	t.Setenv("LINE_CHANNEL_ID", id)
	t.Setenv("LINE_CHANNEL_SECRET", secret)
	t.Setenv("LINE_CHANNEL_TOKEN", token)
	t.Setenv("LINE_BASE_URL", base)
}

func TestLoadFull(t *testing.T) {
	setLineEnv(t, "id1", "sec1", "tok1", "http://custom:9000")
	cfg, mock := Load()
	if mock || cfg.Mock {
		t.Error("有 token 不應是 Mock")
	}
	if cfg.ChannelID != "id1" || cfg.ChannelSecret != "sec1" || cfg.ChannelAccessToken != "tok1" {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.BaseURL != "http://custom:9000" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if !cfg.Valid() || !cfg.Ready() {
		t.Error("應 Valid 且 Ready")
	}
	if len(cfg.Missing()) != 0 {
		t.Errorf("Missing = %v", cfg.Missing())
	}
	if got := cfg.StatusLine(); got != "已連線（Channel id1）" {
		t.Errorf("StatusLine = %q", got)
	}
}

func TestLoadMockAndDefaultBase(t *testing.T) {
	setLineEnv(t, "", "", "", "")
	cfg, mock := Load()
	if !mock || !cfg.Mock {
		t.Error("無 token 應是 Mock")
	}
	if cfg.BaseURL != "https://api.line.me" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Valid() || cfg.Ready() {
		t.Error("無憑證不應 Valid/Ready")
	}
	if len(cfg.Missing()) != 3 {
		t.Errorf("Missing = %v", cfg.Missing())
	}
	s := cfg.StatusLine()
	if !strings.Contains(s, "MOCK") || !strings.Contains(s, "LINE_CHANNEL_TOKEN") {
		t.Errorf("StatusLine = %q", s)
	}
}

func TestLoadWhitespaceTrim(t *testing.T) {
	setLineEnv(t, "  id  ", " sec ", " tok ", "  ")
	cfg, _ := Load()
	if cfg.ChannelID != "id" || cfg.ChannelSecret != "sec" || cfg.ChannelAccessToken != "tok" {
		t.Errorf("應 trim: %+v", cfg)
	}
	if cfg.BaseURL != "https://api.line.me" {
		t.Errorf("空白 BaseURL 應回預設: %q", cfg.BaseURL)
	}
}

func TestConfigValidMissingStatus(t *testing.T) {
	cases := []struct {
		cfg     *Config
		valid   bool
		missing int
		status  string
	}{
		{&Config{}, false, 3, "憑證不完整"},
		{&Config{Mock: true}, false, 3, "MOCK"},
		{&Config{Mock: true, ChannelID: "x"}, false, 2, "MOCK"},
		{&Config{ChannelID: "x", ChannelSecret: "s", ChannelAccessToken: "t"}, true, 0, "已連線"},
		{&Config{ChannelID: "x", ChannelSecret: "s"}, false, 1, "憑證不完整"},
	}
	for _, tc := range cases {
		if tc.cfg.Valid() != tc.valid {
			t.Errorf("Valid(%+v) = %v want %v", tc.cfg, tc.cfg.Valid(), tc.valid)
		}
		if len(tc.cfg.Missing()) != tc.missing {
			t.Errorf("Missing(%+v) = %v", tc.cfg, tc.cfg.Missing())
		}
		if !strings.Contains(tc.cfg.StatusLine(), tc.status) {
			t.Errorf("StatusLine(%+v) = %q want contains %q", tc.cfg, tc.cfg.StatusLine(), tc.status)
		}
	}
	// Mock 但憑證齊全 → 第二種 MOCK 文案
	full := &Config{Mock: true, ChannelID: "x", ChannelSecret: "s", ChannelAccessToken: "t"}
	if full.StatusLine() != "MOCK 模式 — 通知只記錄不外送" {
		t.Errorf("StatusLine full mock = %q", full.StatusLine())
	}
	if full.Ready() {
		t.Error("Mock 不應 Ready")
	}
}

func TestErrUnconfigured(t *testing.T) {
	if ErrUnconfigured() == nil {
		t.Fatal("應回非 nil 錯誤")
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
	loadFromEnvFile(filepath.Join(t.TempDir(), "does-not-exist.env")) // 不應 panic

	dir := t.TempDir()
	f := filepath.Join(dir, "test.env")
	content := "# comment\n\nFOO=bar\nQUOTED=\"hello world\"\nSQUOTED='single'\nNOEQ\n  SPACED = val  \n"
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// 既有環境變數不被蓋掉（含明確設為空字串的情況）
	t.Setenv("FOO", "orig")
	t.Setenv("QUOTED", "")
	loadFromEnvFile(f)
	if os.Getenv("FOO") != "orig" {
		t.Errorf("FOO 被蓋掉: %q", os.Getenv("FOO"))
	}
	if os.Getenv("QUOTED") != "" {
		t.Errorf("QUOTED（明確空字串）被 .env 補入: %q", os.Getenv("QUOTED"))
	}
	if os.Getenv("SQUOTED") != "'single" {
		t.Errorf("SQUOTED = %q", os.Getenv("SQUOTED"))
	}
	if os.Getenv("SPACED") != "val" {
		t.Errorf("SPACED = %q", os.Getenv("SPACED"))
	}
}

func TestSharedAndReset(t *testing.T) {
	setLineEnv(t, "id9", "sec9", "tok9", "")
	ResetSharedForTest()
	t.Cleanup(ResetSharedForTest)
	cfg1, cli1 := Shared()
	cfg2, cli2 := Shared()
	if cfg1 != cfg2 || cli1 != cli2 {
		t.Error("Shared 應回同一份")
	}
	if cfg1.ChannelID != "id9" {
		t.Errorf("cfg = %+v", cfg1)
	}
	ResetSharedForTest()
	setLineEnv(t, "id10", "sec10", "tok10", "")
	cfg3, _ := Shared()
	if cfg3.ChannelID != "id10" {
		t.Errorf("Reset 後應重讀: %+v", cfg3)
	}
}

func TestClearEnvFileCache(t *testing.T) {
	t.Setenv("LINE_CHANNEL_ID", "x")
	t.Setenv("LINE_CHANNEL_SECRET", "y")
	t.Setenv("LINE_CHANNEL_TOKEN", "z")
	t.Setenv("LINE_BASE_URL", "b")
	ClearEnvFileCache()
	for _, k := range []string{"LINE_CHANNEL_ID", "LINE_CHANNEL_SECRET", "LINE_CHANNEL_TOKEN", "LINE_BASE_URL"} {
		if _, present := os.LookupEnv(k); present {
			t.Errorf("%s 應被清除", k)
		}
	}
}
