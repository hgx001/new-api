#!/usr/bin/env bash
# new-api 后端部署本地入口：SSH 到生产服务器，把 backend-remote.sh 经管道送入执行。
# 服务器侧执行体: deploy/backend-remote.sh
# 用法: deploy/deploy-backend.sh <short-sha>
#       deploy/deploy-backend.sh --dry-run <short-sha>
set -euo pipefail

DRY=0
TARGET=""
for a in "$@"; do
  if [ "$a" = "--dry-run" ]; then DRY=1
  elif [ -z "$TARGET" ]; then TARGET="$a"
  else echo "ABORT: 多余参数 '$a'" >&2; exit 1; fi
done
[ -n "$TARGET" ] || { echo "usage: deploy-backend.sh [--dry-run] <short-sha>" >&2; exit 1; }

# 边界校验：SHA 只允许十六进制，防止注入进远程命令串
if ! [[ "$TARGET" =~ ^[0-9a-f]{7,40}$ ]]; then
  echo "ABORT: '$TARGET' 不是合法的 git SHA 前缀" >&2
  exit 1
fi

HOST="${DEPLOY_HOST:-ubuntu@119.29.253.97}"
PORT="${DEPLOY_PORT:-877}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

CMD="bash -s $TARGET"
if [ "${DRY:-0}" = 1 ]; then
  echo "DRY-RUN: ssh -p $PORT $HOST \"$CMD\" < $SCRIPT_DIR/backend-remote.sh"
  exit 0
fi
exec ssh -p "$PORT" "$HOST" "$CMD" < "$SCRIPT_DIR/backend-remote.sh"
