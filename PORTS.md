# 開發環境 port 分配

避免服務互相衝突的約定。改動前先看這裡。

| Port | 服務 | 啟動方式 | 備註 |
|------|------|----------|------|
| 8080 | **llama-server** | 獨立程序 | 保留給本地 LLM，不要動 |
| 8090 | **AutoRepair OS** | `docker compose up -d --build` | 主服務，區網內其他裝置用這個 |
| 8099 | AutoRepair OS 測試容器 | `docker run -d --name autorepairos_test -p 8099:8080 ...` | 早期測試用，可隨時刪 |

## 檢查 port 是否被占用

```bash
netstat -ano | grep -E ":(8080|8090|8099)\s"
```

Windows 上 `docker` 與 Go 服務的 port 檢查都可直接用 `curl` 驗證：

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8090/login
```

## 為什麼不用 8080

llama-server（PID 4468）與 OpenClaw／Hermes 的 OpenAI 相容端點都佔用 8080。
Auto Repair OS 改用 8090，容器內部仍為 8080（`8090:8080` 映射），
這樣容器設定不需要跟外部 port 綁定，改外部 port 只需改 `docker-compose.yml` 一行。

## 排查

- **服務起不來 / port 衝突**：先跑上面的 `netstat` 確認目標 port 狀態
- **想確認 Auto Repair OS 是否活著**：`curl http://localhost:8090/login` 應回 200
- **改了 compose 的 port 後**：必須 `docker compose up -d --build`（帶 `--build`）才會生效
