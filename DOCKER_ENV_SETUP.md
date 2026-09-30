# AutoRepair OS - 車行維修管理系統 Docker 環境

本機目前無 Docker。環境建立方式如下（任選）：

## 方案 A：Docker Desktop for Windows（推薦，最簡單）
1. 下載：https://www.docker.com/products/docker-desktop/
2. 安裝時勾選 WSL2 backend（若 Windows 10/11 且支援虛擬化）
3. 安裝後開啟 Docker Desktop，等狀態列顯示「Engine running」
4. 在專案目錄開終端機執行：
   ```bash
   docker compose up --build
   ```
5. 服務會跑在 `http://localhost:8080`

## 方案 B：純 WSL2 + Docker Engine（若不想裝 Desktop）
1. Windows 功能開啟「Windows Subsystem for Linux」
2. 安裝 Ubuntu（Microsoft Store 或 `wsl --install -d Ubuntu`）
3. 在 WSL 執行：
   ```bash
   sudo apt update && sudo apt install -y docker.io docker-compose
   sudo usermod -aG docker $USER
   # 重新登入 WSL 後
   docker compose up --build
   ```

## 方案 C：本機 exe 直接跑（跳過 Docker，已有可執行檔）
```bash
cd C:/Users/fr/AutoRepairOS
./autrepairos.exe
```
服務跑在 `http://localhost:8080`（或 8092，若 8080 被佔）。

## 方案 D：臨時 Docker 環境（遠端 / 容器編排）
若本機真的無法裝 Docker，可改用：
- GitHub Codespaces（免費用）
- 租一台 Linux VPS（最低 1GB RAM 足夠跑 Go + SQLite）
- 用 `docker compose` 部署上去

## 確認環境
目前檢測結果：
- Docker: 未安裝
- WSL2: 未安裝或未掛載
- Go: 本機有但 agent 執行環境找不到（可能是 PATH 問題）

若要我幫忙裝 Docker Desktop 或 WSL2，請明確說要哪個方案，我會開終端機安裝。
