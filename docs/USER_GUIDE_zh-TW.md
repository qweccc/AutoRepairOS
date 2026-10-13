# AutoRepairOS 使用者安裝與操作手冊

本手冊說明如何在 Windows 11 使用 Docker 或獨立 Windows `.exe` 安裝 AutoRepairOS、建立第一個資料庫與管理員帳號，以及日常操作、備份和還原。每家車廠應使用自己的部署設定和資料庫，不要共用資料庫或資料目錄。

## Windows 11 獨立 EXE 安裝（不使用 Docker）

此方式直接在 Windows 執行 Go 程式，適合單機或可信任的內部網路。建議正式營運優先使用後面的 Docker 安裝方式，因其資料 volume 與升級/備份流程較明確。

### 準備程式檔案

使用日期或版本最新的 release 套件，內含 `AutoRepairOS-日期或版本.exe`、`web` 資料夾、`.env` 設定檔及 `start.cmd` 啟動器。不要只複製 exe，也不要使用根目錄中的舊 `autorepairos.exe`。從原始碼自行建置時，在專案目錄執行：

```powershell
.\scripts\build-release.ps1
```

建置結果在 `release` 資料夾。將整個 `release` 資料夾放在不會被清除的位置，例如 `C:\AutoRepairOS`。資料庫會建立在 release 資料夾下的 `data\autorepair.db`；每家車廠使用自己的安裝資料夾，不要共用 `data` 目錄。

### 第一次啟動並建立資料庫

**沒有內建的預設登入密碼。** 第一次啟動時由安裝者自行設定 `DEFAULT_OWNER_USERNAME` 和 `DEFAULT_OWNER_PASSWORD`；以下範例帳號是 `owner`，密碼則是你透過安全提示輸入的自訂密碼。不要使用或期待 `owner/owner`、`admin/admin` 等共用預設帳密。

在 `C:\AutoRepairOS` 開啟 `.env`，設定第一次啟動所需參數：

```powershell
PORT=8080
RUN_SEED=true
DEFAULT_OWNER_USERNAME=owner
# 填入至少12字元的專屬強密碼
DEFAULT_OWNER_PASSWORD=
```

儲存後雙擊 `start.cmd`。啟動器會讀取同目錄 `.env`，檢查連接埠、初始化密碼與 `web\index.html`，再啟動最新日期/版本的 exe。瀏覽器前往 `http://localhost:8080/login`（若 `PORT` 改成其他值，請使用該 port），以 `.env` 中的管理員帳號和密碼登入。首次啟動會自動建立 `data` 資料夾、SQLite 資料庫 `data\autorepair.db`、schema migrations 及管理員帳號；不需安裝 SQLite 或手動建立資料表。

若是全新的資料庫，必須在第一次啟動前設 `RUN_SEED=true` 並設定至少 12 字元的管理員密碼，否則啟動器會停止並提示修正 `.env`。若資料庫內的管理員帳號已存在，seed 會略過該帳號，不會用環境變數覆寫或重設它的密碼；請勿靠刪除資料庫來重設密碼。

完成初始設定並確認能登入後，在啟動中的視窗按 `Ctrl+C` 停止服務，再編輯 `.env`：

```dotenv
RUN_SEED=false
DEFAULT_OWNER_PASSWORD=
```

之後每次在安裝資料夾雙擊 `start.cmd` 即可。常用設定都可在 `.env` 修改：

```dotenv
PORT=8091
APP_NAME=我的維修廠
RUN_SEED=false
```

`PORT` 改成 8091 後，瀏覽器使用 `http://localhost:8091/login`。`DEFAULT_OWNER_PASSWORD` 會以明文保存在 `.env`，首次登入後務必清空，並限制安裝資料夾的 Windows 存取權限。程式會持續在啟動視窗執行，關閉視窗或按 `Ctrl+C` 即停止服務。

### EXE 模式資料管理、備份與更新

- 重要資料在安裝目錄的 `data` 資料夾；工單照片在 `data\photos`。備份或搬移時兩者都要保留。
- 備份前先在執行程式的 PowerShell 按 `Ctrl+C` 停止服務，再將 `data` 整個資料夾複製到另一個安全位置。例如：`Copy-Item .\data "C:\AutoRepairOS-backups\data-20261012" -Recurse`。完成後可用相同工作目錄重新啟動程式。
- 還原前先停止程式，保留目前 `data` 的副本，再以已核對過的備份 `data` 資料夾還原；確認其中有 `autorepair.db`，若有照片也應有 `photos`，再啟動程式。
- 更新時先完整備份，將新版日期/版本 exe 和其配套 `web` 資料夾放入同一安裝目錄，再由該目錄啟動新版。**不要刪除或覆蓋 `data` 資料夾**；新版啟動時會自動套用尚未執行的 migrations。
- release 套件的 `start.cmd` 會透過 `start.ps1` 讀取 `.env`，再以環境變數啟動 exe；若直接雙擊 exe，程式本身不會載入 `.env`。日常啟動請使用 `start.cmd`，並保持 `RUN_SEED=false`、初始化密碼留白。
- 忘記管理員密碼時，不要刪除資料庫或重做初始化；先備份資料，再由系統維運人員處理。

### EXE 模式必備工具（Windows 11 單機）

| 工具 | 用途 | 取得方式 |
|---|---|---|
| DB Browser for SQLite | 直接看/修改 `data\autorepair.db` | 官網下載安裝 |
| llama.cpp（llama-server） | 本地視覺辨識 LLM（行照/車牌拍照辨識） | `winget install ggml.llamacpp` |
| start-qwen-vision.bat | 手動啟動視覺服務（Qwen2.5-VL-3B on 8092） | 隨 release 提供 |

**視覺辨識預設全自動**（`.env` 已設 `VISION_AUTOSTART=true`）：在車輛表單按「拍照辨識行照」時，系統會自動啟動本地 LLM、辨識完閒置 10 分鐘自動關閉，**不需要手動跑任何程式**。第一次辨識要等模型載入（約 10-60 秒），之後就快。

想手動管理或節省開銷時，可在 `.env` 切換（詳見 `.env.example` 的 VISION 區塊註解）。

### 視覺模型存放位置（兩種模式）

| 模式 | 模型放哪 | 大小 | 換電腦/重灌怎麼辦 |
|---|---|---|---|
| Windows exe | HuggingFace 快取：`C:\Users\<你>\.cache\huggingface\hub\models--unsloth--Qwen2.5-VL-3B-Instruct-GGUF\snapshots\<hash>\`（`.env` 的 `VISION_MODEL_PATH`/`VISION_MMPROJ_PATH` 就指到這裡） | 約 3.1GB（主模型 1.8GB + mmproj 投影檔 1.2GB） | 有網路：重跑一次 start-qwen-vision.bat 或讓 autostart 重新拉；無網路的離線環境：把整個 `models--unsloth--Qwen2.5-VL-3B-Instruct-GGUF` 資料夾複製過去 |
| Docker | `ollama-data` named volume（Docker 管理的磁碟區，**不在專案資料夾裡**；首次要 `docker exec -it ollama_local ollama pull qwen2.5vl:3b`） | 約 3GB | 升級/重建容器不會重拉；只有 `docker compose down -v` 才會刪掉 volume 裡的模型 |

注意：模型是「可以重抓」的資料，日常備份不需要包含它；但離線部署的車廠如果無法重新下載，換機時要連模型資料夾一起搬。

## 1. 安裝前準備

Windows 11 建議使用 **Docker Desktop + WSL2 backend + Linux containers**。請先安裝並啟動 Docker Desktop，完成首次設定並確認 Docker Desktop 使用 Linux containers。AutoRepairOS 的容器映像是 Linux 映像，不是原生 Windows container。

Linux VPS 也可使用 Docker Engine、Docker Compose v2 和 `curl`。在專案根目錄複製並編輯設定，再啟動：

```sh
cp .env.example .env
${EDITOR:-nano} .env
bash scripts/deploy.sh
```

Linux 與 Windows 使用相同的初始化和資料庫建立方式；請同樣設定唯一的 `COMPOSE_PROJECT_NAME`、連接埠、`RUN_SEED=true` 和至少 12 字元的專屬管理員密碼。

第一次部署前準備：

- 取得 AutoRepairOS 專案資料夾，並在該資料夾開啟 PowerShell。
- 確認 Docker Desktop 正在執行。`deploy.cmd` 會檢查 Docker、Compose、WSL2 和 Linux containers；必要時會提示是否安裝相依元件。
- 為這家車廠準備一個獨立的專案名稱、主機連接埠和管理員密碼。

## 2. 設定並啟動

在專案根目錄執行：

```powershell
Copy-Item .env.example .env
notepad .env
```

在 `.env` 至少設定以下值：

```dotenv
COMPOSE_PROJECT_NAME=repairshop_a
HOST_PORT=8090
RUN_SEED=true
DEFAULT_OWNER_USERNAME=owner
# 必須填入專屬且至少12字元的強密碼
DEFAULT_OWNER_PASSWORD=
```

- `COMPOSE_PROJECT_NAME`：這家車廠專用的名稱，只使用小寫英數字、底線或連字號，且以英數字開頭。不同車廠必須使用不同名稱。
- `HOST_PORT`：瀏覽器連線使用的主機連接埠；若 8090 已被使用，可改成其他未占用的連接埠。
- `RUN_SEED=true`：只在第一次建立資料庫時啟用初始化。
- `DEFAULT_OWNER_USERNAME` 和 `DEFAULT_OWNER_PASSWORD`：第一次登入的系統管理員帳號與密碼。部署程式要求密碼至少 12 字元，請勿使用範例值或與其他系統共用的密碼。
- `DEFAULT_COUNTER_PASSWORD`、`DEFAULT_TECH_PASSWORD`：可先留白；留白時不會以初始化程序建立這兩種帳號。登入後可由管理員在「員工」頁面新增人員。

儲存 `.env` 後啟動：

```powershell
.\deploy.cmd
```

部署完成後，使用瀏覽器開啟 `http://localhost:8090/login`（若設定其他 `HOST_PORT`，請換成該連接埠）。以剛才設定的管理員帳號密碼登入。

若第一次執行時找不到 `.env`，部署程式會從 `.env.example` 建立 `.env` 並停止；填妥設定後再執行一次即可。部署失敗時先依訊息檢查設定、Docker Desktop 和日誌，不要刪除資料 volume。

## 3. 資料庫如何建立

不需要自行安裝 SQLite、建立空白 `.db` 檔或手動執行 SQL。第一次啟動時：

1. Docker Compose 建立該車廠專用的持久化 volume，名稱為 `<COMPOSE_PROJECT_NAME>_autorepairos-data`。
2. AutoRepairOS 在 volume 中建立 SQLite 資料庫檔 `/app/data/autorepair.db`。
3. 應用程式依序執行內嵌的資料庫 migrations，並在 `schema_migrations` 記錄已套用版本。
4. `RUN_SEED=true` 時，初始化程序依設定建立管理員帳號；未設定密碼的櫃檯或技師帳號不會建立。

資料庫檔位於 Docker 管理的 volume，不是專案目錄中的 `data\autorepair.db`。容器重新啟動或更新時，資料會保留在該 volume。

第一次成功登入後，請立即打開 `.env`，將 `RUN_SEED=false` 並清空 `DEFAULT_OWNER_PASSWORD`，然後再次執行 `.\deploy.cmd`。初始化只會建立尚不存在的帳號；不會藉由重跑初始化更新既有帳號密碼。請勿把 `.env`、資料庫或備份加入 Git 或傳給未授權人員。

常用管理命令（請在專案根目錄執行，`repairshop_a` 應與 `.env` 的專案名稱一致）：

```powershell
# 查看服務狀態
docker compose --env-file .env -p repairshop_a ps

# 查看最近的服務日誌
docker compose --env-file .env -p repairshop_a logs --tail 100 autorepairos

# 停止／重新啟動服務（不會刪除資料）
docker compose --env-file .env -p repairshop_a stop
docker compose --env-file .env -p repairshop_a start

# 確認資料 volume 存在
docker volume inspect repairshop_a_autorepairos-data
```

**不要執行 `docker compose down -v` 或刪除該 volume**，否則會刪除資料庫與上傳照片。

## 4. 第一次登入後的建議設定

1. 以管理員登入，確認首頁總覽和資料頁可以正常開啟。
2. 到「員工」新增每位實際使用者的獨立帳號，分派適當角色，不要多人共用管理員帳號。
3. 初始資料準備完成後，確認 `.env` 的 `RUN_SEED=false` 且初始化密碼已清空。
4. 建立常用客戶、車輛、零件/服務項目和供應商，再開始建立正式工單。
5. 先做一筆可辨識的練習資料，確認工單、照片、工時和收款記錄流程符合店內作業，再開始正式登錄。

目前角色用途如下：

| 角色 | 常見用途 |
| --- | --- |
| 系統管理員（owner） | 系統設定、人員管理、排程，以及管理員限定的停用/刪除等操作。 |
| 櫃檯人員（counter） | 客戶與車輛、工單、庫存、供應商和收款等日常營運。 |
| 技師（technician） | 查看工單、照片並登錄自己的工時；部分財務和修改功能受限。 |

實際可見頁面與操作以程式設定的角色權限為準。注意：「員工」頁目前對所有已登入角色開放檢視，可能顯示身分證等健保資料；若這些資料不應讓所有使用者看到，請勿建立不必要的帳號或填入非必要敏感資料，並將系統限制在可信任的內部網路。

## 5. 日常操作

### 客戶、車輛與預約

- 先在「客戶」建立或查找客戶，再到「車輛」建立並關聯車輛。
- 開立工單時選取客戶和車輛；新客戶或車輛也可使用畫面提供的快速建立功能。
- 「預約」用於記錄排程；工單用於實際維修作業與後續追蹤。

### 工單、工時與照片

- 在「工單」建立工單，填寫客訴/維修內容、指派技師，並按工作進度更新狀態。
- 在工單明細維護服務或零件項目、報價/核准資訊和作業檢查狀態。
- 技師可從工單查看作業內容、登錄工時及查看照片；櫃檯或管理員可依權限協助維護工時。
- 工單照片支援 JPG、PNG、WEBP、GIF，單檔上限 8 MB。照片會存放在同一資料 volume 的 `data/photos` 目錄，完整備份時也必須保留照片目錄。

### 庫存、收款及其他頁面

- 「庫存」可維護品項、庫存數量和調整紀錄；請透過系統庫存操作維護數量，不要直接修改資料庫。
- 「收款」可新增和查詢收款記錄；工單完成狀態與實際收款記錄是不同資料，請依店內流程登錄兩者並核對金額。作廢收款為管理員限定操作。
- 「報表」、「投保」、「定檢」、「保養套餐」、「支出」、「外包」、「設備」等頁面依目前版本提供相應查詢或維護功能；可操作範圍依角色權限而異。

### 進貨單（採購進貨）

- 左上選單「進貨單」：記錄向供應商進了什麼、各多少錢。
- **開單**：按「＋ 開立進貨單」→ 選供應商、進貨日期；若這批貨是為某張工單特別叫的（客戶指定零件、急件補料），填工單號碼；一般進貨留空。
- **加品項**：輸入品項名稱（或先搜尋庫存零件帶入名稱/料號）、數量、進貨單價；畫面即時顯示應付總額（**總共要給廠商多少錢**）。
- **確認進貨**：品項確認無誤後按「✓ 確認進貨」→ 零件數量自動加回庫存，單據鎖定不可再改（避免重複入庫）。
- 草稿狀態的單據可刪除；已進貨的單據不可刪除。

## 6. 備份與還原

資料庫和照片均在 `<COMPOSE_PROJECT_NAME>_autorepairos-data` volume 中。更新部署腳本會在有既有資料庫時嘗試建立**資料庫檔案**的部署前備份，存放於專案的 `data\backups`；這項自動備份不包含照片，不能取代完整備份。

### 建立完整備份（Windows PowerShell）

請在專案根目錄執行，並將專案名稱換成 `.env` 中設定的值。停止服務可確保 SQLite 檔案與照片在同一時間點複製：

```powershell
$project = "repairshop_a"
$backupDir = Join-Path (Get-Location) ("data\backups\full-" + (Get-Date -Format "yyyyMMdd_HHmmss"))
New-Item -ItemType Directory -Force -Path $backupDir | Out-Null

docker compose --env-file .env -p $project stop autorepairos
if ($LASTEXITCODE -ne 0) { throw "停止服務失敗，未建立備份。" }

docker compose --env-file .env -p $project cp autorepairos:/app/data/. $backupDir
$copyExitCode = $LASTEXITCODE
docker compose --env-file .env -p $project start autorepairos
if ($LASTEXITCODE -ne 0) { throw "備份完成後服務未能重新啟動，請檢查 Docker 狀態。" }
if ($copyExitCode -ne 0) { throw "備份複製失敗；請檢查磁碟空間與 Docker 日誌。" }
```

確認備份資料夾內至少有 `autorepair.db`；若曾上傳照片，也應包含 `photos` 目錄。將完整備份複製到另一個受控位置或離線儲存，並保護其中的客戶、員工與車輛個資。
若曾設定 `DB_ENABLE_WAL=true`，備份中的 `autorepair.db-wal` 和 `autorepair.db-shm` 也屬同一份資料庫狀態；還原時必須一併使用同一時間點的檔案，不可混搭其他備份。預設值為 `false`。

### 還原完整備份

還原會覆蓋目前資料。先確認選對車廠與備份，並保留目前資料的另一份完整備份。以下命令會停止服務，使用 Docker 暫存容器將資料庫及照片寫回該車廠的 volume，並清除舊的 SQLite WAL sidecar，避免與備份狀態混用：

```powershell
$project = "repairshop_a"
$backupDir = (Resolve-Path "C:\請替換成完整備份資料夾").Path
$volume = "${project}_autorepairos-data"

docker volume inspect $volume *> $null
if ($LASTEXITCODE -ne 0) { throw "找不到目標車廠的資料 volume；請先核對專案名稱，不要繼續還原。" }

docker compose --env-file .env -p $project stop autorepairos
if ($LASTEXITCODE -ne 0) { throw "停止服務失敗，取消還原。" }

docker run --rm `
  --mount "type=volume,source=$volume,target=/data" `
  --mount "type=bind,source=$backupDir,target=/backup,readonly" `
  alpine:3.21 sh -ec 'test -f /backup/autorepair.db; rm -f /data/autorepair.db-wal /data/autorepair.db-shm; cp /backup/autorepair.db /data/autorepair.db; for sidecar in autorepair.db-wal autorepair.db-shm; do if [ -f "/backup/$sidecar" ]; then cp "/backup/$sidecar" "/data/$sidecar"; fi; done; rm -rf /data/photos; if [ -d /backup/photos ]; then mkdir -p /data/photos; cp -a /backup/photos/. /data/photos/; fi'
if ($LASTEXITCODE -ne 0) { throw "還原失敗；服務仍保持停止，請檢查備份資料夾、Docker 狀態與 volume 名稱。" }
```

還原完成後啟動並檢查日誌：

```powershell
docker compose --env-file .env -p $project start autorepairos
docker compose --env-file .env -p $project logs --tail 100 autorepairos
```

若照片複製失敗，不要忽略錯誤；可先停止服務，修正照片目錄後再啟動。啟動時應用程式會檢查資料庫 migrations；若有錯誤，停止操作並從剛才保留的備份復原。不要用較新的程式直接覆蓋唯一一份正式資料庫。

## 7. 升級與故障排除

- 升級時使用同一個 `.env` 和 `.\deploy.cmd`。部署程序會建置新映像，並在資料庫 volume 已存在時先嘗試備份 SQLite 檔案；重要升級前仍應自行建立包含照片的完整備份。
- **瀏覽器無法連線**：確認 Docker Desktop 已啟動、服務狀態為 running，並檢查 `HOST_PORT` 是否被其他程式占用。
- **部署提示 Docker/WSL2 未就緒**：依提示啟動或安裝 Docker Desktop/WSL2、切換到 Linux containers，必要時重新啟動 Windows 後再執行部署。
- **登入失敗**：確認使用的是 `DEFAULT_OWNER_USERNAME` 與首次初始化設定的密碼。`RUN_SEED` 不會重設已存在帳號的密碼；不要為了解決登入問題刪除資料 volume。
- **服務無法啟動或資料庫錯誤**：查看 `docker compose ... logs`。不要手動刪除 SQLite 檔案、WAL 檔或 Docker volume；先備份現況，再處理錯誤。

### Docker 模式維運工具速查

| 工具/指令 | 用途 |
|---|---|
| `http://localhost:8091`（sqlite-web） | 唯讀看 Docker 內的 SQLite 資料庫（compose 已內建） |
| `docker exec -it ollama_local ollama run qwen2.5vl:3b` | 進 ollama 容器互動跑視覺模型（Docker 模式的地端 LLM）|
| `docker exec -it ollama_local ollama pull qwen2.5vl:3b` | 首次使用先拉模型（約 3GB，只需一次，存在 volume 不會重拉）|
| `docker compose logs -f autorepairos` | 看服務日誌 |
| `docker compose up -d --force-recreate` | 改程式/`.env` 後重新部署 |

Docker 模式的地端視覺 LLM 由 compose 內的 `ollama` 容器提供（`ollama_local`，模型 qwen2.5vl:3b）；Windows EXE 模式則由系統自動管理 llama-server（見上方「EXE 模式必備工具」）。兩種模式的視覺設定互相獨立，`.env` 的 VISION_* 只影響 exe。

## 8. 網路與個資安全

- `.env`、資料庫、備份和照片都可能含有機密或個人資料，應限制檔案權限並安全保存。
- 預設連線使用 HTTP。不要將 `HOST_PORT` 直接暴露到公用網際網路；遠端使用應透過可信任的 VPN 或受正確設定的 HTTPS 反向代理，並配置防火牆。
- 每家車廠使用自己的 `.env`、Compose 專案、資料 volume 和備份；不要把不同車廠的資料放進同一個資料庫。
- 忘記管理員密碼時，請勿重建或刪除資料庫；先保留備份，再由有權限的系統管理員依維運程序處理。
