package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/go-chi/chi/v5"

	"autorepairos/internal/config"
	"autorepairos/internal/cron"
	"autorepairos/internal/db"
	"autorepairos/internal/handler"
	"autorepairos/internal/i18n"
	"autorepairos/internal/integration/vision"
	"autorepairos/internal/routes"
	"autorepairos/internal/seed"
	"autorepairos/web/pages"
)

// 建置資訊，由 build-release.ps1 以 -ldflags -X 注入。
// 本地 go run / go build 沒有注入時保持空字串，頁面顯示「開發版」。
var (
	buildTime = ""
	gitCommit = ""
)

// versionText 回傳「版本 + 建置時間 + Git 修訂」的顯示字串。
// 用在啟動訊息與頁尾，讓現場回報問題時能直接說出跑的是哪一版。
func versionText() string {
	if buildTime == "" && gitCommit == "" {
		return "開發版"
	}
	s := ""
	if buildTime != "" {
		s = "建置 " + buildTime
	}
	if gitCommit != "" {
		if s != "" {
			s += " / "
		}
		s += "git " + gitCommit
	}
	return s
}

func main() {
	cfg := config.Load()
	if cfg.AppName != "" {
		pages.AppName = cfg.AppName
	}
	// i18n：啟動載入 dictionaries（locales/ 在專案根目錄）；失敗只 log 不中斷。
	if err := i18n.Load("locales"); err != nil {
		log.Printf("i18n 字典載入失敗（將使用預設語系）：%v", err)
	}
	log.Printf("AutoRepairOS 啟動：%s", versionText())

	if err := db.Init(cfg); err != nil {
		log.Fatalf("資料庫初始化失敗: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Migration 執行失敗: %v", err)
	}

	// 檢查 lookup 分頁籤與資料是否配套（只 log 警告，不中斷啟動）。
	handler.CheckLookupCategories()

	// 視覺辨識（行照/車牌 OCR）：依 config 建立 client，未設定則為 nil（功能停用）。
	if vc := vision.NewClient(cfg.VisionMode, cfg.VisionAPIURL, cfg.VisionAPIKey,
		cfg.VisionModel, cfg.VisionLocalURL, cfg.VisionAutoStart, cfg.VisionIdleMin,
		cfg.VisionHFModel, cfg.VisionModelPath, cfg.VisionMmprojPath, cfg.VisionLocalProtocol); vc != nil {
		handler.SetVisionClient(vc)
		if vc.Mode == "local" && vc.AutoStart {
			log.Printf("視覺辨識已啟用：模式=local（自動管理 llama-server，閒置 %d 分鐘自動關閉）", vc.IdleMin)
		} else {
			log.Printf("視覺辨識已啟用：模式=%s", vc.Mode)
		}
	} else {
		log.Printf("視覺辨識未啟用（.env 未設定 VISION_API_URL/VISION_API_KEY 或 VISION_LOCAL_URL）")
	}

	if cfg.RunSeed {
		if err := seed.Run(cfg); err != nil {
			log.Fatalf("Seed 執行失敗: %v", err)
		}
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			rc := chi.RouteContext(req.Context())
			if rc != nil {
				for i := 0; i < len(rc.URLParams.Keys); i++ {
					if i < len(rc.URLParams.Values) {
						req.SetPathValue(rc.URLParams.Keys[i], rc.URLParams.Values[i])
					}
				}
			}
			next.ServeHTTP(w, req)
		})
	})

	// 路由已遷移至 internal/routes
	r.Mount("/", routes.New(routes.Deps{}))

	port := cfg.Port
	if port == "" {
		port = "8080"
	}

	handler.RegisterStatic(r)

	// 啟動排程器（通知佇列、每日備份、保養到期掃描）
	scheduler, err := cron.New()
	if err != nil {
		log.Printf("排程器初始化失敗（功能停用）: %v", err)
	} else {
		scheduler.Start()
		defer scheduler.Stop()
	}

	telegramCtx, telegramCancel := context.WithCancel(context.Background())
	var telegramWG sync.WaitGroup
	telegramWG.Add(1)
	go handler.StartTelegramBinding(telegramCtx, &telegramWG)

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			log.Printf("正在關閉服務，清理資源…")
			telegramCancel()
			telegramWG.Wait()
			handler.StopVisionClient()
			if scheduler != nil {
				scheduler.Stop()
			}
			db.Close()
		})
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("收到關閉訊號（Ctrl+C / SIGTERM）…")
		shutdown()
		os.Exit(0)
	}()

	log.Printf("%s 啟動中，監聽 port: %s", cfg.AppName, port)
	if err := http.ListenAndServe(":"+port, handler.CSRFProtection(i18n.Middleware(r))); err != nil {
		log.Printf("HTTP 服務結束: %v", err)
		shutdown()
		os.Exit(1)
	}
}
