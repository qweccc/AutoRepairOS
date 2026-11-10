package vision

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ---------- NewClient ----------

func TestNewClientVariants(t *testing.T) {
	// cloud 缺 key/url → nil
	if c := NewClient("cloud", "", "", "", "", false, 0, "", "", "", ""); c != nil {
		t.Error("cloud 未設定應回 nil")
	}
	if c := NewClient("cloud", "http://x", "", "", "", false, 0, "", "", "", ""); c != nil {
		t.Error("cloud 缺 key 應回 nil")
	}
	// local 缺 url → nil
	if c := NewClient("local", "", "", "", "", false, 0, "", "", "", ""); c != nil {
		t.Error("local 缺 url 應回 nil")
	}
	// cloud 正常（mode 空 → cloud；idleMin 預設 10；hfModel 預設；protocol 預設 openai）
	c := NewClient("", "http://x", "k", "", "", false, 0, "", "", "", "")
	if c == nil || c.Mode != "cloud" || c.IdleMin != 10 || c.LocalProtocol != "openai" || c.HFModel == "" {
		t.Errorf("c = %+v", c)
	}
	// local + custom protocol 保留
	c2 := NewClient(" LOCAL ", "", "", "", "http://localhost:8080", true, 5, "m:Q4", "/a.gguf", "/b.gguf", "custom")
	if c2 == nil || c2.Mode != "local" || c2.LocalProtocol != "custom" || c2.IdleMin != 5 || c2.ModelPath != "/a.gguf" {
		t.Errorf("c2 = %+v", c2)
	}
	// 未知 protocol → openai
	c3 := NewClient("local", "", "", "", "http://localhost:8080", false, 0, "", "", "", "bogus")
	if c3.LocalProtocol != "openai" {
		t.Errorf("protocol = %q", c3.LocalProtocol)
	}
	// cloudModel / openAIModel
	if got := c.cloudModel(); got != "gemini-2.0-flash" {
		t.Errorf("cloudModel default = %q", got)
	}
	c.Model = "my-model"
	if got := c.cloudModel(); got != "my-model" {
		t.Errorf("cloudModel = %q", got)
	}
	if got := c.openAIModel(); got != "my-model" {
		t.Errorf("openAIModel Model 優先 = %q", got)
	}
	c.Model = ""
	c2.HFModel = "unsloth/Qwen2.5-VL-3B-Instruct-GGUF:Q4_K_M"
	if got := c2.openAIModel(); got != "unsloth/Qwen2.5-VL-3B-Instruct-GGUF" {
		t.Errorf("openAIModel 去 :suffix = %q", got)
	}
	c2.HFModel = ""
	if got := c2.openAIModel(); got != "local" {
		t.Errorf("openAIModel 空 = %q", got)
	}
}

// ---------- chatText ----------

func chatServer(t *testing.T, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + jstr(content) + `}}]}`))
	}))
}

func jstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestChatTextURLForms(t *testing.T) {
	content := `{"plate_number":"ABC-1234"}`
	forms := []struct{ name, suffix string }{
		{"base", ""},
		{"v1", "/v1"},
		{"full", "/v1/chat/completions"},
		{"trailing slash", "/"},
	}
	for _, f := range forms {
		t.Run(f.name, func(t *testing.T) {
			srv := chatServer(t, content)
			defer srv.Close()
			c := NewClient("cloud", srv.URL+f.suffix, "k", "m", "", false, 0, "", "", "", "")
			text, err := c.chatText(context.Background(), srv.URL+f.suffix, "k", "m", "prompt", "aW1n")
			if err != nil {
				t.Fatal(err)
			}
			if text != content {
				t.Errorf("text = %q", text)
			}
		})
	}
}

func TestChatTextAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x items y"}}]}`))
	}))
	defer srv.Close()
	c := NewClient("cloud", srv.URL, "sekret", "m", "", false, 0, "", "", "", "")
	if _, err := c.chatText(context.Background(), srv.URL, "sekret", "m", "p", "aW1n"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sekret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	// 無 apiKey 時不帶 header
	gotAuth = "unset"
	if _, err := c.chatText(context.Background(), srv.URL, "", "m", "p", "aW1n"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Errorf("無 key 不應帶 Authorization: %q", gotAuth)
	}
}

func TestChatTextReasoningFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"thinking","reasoning_content":"{\"items\":[]}"}}]}`))
	}))
	defer srv.Close()
	c := NewClient("cloud", srv.URL, "k", "m", "", false, 0, "", "", "", "")
	text, err := c.chatText(context.Background(), srv.URL, "k", "m", "p", "aW1n")
	if err != nil {
		t.Fatal(err)
	}
	if text != `{"items":[]}` {
		t.Errorf("應回 reasoning_content: %q", text)
	}
}

func TestChatTextErrors(t *testing.T) {
	c := NewClient("cloud", "http://x", "k", "m", "", false, 0, "", "", "", "")
	// 非 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	if _, err := c.chatText(context.Background(), srv.URL, "k", "m", "p", "aW1n"); err == nil {
		t.Error("非 200 應回錯誤")
	}
	// 非法 JSON
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv2.Close()
	if _, err := c.chatText(context.Background(), srv2.URL, "k", "m", "p", "aW1n"); err == nil {
		t.Error("非法 JSON 應回錯誤")
	}
	// 空 choices
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv3.Close()
	if _, err := c.chatText(context.Background(), srv3.URL, "k", "m", "p", "aW1n"); err == nil {
		t.Error("空 choices 應回錯誤")
	}
	// 連線失敗
	srv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url4 := srv4.URL
	srv4.Close()
	if _, err := c.chatText(context.Background(), url4, "k", "m", "p", "aW1n"); err == nil {
		t.Error("連線失敗應回錯誤")
	}
	// 非法 baseURL → NewRequest 錯誤
	if _, err := c.chatText(context.Background(), "http://exa mple.com", "k", "m", "p", "aW1n"); err == nil {
		t.Error("非法 URL 應回錯誤")
	}
}

// ---------- Recognize / RecognizeInvoice ----------

func TestRecognizeCloud(t *testing.T) {
	srv := chatServer(t, `{"plate_number":"ABC-1234","brand_model":"Toyota"}`)
	defer srv.Close()
	c := NewClient("cloud", srv.URL, "k", "", "", false, 0, "", "", "", "")
	r, err := c.Recognize(context.Background(), "aW1n")
	if err != nil {
		t.Fatal(err)
	}
	if r.PlateNumber != "ABC-1234" || r.RawText == "" {
		t.Errorf("r = %+v", r)
	}
}

func TestRecognizeLocalOpenAI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"plate_number\":\"XYZ-9999\"}"}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient("local", "", "", "", srv.URL, false, 0, "", "", "", "")
	r, err := c.Recognize(context.Background(), "aW1n")
	if err != nil {
		t.Fatal(err)
	}
	if r.PlateNumber != "XYZ-9999" {
		t.Errorf("r = %+v", r)
	}
}

func TestRecognizeLocalCustom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			return
		}
		if r.URL.Path != "/recognize" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"plate_number":"CUST-1"}`))
	}))
	defer srv.Close()
	c := NewClient("local", "", "", "", srv.URL, false, 0, "", "", "", "custom")
	r, err := c.Recognize(context.Background(), "aW1n")
	if err != nil {
		t.Fatal(err)
	}
	if r.PlateNumber != "CUST-1" {
		t.Errorf("r = %+v", r)
	}
}

func TestRecognizeLocalDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient("local", "", "", "", url, false, 0, "", "", "", "")
	if _, err := c.Recognize(context.Background(), "aW1n"); err == nil {
		t.Fatal("服務未啟動應回錯誤")
	}
}

func TestRecognizeInvoiceCloud(t *testing.T) {
	srv := chatServer(t, `{"supplier_name":"鋒勝","order_date":"2026-10-14","items":[{"description":"機油","quantity":2,"unit_price":500}]}`)
	defer srv.Close()
	c := NewClient("cloud", srv.URL, "k", "", "", false, 0, "", "", "", "")
	doc, err := c.RecognizeInvoice(context.Background(), "aW1n")
	if err != nil {
		t.Fatal(err)
	}
	if doc.SupplierName != "鋒勝" || len(doc.Items) != 1 || doc.Items[0].GuessCategory != "引擎" {
		t.Errorf("doc = %+v", doc)
	}
}

func TestRecognizeInvoiceLocalOpenAI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[]}"}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient("local", "", "", "", srv.URL, false, 0, "", "", "", "")
	doc, err := c.RecognizeInvoice(context.Background(), "aW1n")
	if err != nil {
		t.Fatal(err)
	}
	if doc == nil {
		t.Fatal("doc 不應 nil")
	}
}

func TestNormalizeDateZeroYear(t *testing.T) {
	// y < 1000（且非民國可轉換）→ 回原字串
	if got := normalizeDate("0-1-1"); got != "0-1-1" {
		t.Errorf("normalizeDate(0-1-1) = %q", got)
	}
	// 非數字片段 → Atoi 失敗回原字串
	if got := normalizeDate("a/b/c"); got != "a/b/c" {
		t.Errorf("normalizeDate(a/b/c) = %q", got)
	}
}

func TestRecognizeInvoiceLocalDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := NewClient("local", "", "", "", url, false, 0, "", "", "", "")
	if _, err := c.RecognizeInvoice(context.Background(), "aW1n"); err == nil {
		t.Fatal("local 服務未啟動應回錯誤")
	}
}

func TestRecognizeCloudError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	c := NewClient("cloud", srv.URL, "k", "", "", false, 0, "", "", "", "")
	if _, err := c.Recognize(context.Background(), "aW1n"); err == nil {
		t.Fatal("chatText 錯誤應向上传播")
	}
}

func TestRecognizeInvoiceLocalCustomRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	c := NewClient("local", "", "", "", srv.URL, false, 0, "", "", "", "custom")
	if _, err := c.RecognizeInvoice(context.Background(), "aW1n"); err == nil {
		t.Fatal("custom 協定不支援單據辨識")
	}
}

func TestRecognizeInvoiceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	c := NewClient("cloud", srv.URL, "k", "", "", false, 0, "", "", "", "")
	if _, err := c.RecognizeInvoice(context.Background(), "aW1n"); err == nil {
		t.Fatal("chatText 錯誤應向上传播")
	}
}

// ---------- recognizeCustomLocal ----------

func TestRecognizeCustomLocalErrors(t *testing.T) {
	// 非 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_, _ = w.Write([]byte("bad gw"))
	}))
	defer srv.Close()
	c := NewClient("local", "", "", "", srv.URL, false, 0, "", "", "", "custom")
	if _, err := c.recognizeCustomLocal(context.Background(), "aW1n"); err == nil {
		t.Error("非 200 應回錯誤")
	}
	// 非法 JSON
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("xx"))
	}))
	defer srv2.Close()
	c2 := NewClient("local", "", "", "", srv2.URL, false, 0, "", "", "", "custom")
	if _, err := c2.recognizeCustomLocal(context.Background(), "aW1n"); err == nil {
		t.Error("非法 JSON 應回錯誤")
	}
	// 連線失敗
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url3 := srv3.URL
	srv3.Close()
	c3 := NewClient("local", "", "", "", url3, false, 0, "", "", "", "custom")
	if _, err := c3.recognizeCustomLocal(context.Background(), "aW1n"); err == nil {
		t.Error("連線失敗應回錯誤")
	}
	// 非法 URL
	c4 := &Client{LocalURL: "http://exa mple.com", HTTP: &http.Client{}}
	if _, err := c4.recognizeCustomLocal(context.Background(), "aW1n"); err == nil {
		t.Error("非法 URL 應回錯誤")
	}
}

// ---------- healthOK / ensureServerUp / watchProcess / Stop ----------

func TestHealthOK(t *testing.T) {
	// /health 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	c := NewClient("local", "", "", "", srv.URL, false, 0, "", "", "", "")
	if !c.healthOK() {
		t.Error("/health 200 應 true")
	}
	// /health 404 但 / 200（Ollama 形狀）
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv2.Close()
	c2 := NewClient("local", "", "", "", srv2.URL, false, 0, "", "", "", "")
	if !c2.healthOK() {
		t.Error("/ 200 應 true")
	}
	// 全 404
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) }))
	defer srv3.Close()
	c3 := NewClient("local", "", "", "", srv3.URL, false, 0, "", "", "", "")
	if c3.healthOK() {
		t.Error("全 404 應 false")
	}
	// 連線失敗
	srv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url4 := srv4.URL
	srv4.Close()
	c4 := NewClient("local", "", "", "", url4, false, 0, "", "", "", "")
	if c4.healthOK() {
		t.Error("連線失敗應 false")
	}
}

func TestEnsureServerUpAlreadyRunning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	c := NewClient("local", "", "", "", srv.URL, false, 0, "", "", "", "")
	if err := c.ensureServerUp(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.lastUsed.IsZero() {
		t.Error("應 touch 更新 lastUsed")
	}
}

func TestEnsureServerUpAutostartValidation(t *testing.T) {
	// Regression: validation used to return while holding c.mu, deadlocking
	// the next call on the same Client. The same client must stay usable.
	c := NewClient("local", "", "", "", "http://127.0.0.1:1", true, 0, "m", "model.txt", "", "")
	for i := 0; i < 2; i++ {
		if err := c.ensureServerUp(context.Background()); err == nil || !strings.Contains(err.Error(), ".gguf") {
			t.Errorf("attempt %d: 非 .gguf 應回錯誤: %v", i, err)
		}
	}
	c2 := NewClient("local", "", "", "", "http://127.0.0.1:1", true, 0, "m", filepath.Join(t.TempDir(), "nope.gguf"), "", "")
	if err := c2.ensureServerUp(context.Background()); err == nil {
		t.Error("不存在的 ModelPath 應回錯誤")
	}
	dir := t.TempDir()
	gguf := filepath.Join(dir, "m.gguf")
	_ = os.WriteFile(gguf, []byte("x"), 0o600)
	c3 := NewClient("local", "", "", "", "http://127.0.0.1:1", true, 0, "m", gguf, "mm.txt", "")
	if err := c3.ensureServerUp(context.Background()); err == nil || !strings.Contains(err.Error(), "MMPROJ") {
		t.Errorf("mmproj 非 .gguf 應回錯誤: %v", err)
	}
	c4 := NewClient("local", "", "", "", "http://127.0.0.1:1", true, 0, "m", gguf, filepath.Join(dir, "nope.gguf"), "")
	if err := c4.ensureServerUp(context.Background()); err == nil || !strings.Contains(err.Error(), "MMPROJ") {
		t.Errorf("mmproj 不存在應回錯誤: %v", err)
	}
}

func TestEnsureServerUpStartFails(t *testing.T) {
	// llama-server 未安裝 → Start 失敗（model+mmproj 皆有效時也會走到 --mmproj 組裝）
	c := NewClient("local", "", "", "", "http://127.0.0.1:59999", true, 0, "m", "", "", "")
	if err := c.ensureServerUp(context.Background()); err == nil {
		t.Fatal("llama-server 不存在應回錯誤")
	}
	dir := t.TempDir()
	gguf := filepath.Join(dir, "m.gguf")
	_ = os.WriteFile(gguf, []byte("x"), 0o600)
	mm := filepath.Join(dir, "mm.gguf")
	_ = os.WriteFile(mm, []byte("x"), 0o600)
	c2 := NewClient("local", "", "", "", "http://127.0.0.1:59997", true, 0, "m", gguf, mm, "")
	if err := c2.ensureServerUp(context.Background()); err == nil {
		t.Fatal("llama-server 不存在應回錯誤（含 --mmproj 組裝）")
	}
}

func TestStopWithJob(t *testing.T) {
	cmd := exec.Command("ping", "-n", "10", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skipf("ping 不可用: %v", err)
	}
	job := assignKillOnJobClose(cmd.Process.Pid)
	c := &Client{LocalURL: "http://x", HTTP: &http.Client{Timeout: time.Second}, stopWatch: make(chan struct{}), cmd: cmd, job: job}
	c.Stop()
	if c.job != 0 || c.cmd != nil {
		t.Errorf("Stop 後 job/cmd 應清空: job=%d cmd=%v", c.job, c.cmd)
	}
	_ = cmd.Wait()
}

func TestAssignKillOnJobCloseErrorPaths(t *testing.T) {
	// 已結束的程序：OpenProcess 失敗 → 關 job 回 0
	cmd := exec.Command("ping", "-n", "1", "127.0.0.1")
	if err := cmd.Start(); err == nil {
		pid := cmd.Process.Pid
		_ = cmd.Wait()
		if got := assignKillOnJobClose(pid); got != 0 {
			t.Skip("已結束程序仍可 assign（環境差異），跳過")
		}
	}
	// 同一程序 assign 第二個 job：Windows 允許 nested job 時會成功、
	// 不允許時回 0；兩者都可接受（error path 視系統而定）
	cmd2 := exec.Command("ping", "-n", "10", "127.0.0.1")
	if err := cmd2.Start(); err != nil {
		t.Skipf("ping 不可用: %v", err)
	}
	defer func() { _ = cmd2.Process.Kill(); _ = cmd2.Wait() }()
	job1 := assignKillOnJobClose(cmd2.Process.Pid)
	if job1 != 0 {
		defer closeJob(job1)
		_ = assignKillOnJobClose(cmd2.Process.Pid)
	}
}

func TestStartIdleWatcherKillsIdleProcess(t *testing.T) {
	// 閒置看守每 30 秒 tick 一次；lastUsed 很久以前 + IdleMin=0 → 第一次 tick 就殺掉子程序
	c := NewClient("local", "", "", "", "http://x", false, 0, "", "", "", "")
	cmd := exec.Command("ping", "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skipf("ping 不可用: %v", err)
	}
	c.cmd = cmd
	c.lastUsed = time.Now().Add(-time.Hour)
	c.startIdleWatcher()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		done := c.cmd == nil
		c.mu.Unlock()
		if done {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("閒置看守未在 40 秒內殺掉閒置程序")
}

func TestEnsureServerUpCtxCancelWhileWaiting(t *testing.T) {
	// c.cmd 已非 nil（假裝已啟動）→ 跳過 Start，進入輪詢；ctx 取消應回 ctx.Err()
	c := NewClient("local", "", "", "", "http://127.0.0.1:59998", true, 0, "m", "", "", "")
	c.cmd = exec.Command("ping", "-n", "1", "127.0.0.1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.ensureServerUp(ctx); err == nil {
		t.Fatal("ctx 取消應回錯誤")
	}
}

func TestWatchProcess(t *testing.T) {
	// cmd nil → 直接回
	c := NewClient("local", "", "", "", "http://x", false, 0, "", "", "", "")
	c.watchProcess()
	// 真程序：啟動 ping 後 watchProcess 應等待結束並清空
	c2 := NewClient("local", "", "", "", "http://x", false, 0, "", "", "", "")
	cmd := exec.Command("ping", "-n", "1", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skipf("ping 不可用: %v", err)
	}
	c2.cmd = cmd
	c2.watchProcess()
	if c2.cmd != nil {
		t.Error("程序結束後 cmd 應清空")
	}
}

func TestStartIdleWatcherAndStop(t *testing.T) {
	c := NewClient("local", "", "", "", "http://x", false, 0, "", "", "", "")
	c.startIdleWatcher()
	c.startIdleWatcher() // Once：第二次不重啟
	c.Stop()             // 關 stopWatch，goroutine 退出
	if c.stopWatch != nil {
		t.Error("Stop 後 stopWatch 應 nil")
	}
	c.Stop() // 冪等：stopWatch 已 nil 不 panic
}

func TestStopWithProcess(t *testing.T) {
	c := &Client{LocalURL: "http://x", HTTP: &http.Client{Timeout: time.Second}, stopWatch: make(chan struct{})}
	cmd := exec.Command("ping", "-n", "5", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Skipf("ping 不可用: %v", err)
	}
	c.cmd = cmd
	c.Stop()
	if c.cmd != nil {
		t.Error("Stop 後 cmd 應 nil")
	}
	// 空 Client Stop 不 panic
	(&Client{}).Stop()
}

func TestTouch(t *testing.T) {
	c := &Client{}
	c.touch()
	if c.lastUsed.IsZero() {
		t.Error("touch 應設定 lastUsed")
	}
}

func TestLocalPort(t *testing.T) {
	if got := localPort("http://127.0.0.1:9999"); got != "9999" {
		t.Errorf("port = %q", got)
	}
	if got := localPort("http://127.0.0.1"); got != "8080" {
		t.Errorf("default port = %q", got)
	}
	if got := localPort("http://[::1"); got != "8080" {
		t.Errorf("parse error port = %q", got)
	}
}

// ---------- 小工具 ----------

func TestParseReadingJSON(t *testing.T) {
	r := parseReadingJSON(`前言 {"plate_number":"A1"} 後記`)
	if r.PlateNumber != "A1" || r.RawText == "" {
		t.Errorf("r = %+v", r)
	}
	r2 := parseReadingJSON("no json here")
	if r2.RawText != "no json here" || r2.PlateNumber != "" {
		t.Errorf("r2 = %+v", r2)
	}
	r3 := parseReadingJSON(`{bad json}`)
	if r3.RawText != `{bad json}` {
		t.Errorf("r3 = %+v", r3)
	}
}

func TestReadingEmpty(t *testing.T) {
	if !(&Reading{}).empty() {
		t.Error("全空應 empty")
	}
	if (&Reading{PlateNumber: "x"}).empty() {
		t.Error("有車牌不應 empty")
	}
}

func TestTruncateDecodeEncode(t *testing.T) {
	if got := truncate("ab", 5); got != "ab" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("abcdef", 3); got != "abc" {
		t.Errorf("truncate long = %q", got)
	}
	if got := DecodeDataURL("data:image/jpeg;base64,QUJD"); got != "QUJD" {
		t.Errorf("DecodeDataURL = %q", got)
	}
	if got := DecodeDataURL("QUJD"); got != "QUJD" {
		t.Errorf("DecodeDataURL plain = %q", got)
	}
	enc, err := EncodeReader(strings.NewReader("hello"))
	if err != nil || enc != "aGVsbG8=" {
		t.Errorf("EncodeReader = %q %v", enc, err)
	}
	if _, err := EncodeReader(errReader{}); err == nil {
		t.Error("錯誤 reader 應回錯誤")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestParseInvoiceDocBadJSON(t *testing.T) {
	// 有大括號但 JSON 非法 → 空文件
	doc := parseInvoiceDoc("{oops}")
	if doc == nil || len(doc.Items) != 0 {
		t.Errorf("doc = %+v", doc)
	}
	// 保證 io 已 import（EncodeReader 用）
	_ = io.EOF
}

func TestPlatformHelpers(t *testing.T) {
	// windows 版：assignKillOnJobClose 對無效 pid 回 0；有效 pid 建立 job
	if got := assignKillOnJobClose(-1); got != 0 {
		t.Errorf("invalid pid job = %d", got)
	}
	closeJob(0) // 不 panic
	attr := hideWindowAttr()
	if sp, ok := attr.(*syscall.SysProcAttr); !ok || sp == nil {
		t.Errorf("hideWindowAttr = %#v", attr)
	}
	var job uintptr
	// 用子程序（ping）而非測試程序本身：把自己放進 KILL_ON_JOB_CLOSE job，
	// closeJob 會連測試程序一起殺掉。
	cmd := exec.Command("ping", "-n", "3", "127.0.0.1")
	if err := cmd.Start(); err == nil {
		job = assignKillOnJobClose(cmd.Process.Pid)
		if job != 0 {
			closeJob(job)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}
