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

## 授權

GNU Affero General Public License v3.0，詳見 [`LICENSE`](LICENSE)。
"