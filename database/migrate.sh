#!/usr/bin/env bash
# Wrapper around golang-migrate for database/migrations/.
#
# Usage:
#   ./migrate.sh                                              # auto-load .env, run 'up'
#   ./migrate.sh up [N]                                       # auto-load .env
#   ./migrate.sh down [N]
#   ./migrate.sh version
#   ./migrate.sh goto <V>
#   ./migrate.sh force <V>
#
# 直接传连接串（三种方式等价）：
#   ./migrate.sh up --database 'postgres://user:pass@host:5432/db?sslmode=disable'
#   ./migrate.sh -d 'postgres://...' up
#   DATABASE_URL='postgres://...' ./migrate.sh up
#
# 优先级：--database 参数 > 已 export 的 DATABASE_URL > repo-root .env > database/.env
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------- 1. 解析 --database / -d ----------
ARGS=()
for arg in "$@"; do
  case "$arg" in
    --database=*) DATABASE_URL="${arg#--database=}";;
    -d=*)         DATABASE_URL="${arg#-d=}";;
    --database|-d)
      # 下一个 arg 是值，留给下面手动处理
      ARGS+=("$arg");;
    *)
      ARGS+=("$arg");;
  esac
done
# 处理拆开的 --database URL / -d URL
i=0
NEW_ARGS=()
while [[ $i -lt ${#ARGS[@]} ]]; do
  a="${ARGS[$i]}"
  if [[ "$a" == "--database" || "$a" == "-d" ]]; then
    next="${ARGS[$((i+1))]:-}"
    if [[ -n "$next" ]]; then
      DATABASE_URL="$next"
      i=$((i+2))
      continue
    fi
  fi
  NEW_ARGS+=("$a")
  i=$((i+1))
done
# set -u 跟空数组 ${arr[@]} 冲突，安全写法
if [[ ${#NEW_ARGS[@]} -eq 0 ]]; then
  set --
else
  set -- "${NEW_ARGS[@]}"
fi

# 兼容 `./migrate.sh postgres://... up` 这种 URL 在最前面的写法
if [[ $# -gt 0 ]] && [[ "$1" == postgres://* || "$1" == postgresql://* ]]; then
  DATABASE_URL="$1"
  shift
fi

# ---------- 2. 没拿到 URL 时才读 .env ----------
if [[ -z "${DATABASE_URL:-}" ]]; then
  for candidate in "${ROOT}/.env" "${SCRIPT_DIR}/.env"; do
    if [[ -f "${candidate}" ]]; then
      set -a
      # shellcheck disable=SC1091
      source "${candidate}"
      set +a
      break
    fi
  done
fi

# 剥 CRLF（Windows 编辑器留下的 \r 会破坏连接串）
DATABASE_URL="${DATABASE_URL//$'\r'/}"

if [[ -z "${DATABASE_URL:-}" ]]; then
  echo "DATABASE_URL is required." >&2
  echo "  Usage: $0 --database 'postgres://user:pass@host:5432/db' up" >&2
  echo "  Or put DATABASE_URL=... in repo-root .env" >&2
  exit 1
fi

# ---------- 3. migrate CLI 存在性 ----------
if ! command -v migrate >/dev/null 2>&1; then
  echo "golang-migrate not found. Install:" >&2
  echo "  brew install golang-migrate" >&2
  echo "  or: go install github.com/golang-migrate/migrate/v4/cmd/migrate@latest" >&2
  exit 1
fi

# ---------- 4. 派发 ----------
DIRECTION="${1:-up}"
STEPS="${2:-}"

cd "${SCRIPT_DIR}"

case "${DIRECTION}" in
  up)
    migrate -path migrations -database "${DATABASE_URL}" up ${STEPS}
    ;;
  down)
    migrate -path migrations -database "${DATABASE_URL}" down ${STEPS:-1}
    ;;
  goto)
    [[ -n "${STEPS}" ]] || { echo "usage: $0 [--database URL] goto <version>" >&2; exit 1; }
    migrate -path migrations -database "${DATABASE_URL}" goto "${STEPS}"
    ;;
  force)
    [[ -n "${STEPS}" ]] || { echo "usage: $0 [--database URL] force <version>" >&2; exit 1; }
    migrate -path migrations -database "${DATABASE_URL}" force "${STEPS}"
    ;;
  version)
    migrate -path migrations -database "${DATABASE_URL}" version
    ;;
  *)
    echo "Usage: $0 [--database URL] {up|down [N]|goto <V>|force <V>|version}" >&2
    exit 1
    ;;
esac
