// Package vision 提供行照/車牌的視覺辨識（OCR）能力。
//
// 分層混合策略（對應 docs/DEVELOPMENT.md）：
//   - cloud 模式：呼叫外部 AI Vision API（預設 Gemini，也可接任何 OpenAI 相容
//     的 vision endpoint）。準確率最高、開發最快，但需連網。
//   - local 模式：呼叫區網本地 API 端點（車廠自架 Ollama+LLaVA、PaddleOCR 等）。
//     完全離線，API 形狀由本地服務決定，這裡只做「送 base64 圖、收 JSON」的轉發。
//
// 兩種模式都回傳相同的結構化結果 PlateReading，讓上層 handler 不用分支。
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Reading 行照/車牌辨識的結構化結果。
// 欄位對應台灣行車執照；辨識不到的空著，由上層決定要不要覆蓋既有值。
type Reading struct {
	PlateNumber    string `json:"plate_number"`
	BrandModel     string `json:"brand_model"`
	VIN            string `json:"vin"`
	EngineNumber   string `json:"engine_number"`
	OwnerName      string `json:"owner_name"`
	OwnerAddress   string `json:"owner_address"`
	OwnerIDNumber  string `json:"owner_id_number"`
	FuelType       string `json:"fuel_type"`
	Displacement   string `json:"displacement"`
	ManufactureDate string `json:"manufacture_date"`
	LicenseIssueDate string `json:"license_issue_date"`
	SeatingCapacity  string `json:"seating_capacity"`
	RawText        string `json:"raw_text,omitempty"` // 原始回傳，除錯用
}

// Client 依模式分派到 cloud 或 local。
type Client struct {
	Mode     string // "cloud" | "local"
	APIURL   string
	APIKey   string
	Model    string
	LocalURL string
	HTTP     *http.Client

	// 自動管理本地 llama-server（VISION_AUTOSTART=true 時啟用，僅 local 模式有效）：
	// 第一次辨識時若服務沒開，後端自動背景啟動 llama-server（載入約 10-60 秒），
	// 用完閒置 IdleMin 分鐘後自動關閉程序。只管理「自己啟動的」程序，
	// 使用者手動開的服務（如 start-qwen-vision.bat）不會被動到。
	AutoStart bool
	IdleMin   int
	HFModel   string // llama-server -hf 的模型規格（無 ModelPath 時用；會連 HF 驗證）
	ModelPath string // 本地 GGUF 主模型檔（優先；離線部署不碰網路）
	MmprojPath string // 視覺投影檔 mmproj（視覺模型必備，搭配 ModelPath）
	LocalProtocol string // local 模式協定："openai"（預設，llama-server）或 "custom"（自訂 /recognize API）

	mu        sync.Mutex
	cmd       *exec.Cmd
	job       uintptr // Windows：cmd 所屬的 kill-on-close job（app 死亡時作業系統終止 cmd）
	lastUsed  time.Time
	watchOnce sync.Once
	stopWatch chan struct{}
}

// NewClient 由 config 建立。若未設定（兩者皆空）回傳 nil，上層据此停用功能。
// autoStart/idleMin/hfModel 僅 local 模式使用：服務沒開時由後端自動啟動 llama-server，
// 閒置 idleMin 分鐘後自動關閉。autoStart=false 時服務沒開只回錯誤（不動程序）。
// modelPath 有值時優先吃本地 GGUF（離線不碰網路）；mmprojPath 是視覺模型必備的投影檔。
func NewClient(mode, apiURL, apiKey, model, localURL string, autoStart bool, idleMin int, hfModel, modelPath, mmprojPath, localProtocol string) *Client {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "cloud"
	}
	// 未設定就沒有 client：cloud 缺 key，或 local 缺 url。
	if mode == "local" {
		if localURL == "" {
			return nil
		}
	} else {
		if apiURL == "" || apiKey == "" {
			return nil
		}
	}
	if idleMin <= 0 {
		idleMin = 10
	}
	if hfModel == "" {
		hfModel = "unsloth/Qwen2.5-VL-3B-Instruct-GGUF:Q4_K_M"
	}
	localProtocol = strings.ToLower(strings.TrimSpace(localProtocol))
	if localProtocol != "custom" {
		localProtocol = "openai"
	}
	return &Client{
		Mode:        mode,
		APIURL:      apiURL,
		APIKey:      apiKey,
		Model:       model,
		LocalURL:    localURL,
		HTTP:        &http.Client{Timeout: 120 * time.Second},
		AutoStart:   autoStart,
		IdleMin:     idleMin,
		HFModel:     hfModel,
		ModelPath:   modelPath,
		MmprojPath:  mmprojPath,
		LocalProtocol: localProtocol,
		stopWatch:   make(chan struct{}),
	}
}

// Recognize 辨識一張行照/車牌圖（JPEG/PNG base64，不含 data: 前綴）。
// 回傳結構化欄位；辨識失敗回傳 error（上層轉成 502 給前端）。
func (c *Client) Recognize(ctx context.Context, imageBase64 string) (*Reading, error) {
	if c.Mode == "local" {
		if err := c.ensureServerUp(ctx); err != nil {
			return nil, err
		}
		// 我們管理的 llama-server 就是 OpenAI 相容端點（/v1/chat/completions），
		// 直接走同一條路；自訂協定（VISION_LOCAL_PROTOCOL=custom）才用舊的 /recognize。
		if strings.EqualFold(c.LocalProtocol, "custom") {
			return c.recognizeCustomLocal(ctx, imageBase64)
		}
		return c.recognizeOpenAI(ctx, c.LocalURL, "", c.openAIModel(), cloudPrompt, imageBase64)
	}
	return c.recognizeCloud(ctx, imageBase64)
}

// openAIModel local 模式送給 llama-server 的 model 名稱（llama.cpp 實際上
// 只用載入的那個模型，此欄位只是為了符合 OpenAI 請求格式；取 HFModel 的主名）。
func (c *Client) openAIModel() string {
	if c.Model != "" {
		return c.Model
	}
	name := c.HFModel
	if i := strings.Index(name, ":"); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		name = "local"
	}
	return name
}

// recognizeCustomLocal 舊的自訂本地 API 協定：POST {image_base64} → Reading JSON。
// 給車廠自架的 Ollama/PaddleOCR 包裝服務用（VISION_LOCAL_PROTOCOL=custom）。
func (c *Client) recognizeCustomLocal(ctx context.Context, imageBase64 string) (*Reading, error) {
	payload, _ := json.Marshal(map[string]string{"image_base64": imageBase64})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(c.LocalURL, "/")+"/recognize", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("local vision request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local vision status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var r Reading
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("local vision parse: %w", err)
	}
	r.RawText = string(raw)
	return &r, nil
}

// healthOK 快速探測本地視覺服務是否在跑（1 秒 timeout）。
func (c *Client) healthOK() bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.LocalURL, "/")+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// touch 記錄最近使用時間（閒置計時起點）。
func (c *Client) touch() {
	c.mu.Lock()
	c.lastUsed = time.Now()
	c.mu.Unlock()
}

// ensureServerUp local 模式專用：服務沒開時依 AutoStart 決定行為。
//   - 已在跑（使用者手動開的或我們起的）→ 直接用，不動它
//   - 沒開 + AutoStart=false → 回錯誤，提示手動啟動或開啟 AUTOSTART
//   - 沒開 + AutoStart=true  → 背景啟動 llama-server，輪詢 /health 直到就緒（最多 150 秒）
func (c *Client) ensureServerUp(ctx context.Context) error {
	if c.healthOK() {
		c.touch()
		return nil
	}
	if !c.AutoStart {
		return fmt.Errorf("本地視覺服務未啟動（%s）。請執行 start-qwen-vision.bat，或在 .env 設 VISION_AUTOSTART=true 讓系統自動管理", c.LocalURL)
	}

	c.mu.Lock()
	if c.cmd == nil {
		port := localPort(c.LocalURL)
		var args []string
		if c.ModelPath != "" {
			// 離線優先：直接吃本地 GGUF，不碰 HuggingFace（防火牆會擋 -hf 的驗證請求）。
			args = []string{"-m", c.ModelPath, "--port", port, "--host", "127.0.0.1"}
			if c.MmprojPath != "" {
				// 視覺模型必須帶投影檔，否則只會當文字模型用（回傳空/錯誤）。
				args = append(args, "--mmproj", c.MmprojPath)
			}
		} else {
			args = []string{"-hf", c.HFModel, "--port", port, "--host", "127.0.0.1"}
		}
		cmd := exec.Command("llama-server", args...)
		// Windows：不彈出黑色視窗；離線旗標讓 -hf 模式也只讀快取不下載。
		if attr, ok := hideWindowAttr().(*syscall.SysProcAttr); ok {
			cmd.SysProcAttr = attr
		}
		cmd.Env = append(os.Environ(), "HF_HUB_OFFLINE=1")
		if err := cmd.Start(); err != nil {
			c.mu.Unlock()
			return fmt.Errorf("自動啟動本地視覺服務失敗（請確認 llama-server 已安裝且在 PATH）：%w", err)
		}
		c.cmd = cmd
		c.job = assignKillOnJobClose(cmd.Process.Pid)
		go c.watchProcess()
	}
	c.mu.Unlock()

	// 輪詢 /health 直到模型載入完成（CPU 上 3B 模型約 10-60 秒）。
	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		if c.healthOK() {
			c.touch()
			c.startIdleWatcher()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("本地視覺服務啟動逾時（模型載入超過 150 秒）")
}

// watchProcess 等待我們啟動的 llama-server 結束；結束就清掉 c.cmd，
// 下次辨識會重新 ensureServerUp（可能重新啟動）。
func (c *Client) watchProcess() {
	c.mu.Lock()
	cmd := c.cmd
	c.mu.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Wait()
	c.mu.Lock()
	if c.cmd == cmd {
		c.cmd = nil
	}
	c.mu.Unlock()
}

// startIdleWatcher 啟動閒置看守（只啟動一次）：每 30 秒檢查，
// 超過 IdleMin 分鐘沒人用就關掉「我們啟動的」llama-server。
func (c *Client) startIdleWatcher() {
	c.watchOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-c.stopWatch:
					return
				case <-ticker.C:
					c.mu.Lock()
					idle := !c.lastUsed.IsZero() && time.Since(c.lastUsed) > time.Duration(c.IdleMin)*time.Minute
					cmd := c.cmd
					c.mu.Unlock()
					if idle && cmd != nil && cmd.Process != nil {
						_ = cmd.Process.Kill()
						c.mu.Lock()
						if c.cmd == cmd {
							c.cmd = nil
						}
						c.mu.Unlock()
						return
					}
				}
			}
		}()
	})
}

// localPort 從 LocalURL 取出 port（給 llama-server --port 用）；取不到回 8080。
func localPort(localURL string) string {
	u, err := url.Parse(strings.TrimRight(localURL, "/"))
	if err == nil && u.Port() != "" {
		return u.Port()
	}
	return "8080"
}

// Stop 在 app 關閉時呼叫：關掉閒置看守並結束「我們啟動的」llama-server，
// 避免主程序結束後留下孤兒程序佔用記憶體。
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopWatch != nil {
		close(c.stopWatch)
		c.stopWatch = nil
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		c.cmd = nil
	}
	// closeJob 也會終止 job 裡的程序（KILL_ON_JOB_CLOSE），雙保險。
	if c.job != 0 {
		closeJob(c.job)
		c.job = 0
	}
}

// cloudPrompt 要求模型只回 JSON，欄位對應 Reading。
const cloudPrompt = `You are reading a Taiwan vehicle registration document (行車執照) or license plate.
Extract these fields and return ONLY a JSON object (no markdown, no explanation):
{"plate_number":"","brand_model":"","vin":"","engine_number":"","owner_name":"",
"owner_address":"","owner_id_number":"","fuel_type":"","displacement":"",
"manufacture_date":"","license_issue_date":"","seating_capacity":""}
Use empty string for any field you cannot read. plate_number is the license plate like ABC-1234.`

// recognizeCloud 走 OpenAI 相容的 vision chat completions 端點（cloud 模式）。
func (c *Client) recognizeCloud(ctx context.Context, imageBase64 string) (*Reading, error) {
	model := c.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return c.recognizeOpenAI(ctx, c.APIURL, c.APIKey, model, cloudPrompt, imageBase64)
}

// recognizeOpenAI 打任何 OpenAI 相容的 vision chat completions 端點。
// baseURL 可只給 base（自動補 /v1/chat/completions）、含 /v1、或完整端點。
// cloud 模式（Gemini 等）與 local 模式（我們管理的 llama-server）共用。
func (c *Client) recognizeOpenAI(ctx context.Context, baseURL, apiKey, model, prompt, imageBase64 string) (*Reading, error) {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "text", "text": prompt},
					{"type": "image_url", "image_url": map[string]string{
						"url": "data:image/jpeg;base64," + imageBase64,
					}},
				},
			},
		},
		"temperature": 0,
	}
	body, _ := json.Marshal(payload)

	url := strings.TrimRight(baseURL, "/")
	switch {
	case strings.Contains(url, "/chat/completions"):
		// 已是完整端點，直接用
	case strings.HasSuffix(url, "/v1"):
		// 已含 /v1（如 http://host/v1 或 .../v1beta/openai），只補 /chat/completions
		url += "/chat/completions"
	default:
		// 只給了 base（如 http://host:8080），補完整路徑
		url += "/v1/chat/completions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vision api request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vision api status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	// 解析 OpenAI 形狀：choices[0].message.content 是模型回的文字（內含 JSON）。
	// 有些推理模型（如 llama.cpp 的 ornith、QwQ）把答案放在 reasoning_content、
	// content 只放最終 JSON 或甚至空白。這裡兩個都收，取「含 JSON」的那個。
	var chat struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &chat); err != nil {
		return nil, fmt.Errorf("vision api parse: %w", err)
	}
	if len(chat.Choices) == 0 {
		return nil, fmt.Errorf("vision api: no choices in response")
	}
	msg := chat.Choices[0].Message
	// 優先 content（正式輸出），content 沒有 JSON 才退回 reasoning_content。
	reading := parseReadingJSON(msg.Content)
	if reading.empty() {
		if alt := parseReadingJSON(msg.ReasoningContent); !alt.empty() {
			reading = alt
		}
	}
	return reading, nil
}

// empty 判斷是否完全沒辨識到任何欄位（全空字串）。
// 用來在 content/reasoning_content 之間選擇「有內容」的那個。
func (r *Reading) empty() bool {
	return r.PlateNumber == "" && r.BrandModel == "" && r.VIN == "" &&
		r.EngineNumber == "" && r.OwnerName == "" && r.OwnerAddress == "" &&
		r.OwnerIDNumber == "" && r.FuelType == "" && r.Displacement == "" &&
		r.ManufactureDate == "" && r.LicenseIssueDate == "" && r.SeatingCapacity == ""
}

// parseReadingJSON 從模型回傳文字中抽出 JSON 並解析成 Reading。
// 模型有時會包 ```json ... ``` 或加前言，這裡寬容地取第一個 { 到最後一個 }。
func parseReadingJSON(content string) *Reading {
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end < 0 || end <= start {
		return &Reading{RawText: content}
	}
	var r Reading
	if err := json.Unmarshal([]byte(content[start:end+1]), &r); err != nil {
		return &Reading{RawText: content}
	}
	r.RawText = content
	return &r
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// DecodeDataURL 把前端的 data:image/...;base64,XXXX 轉成純 base64。
// 前端 <input type=file> 用 FileReader.readAsDataURL 得到的就是這種格式。
func DecodeDataURL(dataURL string) string {
	if i := strings.Index(dataURL, ","); i >= 0 {
		return dataURL[i+1:]
	}
	return dataURL
}

// EncodeReader 把任意圖片 reader 編成 base64（給測試或非 dataURL 來源用）。
func EncodeReader(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 10<<20)) // 10MB 上限
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
