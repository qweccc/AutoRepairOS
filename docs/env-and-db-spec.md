# 環境與資料庫規格（Env & DB Spec）

> 目的：統一說明 AutoRepair OS 的**軟體名稱、公司/品牌名、資料庫位置**，
> 以及「哪些該放 .env、哪些該放程式碼預設值」的判斷原則。

## 1. 軟體與公司名稱

| 項目 | 值 | 備註 |
|---|---|---|
| 軟體/品牌名（App Name） | `AutoRepair OS` | `internal/config/config.go` 的 `appName()`，`.env.example` 與 `telegram/templates.go` 三處一致；預設值 `AutoRepair OS` |
| 公司/商號 | 無 | 程式與文件內**沒有**明確的「股份有限公司/Inc./商號」名稱，僅以 `AutoRepair OS` 作為品牌 |
| 舊系統（來源） | `CAR35`（Delphi + Access，1997–2014） | 非本公司品牌，是待遷移的舊系統名 |
| 服務端口 | `8090`（本機/Docker） | `PORTS.md`；測試用 `8099`（`autorepairos_test`） |

**建議：** 品牌名走**環境變數**（已實作 `APP_NAME`），公司/商號若未來有正式名稱，
也建議用 `COMPANY_NAME` 環境變數統一，避免硬編碼。目前無正式名稱，保持 `AutoRepair OS` 即可。

## 2. 資料庫位置（DB Location）

程式碼裡 DB 路徑**預設為相對路徑 `data/autorepair.db`**，並**不屬於機密**，
所以**不必**塞進 `.env`；僅在需要彈性（Docker 卷掛載、本機 vs 開發）時才用 env 覆蓋。

| 路徑 | 實際位置（本機） | 用途 |
|---|---|---|
| 預設 | `file:data/autorepair.db`（`internal/config/config.go`） | 本機開發主 DB，相對路徑解析為 `data/autorepair.db` |
| db.go 實際 | `file:data/autorepair.db?_journal_mode=WAL&_busy_timeout=5000` | 加上 WAL + busy timeout（WAL 為 opt-in，見下） |
| live（平行） | `C:\Users\fr\AutoRepairOS\autorepair_live.db` | Hermes 平行舊系統，未接 main |
| live2（平行） | `C:\Users\fr\AutoRepairOS\autorepair_live2.db` | Hermes 平行舊系統 |
| data_container | `C:\Users\fr\AutoRepairOS\data_container_autorepair.db` | **Docker 實際在跑的服務 DB**（port 8090） |
| backup | `data/backups/autorepair_YYYYMMDD_HHMMSS.db` | 備份快照 |

> 注意：`config.go` 預設寫 `_busy_timeout`，`db.go` 實際用 `_journal_mode=WAL`；兩者細節略異，
> 以 `db.go` 為準（WAL 生效）。

## 3. .env 管理原則（建議）

**放 .env（機密 / 變動值）：**
- `LINE_CHANNEL_ID` / `SECRET` / `TOKEN` — LINE Messaging API
- `LINE_WEBHOOK_PUBLIC_URL` — Cloudflare Tunnel 公開地址
- `APP_NAME` / `COMPANY_NAME` — 品牌名（可選）
- `TELEGRAM_BOT_TOKEN` / `TELEGRAM_CHAT_ID` — Telegram 通知（目前 TELEGRAM_BOT_TOKEN 為空）
- `SESSION_SECRET` — session 金鑰
- `DB_PATH` — **僅當需要覆蓋**（Docker 掛載/本機差異）時；預設已內建，平常不用設

**不放 .env（程式碼預設值 / 固定值）：**
- DB 相對路徑 `data/autorepair.db` — 不機密，留 code default
- `sessionSecret` 開發預設值 `autorepair-dev-secret-change-in-production`

**結論：**
1. **軟體/公司名** → 已走 `APP_NAME` env ✅；公司名未來補 `COMPANY_NAME`。
2. **DB 位置** → **不要**強塞 `.env`（不機密、有 code default）；Docker 用 named volume + bind-mount，
   需要時才 `DB_PATH=file:/data/autorepair.db` 絕對路徑覆蓋。
3. `.gitignore` 已忽略 `.env`（`.env`、`.env.local`）✅，機密不會被 version 控制。
