#!/bin/bash
set -e
# 版本號含日期與時間（vYYYYMMDD-HHMM），一天可 build 多版不互相覆蓋。
STAMP=$(date +%Y%m%d-%H%M)
VERSION="v${STAMP}"
mkdir -p release
echo "Building AutoRepairOS ${VERSION}..."

go build -o "release/autorepair_${VERSION}.exe" .
echo "EXE: release/autorepair_${VERSION}.exe"

docker compose build
docker tag autorepairos-autorepairos:latest "autorepairos-autorepairos:${VERSION}"
echo "Docker: autorepairos-autorepairos:${VERSION}"
echo "Release ${VERSION} done."
