#!/bin/bash
set -e
# 版本號含日期與時間（vYYYYMMDD-HHMM），一天可 build 多版不互相覆蓋。
STAMP=$(date +%Y%m%d-%H%M)
VERSION="v${STAMP}"
mkdir -p release

# 舊版 exe 移到 unpublished/（gitignore，不會上傳）：留最近版即可，舊版留檔不刪。
# release/ 保持只有最新 exe，車廠/上傳時不會拿到舊檔。
if ls release/autorepair_v*.exe >/dev/null 2>&1; then
	mkdir -p unpublished/release-exe
	mv release/autorepair_v*.exe unpublished/release-exe/
	echo "OLD EXE: moved to unpublished/release-exe/"
fi

echo "Building AutoRepairOS ${VERSION}..."

go build -o "release/autorepair_${VERSION}.exe" .
echo "EXE: release/autorepair_${VERSION}.exe"

# 單機版（exe）只需一個磁碟檔：web/index.html（main.go 用 template.ParseFiles
# 讀它注入 APP_NAME，start.ps1 也會檢查它存在）。
# 其餘前端全部編進 binary，不需要也不該複製進 release/：
#   - 靜態資源（CSS/JS）→ internal/handler/static/ 走 //go:embed（web/static/ 已刪除）
#   - 頁面/元件 → web/pages|components/*.templ 由 templ 產生 *_templ.go 編進 binary
# release/web/ 只需要 index.html；多帶的檔案只是死重量，還讓人誤以為要改它們。
mkdir -p release/web
cp web/index.html release/web/index.html
echo "WEB: release/web/index.html synced"

# 舊版 release/web 殘留（過往 cp -r web/. 留下的 pages/、components/、static/）清掉，
# 避免車廠解壓後看到一堆用不到的檔案、或誤改它們以為會生效。
rm -rf release/web/pages release/web/components release/web/static
echo "WEB: stale release/web subdirs removed"

# release/data/ 一律不含在交付物裡：它只會是冒煙測試寫進 release/data/autorepair.db
# 的殘留（與 docker named volume 正式庫完全隔離），帶出去車廠首次啟動就會跳過
# seed、還拿到假資料。db.go 的 MkdirAll 會自己建 data/，.env 的 RUN_SEED=true
# 會在第一次啟動建 owner 帳號。
rm -rf release/data
echo "DATA: release/data removed (exe recreates it on first run)"

# release/.env 是必要設定檔，build.sh 絕不碰它：
#   - start.ps1 讀不到 .env 就直接退出；exe 也從自己所在目錄讀它（config.loadDotEnv
#     的候選路徑：exe 目錄優先，其次 cwd），所以它必須跟 exe 同一層、不能搬到別處。
#   - 它有車廠自己的 PORT / DB_PATH / seed 密碼 / LINE 憑證，跟 repo 根的 .env 不同
#     （repo 根是 8090 + 開發用 seed 密碼）。覆蓋它 = 車廠連線設定與密碼全部跑掉。
#   - 它已在 .gitignore（/release/ 整包忽略），不會進版控。
# 若全新 clone 沒有 release/.env，從 repo 根複製再改：
#   cp .env.example release/.env
if [ ! -f release/.env ]; then
echo "WARN: release/.env 不存在（單機版必要設定檔）。請 cp .env.example release/.env 後補齊設定。"
fi

# release/.env.example 每次同步：它是「這版有哪些開關」的唯一權威清單。
# 為什麼不能省：release/.env 是舊 build 留下來的快照，新功能新增的 key 不會自己
# 長進去（loadDotEnv 只補「尚未設定」的變數，缺 key 就是沒有預設值 → 功能靜默
# 停用，如 VISION_MODEL_PATH 缺席時視覺辨識回 enabled:false）。.env 雖然也有中文
# 註解，但它只反映當初建立時的版本，沒有「這個 key 是這版新加的」資訊。
# 車廠/維護者比對兩邊就知道要補哪些行。
cp .env.example release/.env.example
echo "ENV: release/.env.example synced"

# 列出 .env.example 有、release/.env 卻沒有的 key —— 正是會靜默停用的開關。
# 只提示不改寫 .env（.env 是車廠自己的設定，build.sh 不碰）。
# 分流：下列 key 只服務 docker compose（exe 完全不讀），列為「僅 Docker」；
# 其餘是 exe 真的會讀的，列為「單機版請補」。
# 為什麼要完整列出：把 exe 不讀的 key 混進「請補」清單，每次都看到一堆無關警告，
# 警告就失去意義（真正的缺失會被忽略）。判據是「exe 有沒有讀它」——加新 key 前
# 先 grep 一次 Go 原始碼，不要憑 key 名稱猜。
docker_only="COMPOSE_PROJECT_NAME HOST_PORT SQLITE_WEB_ENABLED SQLITE_WEB_PORT"
if [ -f release/.env ]; then
	missing=$(comm -13 \
		<(grep -oE '^[A-Za-z_][A-Za-z0-9_]*=' release/.env | sed 's/=$//' | sort -u) \
		<(grep -oE '^[A-Za-z_][A-Za-z0-9_]*=' .env.example | sed 's/=$//' | sort -u))
	exe_missing=""
	for k in $missing; do
		case " $docker_only " in
			*" $k "*) ;;
			*) exe_missing="$exe_missing $k" ;;
		esac
	done
	if [ -n "$exe_missing" ]; then
		echo "ENV WARN: release/.env 缺少 exe 會讀的 key（新功能可能靜默停用，請補進 .env）："
		for k in $exe_missing; do echo "  - $k"; done
	else
		echo "ENV: release/.env 已涵蓋 exe 需要的全部 key"
	fi
	if [ -n "$missing" ]; then
		echo "ENV: 僅 Docker 適用的 key（單機版可忽略）：$(echo $missing | tr ' ' '\n' | grep -xF -e COMPOSE_PROJECT_NAME -e HOST_PORT | tr '\n' ' ')"
	fi
fi

docker compose build
docker tag autorepairos-autorepairos:latest "autorepairos-autorepairos:${VERSION}"
echo "Docker: autorepairos-autorepairos:${VERSION}"
echo "Release ${VERSION} done."
