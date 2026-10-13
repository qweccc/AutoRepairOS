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

# 單機版（exe）在 release/ 目錄下執行，start.ps1 會檢查 web\index.html，
# 故每次 release 必須把最新 web/ 同步進 release/web（不然車廠跑舊前端）。
mkdir -p release/web
cp -r web/. release/web/
echo "WEB: release/web synced ($(find web -type f | wc -l) files)"

docker compose build
docker tag autorepairos-autorepairos:latest "autorepairos-autorepairos:${VERSION}"
echo "Docker: autorepairos-autorepairos:${VERSION}"
echo "Release ${VERSION} done."
