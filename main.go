package main

import (
	"context"
	"embed"
	"io/fs"
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
	"autorepairos/internal/integration/line"
	"autorepairos/internal/integration/vision"
	"autorepairos/internal/routes"
	"autorepairos/internal/seed"
	"autorepairos/web/pages"
)

// localeFiles 把字典編進 binary。exe（cwd=release/）與 docker image
// （cwd=/app）的工作目錄都沒有 locales/，只靠磁碟那份會載入失敗，
// 介面上就看到 "dashboard.title" 這種 raw key。
//
//go:embed locales/*.json
var localeFiles embed.FS

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
	// i18n：優先讀專案根目錄的 locales/（開發時改版立即生效）；exe 與
	// docker 的工作目錄沒有這個資料夾，就改用編進 binary 的內嵌字典。
	// 兩者都失敗才真的沒有字典（介面會顯示 raw key），所以只 log 不中斷。
	if err := i18n.Load("locales"); err != nil {
		// embed 的 FS 裡檔案在 locales/ 子目錄下，要先切到那一層。
		sub, subErr := fs.Sub(localeFiles, "locales")
		switch {
		case subErr != nil:
			log.Printf("i18n 字典載入失敗（找不到 locales/，內嵌字典定位也失敗）：%v / %v", err, subErr)
		default:
			if embErr := i18n.LoadEmbedded(sub); embErr != nil {
				log.Printf("i18n 字典載入失敗（磁碟與內嵌都失敗，介面將顯示原始 key）：%v / %v", err, embErr)
			} else {
				// exe（cwd=release/）與 docker（cwd=/app）本來就沒有 locales/，
				// 走內嵌字典是正常路徑，不要印成「失敗」嚇人。
				log.Printf("找不到 locales/ 目錄（exe/docker 部署的正常情況），改用內嵌字典")
			}
		}
	}
	log.Printf("AutoRepairOS 啟動：%s", versionText())
	if lineCfg, _ := line.Shared(); lineCfg != nil && lineCfg.Valid() {
		log.Printf("LINE Messaging API 已設定（webhook: /webhook/line）")
	} else {
		log.Printf("LINE 未設定完整（LINE_CHANNEL_ID/SECRET/TOKEN），客戶 LINE 功能停用")
	}

	if err := db.Init(cfg); err != nil {
		log.Fatalf("資料庫初始化失敗: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Migration 執行失敗: %v", err)
	}

	// --migrate-down-to <version>：回滾模式。先強制備份（DB+照片），
	// 再逐版相鄰回滾到目標版本；完成後直接結束，不啟動 HTTP 服務。
	// --backup-now：手動觸發一次完整備份後結束（維運／還原演練用）。
	for i, arg := range os.Args {
		if arg == "--backup-now" {
			if err := cron.DailyBackup(); err != nil {
				log.Fatalf("手動備份失敗: %v", err)
			}
			log.Printf("手動備份完成")
			os.Exit(0)
		}
		if arg == "--migrate-down-to" && i+1 < len(os.Args) {
			target := os.Args[i+1]
			log.Printf("回滾模式：目標版本 %s（先強制備份）", target)
			if err := cron.DailyBackup(); err != nil {
				log.Fatalf("回滾前備份失敗，中止回滾: %v", err)
			}
			if err := db.MigrateDownTo(target); err != nil {
				log.Fatalf("回滾失敗: %v", err)
			}
			log.Printf("回滾完成：%s", target)
			os.Exit(0)
		}
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
	listenAddr := ":" + port
	if cfg.BindIP != "" {
		listenAddr = cfg.BindIP + ":" + port
	}
	if err := http.ListenAndServe(listenAddr, handler.SecurityHeaders(handler.CSRFProtection(i18n.Middleware(r)))); err != nil {
		log.Printf("HTTP 服務結束: %v", err)
		shutdown()
		os.Exit(1)
	}
}
