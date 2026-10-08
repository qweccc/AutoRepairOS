package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port              string
	DBPath            string
	EnableWAL         bool
	SessionSecret     string
	RunSeed           bool
	AppName           string
	DefaultOwner      string
	DefaultOwnerPwd   string
	DefaultCounter    string
	DefaultCounterPwd string
	DefaultTech       string
	DefaultTechPwd    string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "file:data/autorepair.db?_busy_timeout=5000"
	}

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = "autorepair-dev-secret-change-in-production"
	}

	// DB_ENABLE_WAL 明確 opt-in。預設關閉：Windows bind-mount 下 WAL 會讓
	// SQLite 直接 disk I/O error (4618)，而 DSN 的 _journal_mode 參數不會生效，
	// 只會造成「設定看起來是 WAL、實際不是」的誤解。
	enableWAL := os.Getenv("DB_ENABLE_WAL") == "true" || os.Getenv("DB_ENABLE_WAL") == "1"

	runSeedStr := os.Getenv("RUN_SEED")
	runSeed := runSeedStr == "true" || runSeedStr == "1"

	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "AutoRepair OS"
	}

	return &Config{
		Port:              port,
		DBPath:            dbPath,
		EnableWAL:         enableWAL,
		SessionSecret:     sessionSecret,
		RunSeed:           runSeed,
		AppName:           appName,
		DefaultOwner:      "owner",
		DefaultOwnerPwd:   "Owner001!",
		DefaultCounter:    "counter",
		DefaultCounterPwd: "Counter001!",
		DefaultTech:       "tech",
		DefaultTechPwd:    "Tech001!",
	}
}

func (c *Config) PortInt() int {
	p, _ := strconv.Atoi(c.Port)
	if p == 0 {
		p = 8080
	}
	return p
}
