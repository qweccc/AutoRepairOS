#!/bin/bash
set -e
DATE=$(date +%Y%m%d)
VERSION="v${DATE}"
mkdir -p release
echo "Building AutoRepairOS ${VERSION}..."

go build -o "release/autorepair_${VERSION}.exe" .
echo "EXE: release/autorepair_${VERSION}.exe"

docker compose build
docker tag autorepairos-autorepairos:latest "autorepairos-autorepairos:${VERSION}"
echo "Docker: autorepairos-autorepairos:${VERSION}"
