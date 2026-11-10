# AutoRepairOS（繁體中文）

車行維修管理系統 — Go + SQLite + Templ，離線優先，支援單機/區網部署。

## 功能

- 客戶 / 車輛 / 工單管理
- 庫存與進貨
- 技師工時與佣金
- 收款與應收帳款
- 保險理賠、外包加工、設備管理
- 報表與經營分析
- LINE / Telegram 整合

## 本機執行

需求：Go 1.25+。

```powershell
$env:RUN_SEED = "true"
$env:DEFAULT_OWNER_PASSWORD = "<自訂密碼>"
go run .
```

預設監聽埠 `8080`。建立初始管理員後，請將 `RUN_SEED=false` 並清除 `DEFAULT_OWNER_PASSWORD`。

建置 Windows 執行檔：

```powershell
.\scripts\build-release.ps1
```

## Docker Compose 部署

```powershell
copy .env.example .env
.\deploy.cmd
```

詳細安裝與操作步驟請見 [使用指南](docs/USER_GUIDE_zh-TW.md)。

## 測試與品質

`go test ./...` 17 個 package 全 PASS，`go build ./...` 乾淨。各 package 覆蓋率：

| Package | 覆蓋率 |
|---|---|
| internal/handler | 83.4% |
| internal/service | 95.2% |
| internal/db | 97.8% |
| internal/session | 95.3% |
| internal/cron | 96.1% |
| internal/config | 98.3% |
| internal/i18n | 96.5% |
| internal/seed | 98.1% |
| internal/integration/* | 95.4–100% |
| web/pages | 82.4% |
| web/components | 91.2% |

`web/pages`、`web/components` 的 statements 由 `*_templ.go` 生成碼主導，光靠 render test 只能到 70% 左右。這裡用的手法是 **failing-writer sweep**：把 templ 的 render buffer 縮成 1 byte，讓每次 write 都 flush，生成碼裡的 per-write error 分支才談得上覆蓋（產生器 `tools/gen_writer_sweep.py`）。

介面為雙語：`locales/zh-TW.json` 與 `locales/en.json` 各 1336 個 key，完全對齊。

## 授權

GNU Affero General Public License v3.0，詳見 [`LICENSE`](LICENSE)。
"