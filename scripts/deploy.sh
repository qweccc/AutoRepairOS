#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

fail() {
  printf '部署失敗：%s\n' "$1" >&2
  exit 1
}

if [[ ! -f .env ]]; then
  cp .env.example .env
  printf '已建立 .env。請填入車廠專屬設定與 DEFAULT_OWNER_PASSWORD，再重新執行 scripts/deploy.sh。\n'
  exit 2
fi

env_value() {
  local key="$1"
  local value
  value="$(sed -n "s/^${key}=//p" .env | tail -n 1)"
  value="${value#\"}"
  value="${value%\"}"
  value="${value#\'}"
  value="${value%\'}"
  printf '%s' "$value"
}

project="$(env_value COMPOSE_PROJECT_NAME)"
project="${project:-autorepairos}"
[[ "$project" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || fail "COMPOSE_PROJECT_NAME 格式錯誤。"

port="$(env_value HOST_PORT)"
port="${port:-8090}"
[[ "$port" =~ ^[0-9]+$ ]] && (( port >= 1 && port <= 65535 )) || fail "HOST_PORT 必須介於 1 到 65535。"

run_seed="$(env_value RUN_SEED)"
owner_password="$(env_value DEFAULT_OWNER_PASSWORD)"
if [[ "$run_seed" =~ ^(true|1|yes)$ ]]; then
  [[ ${#owner_password} -ge 12 && ! "$owner_password" =~ (CHANGE_ME|Owner001!|password) ]] || \
    fail "首次初始化需設定至少 12 字元且非範例值的 DEFAULT_OWNER_PASSWORD。"
fi

command -v docker >/dev/null 2>&1 || fail "請先安裝 Docker Engine/Compose plugin，並啟動 Docker daemon。"
command -v curl >/dev/null 2>&1 || fail "部署健康檢查需要 curl，請先安裝 curl。"

compose=(docker compose --env-file "$ROOT/.env" -p "$project")
"${compose[@]}" version >/dev/null 2>&1 || fail "需要 Docker Compose v2。"
docker_type="$(docker info --format '{{.OSType}}' 2>/dev/null)" || fail "Docker daemon 尚未就緒。"
[[ "$docker_type" == "linux" ]] || fail "此應用使用 Linux container image，請切換至 Linux containers。"

printf '正在建置 AutoRepairOS...\n'
"${compose[@]}" build autorepairos || fail "Docker image 建置失敗；現有服務尚未停止。"

was_running="$("${compose[@]}" ps --status running -q autorepairos)"
volume="${project}_autorepairos-data"
if [[ -n "$was_running" ]]; then
  "${compose[@]}" stop autorepairos || fail "無法停止服務；未繼續部署。"
fi

backup_file=""
if docker volume inspect "$volume" >/dev/null 2>&1; then
  mkdir -p "$ROOT/data/backups"
  timestamp="$(date +%Y%m%d_%H%M%S)"
  backup_name="${project}_${timestamp}.db"
  backup_file="$ROOT/data/backups/$backup_name"
  if ! docker run --rm \
    --mount "type=volume,source=$volume,target=/data,readonly" \
    --mount "type=bind,source=$ROOT/data/backups,target=/backup" \
    alpine:3.21 sh -c "if [ -f /data/autorepair.db ]; then cp /data/autorepair.db /backup/$backup_name; else echo NO_DATABASE; fi"; then
    if [[ -n "$was_running" ]]; then "${compose[@]}" start autorepairos >/dev/null 2>&1 || true; fi
    fail "部署前備份失敗；已嘗試恢復原服務，請勿刪除 Docker volume。"
  fi
  [[ -f "$backup_file" ]] || backup_file=""
fi

if ! "${compose[@]}" up -d --no-build; then
  if [[ -n "$was_running" ]]; then "${compose[@]}" start autorepairos >/dev/null 2>&1 || true; fi
  fail "Compose 啟動失敗；資料 volume 未刪除。"
fi

url="http://127.0.0.1:${port}/login"
healthy=false
for _ in $(seq 1 30); do
  if curl --silent --show-error --fail --output /dev/null --max-time 4 "$url"; then
    healthy=true
    break
  fi
  sleep 2
done

if [[ "$healthy" != true ]]; then
  "${compose[@]}" logs --tail 80 autorepairos || true
  [[ -n "$backup_file" ]] && printf '部署前備份：%s\n' "$backup_file" >&2
  fail "健康檢查失敗。請勿執行 docker compose down -v。"
fi

printf 'AutoRepairOS 已就緒：%s\n' "$url"
[[ -n "$backup_file" ]] && printf '部署前資料備份：%s\n' "$backup_file"
if [[ "$run_seed" =~ ^(true|1|yes)$ ]]; then
  printf '首次登入後請將 .env 的 RUN_SEED 設為 false 並清空初始化密碼，再次執行部署腳本。\n'
fi
