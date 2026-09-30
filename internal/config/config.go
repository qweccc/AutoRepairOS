package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port            string
	DBPath          string
	SessionSecret   string
	RunSeed         bool
	DefaultOwner    string
	DefaultOwnerPwd string
	DefaultCounter  string
	DefaultCounterPwd string
	DefaultTech     string
	DefaultTechPwd  string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "file:data/autorepair.db?_journal_mode=WAL&_busy_timeout=5000"
	}

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		sessionSecret = "autorepair-dev-secret-change-in-production"
	}

	runSeedStr := os.Getenv("RUN_SEED")
	runSeed := runSeedStr == "true" || runSeedStr == "1"

	return &Config{
		Port:            port,
		DBPath:          dbPath,
		SessionSecret:   sessionSecret,
		RunSeed:         runSeed,
		DefaultOwner:    "owner",
		DefaultOwnerPwd: "Owner001!",
		DefaultCounter:  "counter",
		DefaultCounterPwd: "Counter001!",
		DefaultTech:     "tech",
		DefaultTechPwd:  "Tech001!",
	}
}

func (c *Config) PortInt() int {
	p, _ := strconv.Atoi(c.Port)
	if p == 0 {
		p = 8080
	}
	return p
}
