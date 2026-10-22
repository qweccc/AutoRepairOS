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
	"path/filepath"
	"os/exec"
	"sort"
	"strconv"
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

// InvoiceLine 進貨單據（供應商出貨單/發票）拍照辨識出的**單行品項**。
// 一張單據通常有多行，所以 RecognizeInvoice 回傳的是切片；每行獨立帶入
// 「加入品項」表單，讓櫃檯人員逐行確認數量與單價（OCR 數字一律要人眼複核）。
type InvoiceLine struct {
	Description  string  `json:"description"`           // 品項名稱（例：機油 5W-30）
	Quantity     float64 `json:"quantity"`              // 數量；模型沒給或不合理時由上層改 1
	UnitPrice    float64 `json:"unit_price"`            // 進貨單價（給廠商的價）
	PartNumber   string  `json:"part_number,omitempty"` // 料號（選填）
	GuessCategory string `json:"guess_category,omitempty"` // 依品名關鍵字猜的零件類別（lookup code）
	RawText      string  `json:"raw_text,omitempty"`    // 該行原文，除錯用
}

// categoryKeywords 零件類別關鍵字：品名含任一關鍵字就歸到該類別。
//
// 為什麼在程式裡而不靠 lookup：lookup 的 label 只有類別本身（「引擎」、「輪胎」），
// 單據上的品名是「機油 5W-30」、「煞車皮」這種具體零件，兩者不會字面相符。
// 這張表就是「零件詞 → 類別」的對照，讓辨識結果能直接帶出類別建議，
// 櫃檯只要確認或改選，不用每個品項都手動歸類。
//
// 維護方式：新增類別或發現猜錯，在這裡加關鍵字即可（不用改 DB）。
var categoryKeywords = map[string][]string{
	"引擎": {"機油", "机油", "机油芯", "機油芯", "機油濾", "机油滤", "火星塞", "火星塞", "點火線圈", "点火线圈",
		"引擎", "引擎修缮", "引擎吊架", "水箱", "水幫浦", "水泵", "節氣門", "进气歧管", "進氣歧管",
		"排氣管", "触媒", "觸媒", "含氧感知器", "含氧传感器", "正時皮帶", "时规皮带", "時規皮帶",
		"發電機", "起动马达", "啟動馬達", "起動馬達", "皮带", "皮帶", "引擎腳", "引擎脚"},
	"電機音響": {"電瓶", "电瓶", "電池", "电池", "蓄電池", "喇叭", "音箱", "主機", "音响", "音響",
		"雨刷", "雨刷片", "燈泡", "灯泡", "大燈", "大灯", "方向燈", "方向灯", "煞車燈", "刹车灯",
		" reversing 燈", "倒車燈", "继电器", "繼電器", "保險絲", "保险丝", "點火開關", "点火开关",
		"車機", "车机", "行車記錄器", "行车记录器", "導航", "导航", "天線", "天线"},
	"車身鈑金": {"保險桿", "保险杠", "葉子板", "叶子板", "鈑金", "钣金", "車門", "车门",
		"引擎蓋", "引擎盖", "後行李箱", "后行李箱", "行李廂", "行李厢", "擋泥板", "挡泥板",
		"飾條", "饰条", "進氣壩", "进气坝", "保桿", "前保", "後保", "後保"},
	"傳動轉向": {"避震器", "避震", "減震", "减震", "三角架", "三角臂", "拉桿", "拉杆",
		"球頭", "球头", "連桿", "连杆", "方向機", "方向机", "動力方向機", "動力轉向",
		"動力转向", "傳動軸", "传动轴", "萬向接頭", "万向接头", "輪軸", "轮轴", "軸承", "轴承"},
	"變速箱": {"變速箱", "变速箱", "離合器", "离合器", "自排", "自变速箱", "手排", "手变速箱",
		"排檔", "排档", "变速箱油", "变速箱油", "CVT"},
	"烤漆": {"烤漆", "塗裝", "涂装", "漆", "塗料", "涂料", "底漆", "面漆", "鈑金烤漆", "喷漆", "噴漆"},
	"輪胎": {"輪胎", "轮胎", "鋼圈", "钢圈", "鋁圈", "铝圈", "胎", "備胎", "备胎", "氣嘴", "气嘴"},
}

// GuessCategory 依品名關鍵字猜零件類別（回 lookup 的 code；猜不到回空字串）。
// 順序：長關鍵字優先（「变速箱油」比「变速箱」具體），全表掃過取第一個命中。
func GuessCategory(description string) string {
	desc := strings.ToLower(strings.TrimSpace(description))
	if desc == "" {
		return ""
	}
	// 關鍵字由長到短比對，避免「機油」先命中而漏掉更精確的「機油濾芯」。
	type kw struct {
		word string
		cat  string
	}
	var all []kw
	for cat, words := range categoryKeywords {
		for _, w := range words {
			all = append(all, kw{strings.ToLower(w), cat})
		}
	}
	sort.Slice(all, func(i, j int) bool { return len(all[i].word) > len(all[j].word) })
	for _, k := range all {
		if strings.Contains(desc, k.word) {
			return k.cat
		}
	}
	return ""
}

// InvoiceDoc 整張進貨單據的辨識結果：供應商、日期與品項列。
// 用途是「先拍單據再開單」——櫃檯不用先選供應商，拍照後由系統帶出。
type InvoiceDoc struct {
	SupplierName string        `json:"supplier_name"` // 供應商名稱（單據抬頭）；辨識不到為空
	OrderDate    string        `json:"order_date"`    // 單據日期 YYYY-MM-DD；辨識不到為空
	Items        []InvoiceLine `json:"items"`
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

// invoicePrompt 要求模型把單據每一行列成 JSON 陣列，並帶出供應商與日期。
const invoicePrompt = `You are reading a Taiwan auto parts supplier invoice / delivery note (進貨單/出貨單/送貨單).
Return ONLY one JSON object:
{"supplier_name":"","order_date":"","items":[{"description":"","quantity":0,"unit_price":0,"part_number":""}]}
- supplier_name: the seller/supplier company name printed at the top of the document (抬頭/賣方).
  If several names appear, prefer the one issuing the invoice. Never invent one.
- order_date: the document date as YYYY-MM-DD. If it uses ROC year (民國, e.g. 115/10/14),
  convert to AD by adding 1911 to the year. Empty string if unreadable.
- items: one entry per line item:
  - description: the part name as printed (例：機油 5W-30). Never invent a name.
  - quantity: number of units (bare number, no unit). If unreadable use 1.
  - unit_price: the price charged to the garage for ONE unit, excluding tax and totals.
    If the document shows a line total instead of a unit price, divide by quantity.
  - part_number: the supplier part code if printed, otherwise empty string.
Skip header rows, subtotal, tax and total lines. Use empty string / 0 for anything you cannot read.`

// RecognizeInvoice 辨識供應商進貨單據，回傳供應商、日期與每一行品項。
// 圖為 JPEG/PNG base64（不含 data: 前綴）。沒有品項可讀時 Items 為空切片而非錯誤，
// 讓前端提示「沒辨識到品項」而不是當成系統故障。
func (c *Client) RecognizeInvoice(ctx context.Context, imageBase64 string) (*InvoiceDoc, error) {
	var text string
	var err error
	if c.Mode == "local" {
		if strings.EqualFold(c.LocalProtocol, "custom") {
			// 自訂協定（/recognize）只回行照欄位，沒有單據品項概念。
			return nil, fmt.Errorf("local 模式的自訂協定（VISION_LOCAL_PROTOCOL=custom）不支援進貨單據辨識，請改用 openai 協定或 cloud 模式")
		}
		if err := c.ensureServerUp(ctx); err != nil {
			return nil, err
		}
		text, err = c.chatText(ctx, c.LocalURL, "", c.openAIModel(), invoicePrompt, imageBase64)
	} else {
		text, err = c.chatText(ctx, c.APIURL, c.APIKey, c.cloudModel(), invoicePrompt, imageBase64)
	}
	if err != nil {
		return nil, err
	}
	return parseInvoiceDoc(text), nil
}

// cloudModel cloud 模式送給 API 的模型名稱（與 recognizeCloud 同一套 fallback）。
func (c *Client) cloudModel() string {
	if c.Model != "" {
		return c.Model
	}
	return "gemini-2.0-flash"
}

// chatText 打 OpenAI 相容的 vision 端點，回傳模型的**純文字輸出**。
// Recognize 只要行照欄位所以直接解析成 Reading；單據辨識需要自己解析
// items 陣列，因此把「拿到文字」這一步抽出來共用。
func (c *Client) chatText(ctx context.Context, baseURL, apiKey, model, prompt, imageBase64 string) (string, error) {
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
	case strings.HasSuffix(url, "/v1"):
		url += "/chat/completions"
	default:
		url += "/v1/chat/completions"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision api request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vision api status %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	// 有些推理模型把答案放在 reasoning_content，content 只放最終 JSON。
	var chat struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &chat); err != nil {
		return "", fmt.Errorf("vision api parse: %w", err)
	}
	if len(chat.Choices) == 0 {
		return "", fmt.Errorf("vision api: no choices in response")
	}
	msg := chat.Choices[0].Message
	if strings.Contains(msg.Content, "items") {
		return msg.Content, nil
	}
	if strings.Contains(msg.ReasoningContent, "items") {
		return msg.ReasoningContent, nil
	}
	return msg.Content, nil
}

// parseInvoiceDoc 從模型輸出抽出整張單據（供應商、日期、品項列）並正規化。
// 寬容處理：包 ```json 圍欄、加前言、數字給成字串、數量缺漏、民國日期。
func parseInvoiceDoc(content string) *InvoiceDoc {
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return &InvoiceDoc{}
	}
	var raw struct {
		SupplierName string `json:"supplier_name"`
		OrderDate    string `json:"order_date"`
		Items        []struct {
			Description string          `json:"description"`
			Quantity    json.RawMessage `json:"quantity"`
			UnitPrice   json.RawMessage `json:"unit_price"`
			PartNumber  string          `json:"part_number"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &raw); err != nil {
		return &InvoiceDoc{}
	}
	doc := &InvoiceDoc{
		SupplierName: strings.TrimSpace(raw.SupplierName),
		OrderDate:    normalizeDate(raw.OrderDate),
	}
	for _, it := range raw.Items {
		desc := strings.TrimSpace(it.Description)
		if desc == "" {
			continue // 沒品名的行列對車行沒有意義
		}
		q := jsonNumber(it.Quantity)
		if q <= 0 {
			q = 1 // 數量不明給 1，讓櫃檯自己改（比 0 好，0 會被表單擋掉）
		}
		doc.Items = append(doc.Items, InvoiceLine{
			Description:   desc,
			Quantity:      q,
			UnitPrice:     jsonNumber(it.UnitPrice),
			PartNumber:    strings.TrimSpace(it.PartNumber),
			GuessCategory: GuessCategory(desc),
			RawText:       desc,
		})
	}
	return doc
}

// normalizeDate 把模型回的日期正規成 YYYY-MM-DD。
// 模型有時回「115/10/14」（民國）或「2026/10/14」或「2026-10-14」，
// 這裡寬容地吃掉各種分隔符並把民國年轉成公元；解析不了就回原字串（上層自行處理）。
func normalizeDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == '-' || r == '.' || r == ' '
	})
	if len(parts) != 3 {
		return s
	}
	y, errY := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	d, errD := strconv.Atoi(parts[2])
	if errY != nil || errM != nil || errD != nil {
		return s
	}
	if y > 0 && y < 1911 { // 民國年 → 公元
		y += 1911
	}
	if y < 1000 || m < 1 || m > 12 || d < 1 || d > 31 {
		return s
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
}

// jsonNumber 寬容地解析「數字或數字字串」的 JSON 值；不是數字回 0。
func jsonNumber(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, ",", ""), "NT$", ""))
		s = strings.TrimPrefix(strings.TrimSpace(s), "$")
		if n, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return n
		}
	}
	return 0
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
// 依序試 /health 再試 /：llama-server 有 /health；Ollama 沒有 /health
// 但根路徑回 200（"Ollama is running"）——兩種服務都要認得。
func (c *Client) healthOK() bool {
	for _, path := range []string{"/health", "/"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.LocalURL, "/")+path, nil)
		if err == nil {
			if resp, err := c.HTTP.Do(req); err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					cancel()
					return true
				}
			}
		}
		cancel()
	}
	return false
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
			if filepath.Ext(c.ModelPath) != ".gguf" {
				return fmt.Errorf("VISION_MODEL_PATH 必須指向 .gguf 檔案")
			}
			if _, err := os.Stat(c.ModelPath); err != nil {
				return fmt.Errorf("VISION_MODEL_PATH 不存在：%w", err)
			}
			args = []string{"-m", c.ModelPath, "--port", port, "--host", "127.0.0.1"}
			if c.MmprojPath != "" {
				// 視覺模型必須帶投影檔，否則只會當文字模型用（回傳空/錯誤）。
				if filepath.Ext(c.MmprojPath) != ".gguf" {
					return fmt.Errorf("VISION_MMPROJ_PATH 必須指向 .gguf 檔案")
				}
				if _, err := os.Stat(c.MmprojPath); err != nil {
					return fmt.Errorf("VISION_MMPROJ_PATH 不存在：%w", err)
				}
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
	text, err := c.chatText(ctx, baseURL, apiKey, model, prompt, imageBase64)
	if err != nil {
		return nil, err
	}

	// 解析 OpenAI 形狀：choices[0].message.content 是模型回的文字（內含 JSON）。
	// 有些推理模型（如 llama.cpp 的 ornith、QwQ）把答案放在 reasoning_content，
	// content 只放最終 JSON 或甚至空白 —— chatText 已挑好含 JSON 的那一份。
	return parseReadingJSON(text), nil
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
