package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Port              string
	DBPath            string
	EnableWAL         bool
	RunSeed           bool
	AppName           string
	DefaultOwner      string
	DefaultOwnerPwd   string
	DefaultCounter    string
	DefaultCounterPwd string
	DefaultTech       string
	DefaultTechPwd    string

	// 視覺辨識（行照/車牌 OCR）：分層混合策略。
	// VisionMode = "cloud" 走外部 AI Vision API（預設，準確率最高）；
	//             "local" 走區網本地 API（完全離線，車廠自架 Ollama/PaddleOCR 等）。
	// VisionAPIKey 只給 cloud 用；VisionLocalURL 給 local 用。兩者皆空則功能停用。
	VisionMode     string
	VisionAPIURL   string
	VisionAPIKey   string
	VisionModel    string
	VisionLocalURL string
}

// loadDotEnv 從 .env 補讀「尚未設定」的變數，讓本機 exe 與 Docker 用同一份設定檔。
//
// 為什麼需要：Docker Compose 解析 ${VAR} 時會先讀 .env，但本機跑 exe 沒有
// compose，os.Getenv 讀不到 .env 的值 —— 同一個 .env 在兩種部署下行為不一致，
// 會讓人誤以為「改了 .env 沒生效」。
//
// 規則（與 integration/line、integration/telegram 原本的做法一致）：
//  1. 只補「尚未存在」的變數（用 LookupEnv 判斷，不是 Getenv()==""）。
//     兩者差在「明確設成空字串」：測試會 os.Setenv(SECRET, "") 模擬未設定，
//     若用 Getenv 判空，.env 的真實 secret 會被補進來，讓測試測不出來。
//     Docker 注入的環境變數因此永遠優先，不會被 .env 覆蓋。
//  2. 只取 KEY=VALUE，支援引號包裹；跳過註解與空行。
//  3. 找不到 .env 就安靜返回 —— 沒有 .env 是合法的（純環境變數部署）。
//
// 找不到 .env 不會報錯：Docker 部署通常靠環境變數注入而非 .env 檔。
func loadDotEnv() {
	for _, p := range dotEnvPaths() {
		if _, err := os.Stat(p); err == nil {
			loadDotEnvFile(p)
			return
		}
	}
}

// dotEnvPaths .env 候選路徑：執行檔所在目錄優先，其次 CWD。
//
// 執行檔目錄優先是為了 release 版：release/AutoRepairOS-*.exe 會帶著自己的
// .env（build-release.ps1 產生），跑的人不一定在 repo 根啟動。
// 用 os.Executable() 而非 argv[0]：後者在捷徑或服務啟動時不可靠。
func dotEnvPaths() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), ".env"))
	}
	if wd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(wd, ".env"))
	}
	return out
}

func loadDotEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.Trim(line, "\"'")
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		if key == "" {
			continue
		}
		// LookupEnv 而非 Getenv()==""：保留「明確設成空字串」的語意，
		// 讓測試能模擬未設定，也讓 Docker 注入的空值不被 .env 覆蓋。
		if _, present := os.LookupEnv(key); !present {
			_ = os.Setenv(key, strings.TrimSpace(kv[1]))
		}
	}
}

func Load() *Config {
	// 先載入 .env，後續 os.Getenv 才會讀到。
	// 已存在的環境變數（Docker 注入、shell 設定）優先，不被 .env 覆蓋。
	loadDotEnv()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "file:data/autorepair.db?_busy_timeout=5000"
	}

	// WAL 預設開啟。
	//
	// 曾經預設關閉，理由是「Windows bind-mount 下 WAL 會 disk I/O error (4618)」。
	// 該理由已不成立：
	//   1. docker-compose 的掛載早就改成 named volume（autorepairos-data），
	//      不是 bind-mount —— compose 自己的註解也寫著「named volume 可安全使用 WAL」
	//   2. 實測一般 Windows 目錄下 PRAGMA journal_mode=WAL 正常回傳 wal
	// WAL 讓讀寫不互斥（SQLite 單寫者限制下讀取不被寫入阻塞），車廠同時有
	// 櫃檯開單、技師記工時、報表查詢時特別有感。
	//
	// 注意：DSN 的 _journal_mode=WAL 對 modernc.org/sqlite 是無效參數（實測會被
	// 靜默忽略），真正的 WAL 必須靠 db.go 的 PRAGMA，所以這裡只傳布林值。
	enableWAL := os.Getenv("DB_ENABLE_WAL")
	if enableWAL == "" {
		enableWAL = "true"
	}
	walOn := enableWAL == "true" || enableWAL == "1"

	runSeedStr := os.Getenv("RUN_SEED")
	runSeed := runSeedStr == "true" || runSeedStr == "1"

	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "AutoRepair OS"
	}

	return &Config{
		Port:              port,
		DBPath:            dbPath,
		EnableWAL:         walOn,
		RunSeed:           runSeed,
		AppName:           appName,
		DefaultOwner:      envOrDefault("DEFAULT_OWNER_USERNAME", "owner"),
		DefaultOwnerPwd:   os.Getenv("DEFAULT_OWNER_PASSWORD"),
		DefaultCounter:    envOrDefault("DEFAULT_COUNTER_USERNAME", "counter"),
		DefaultCounterPwd: os.Getenv("DEFAULT_COUNTER_PASSWORD"),
		DefaultTech:       envOrDefault("DEFAULT_TECH_USERNAME", "tech"),
		DefaultTechPwd:    os.Getenv("DEFAULT_TECH_PASSWORD"),

		VisionMode:     envOrDefault("VISION_MODE", "cloud"),
		VisionAPIURL:   os.Getenv("VISION_API_URL"),
		VisionAPIKey:   os.Getenv("VISION_API_KEY"),
		VisionModel:    os.Getenv("VISION_MODEL"),
		VisionLocalURL: os.Getenv("VISION_LOCAL_URL"),
	}
}

// VisionEnabled 視覺辨識是否已設定（有 cloud key 或 local url 任一即可用）。
func (c *Config) VisionEnabled() bool {
	if c.VisionMode == "local" {
		return c.VisionLocalURL != ""
	}
	return c.VisionAPIURL != "" && c.VisionAPIKey != ""
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (c *Config) PortInt() int {
	p, _ := strconv.Atoi(c.Port)
	if p == 0 {
		p = 8080
	}
	return p
}
