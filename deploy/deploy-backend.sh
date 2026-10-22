#!/usr/bin/env bash
# new-api 后端部署本地入口：把 backend-remote.sh 上传到生产服务器，**以脱离会话的方式**执行，
# 再轮询服务器上的部署日志直到出现终态标记。
#
# 为什么不用 `ssh ... 'bash -s < sha'` 直接管道：
#   2026-10-22 实测——本地命令超时/断网会连带杀掉远端脚本，部署可能停在
#   「旧容器已改名、新容器未起」的危险窗口（本次侥幸停在 build 阶段，零停机）。
#   改成 nohup+setsid 脱离会话后，部署与本地连接解耦：本地断了不影响远端，
#   远端日志留在服务器上，随时可查/重连续看。
#
# 用法: deploy/deploy-backend.sh [--dry-run] <short-sha>
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
REMOTE_SCRIPT="/tmp/backend-remote.sh"
REMOTE_LOG="/tmp/backend-deploy-$(date +%Y%m%d%H%M%S).log"
POLL_TIMEOUT="${DEPLOY_POLL_TIMEOUT:-5400}"
POLL_INTERVAL="${DEPLOY_POLL_INTERVAL:-15}"

if [ "${DRY:-0}" = 1 ]; then
  echo "DRY-RUN: scp -P $PORT $SCRIPT_DIR/backend-remote.sh $HOST:$REMOTE_SCRIPT"
  echo "DRY-RUN: ssh -p $PORT $HOST 'nohup setsid bash $REMOTE_SCRIPT $TARGET > $REMOTE_LOG 2>&1 &'"
  exit 0
fi

echo "[deploy] 上传部署脚本"
scp -P "$PORT" "$SCRIPT_DIR/backend-remote.sh" "$HOST:$REMOTE_SCRIPT"

echo "[deploy] 启动远端部署（脱离会话，日志：$REMOTE_LOG）"
ssh -p "$PORT" "$HOST" \
  "nohup setsid bash '$REMOTE_SCRIPT' '$TARGET' > '$REMOTE_LOG' 2>&1 < /dev/null & echo started:\$REMOTE_LOG"

# 轮询远端日志直到出现终态标记；期间本地断开也不影响远端部署继续跑。
seen=0
deadline=$(( $(date +%s) + POLL_TIMEOUT ))
while :; do
  out=$(ssh -p "$PORT" "$HOST" "cat '$REMOTE_LOG' 2>/dev/null || true")
  if [ -n "$out" ]; then
    total=$(printf '%s\n' "$out" | wc -l)
    if [ "$total" -gt "$seen" ]; then
      printf '%s\n' "$out" | tail -n +$((seen + 1))
      seen=$total
    fi
  fi
  if printf '%s\n' "$out" | grep -q 'FINAL_SHA:'; then
    echo "[deploy] 部署完成"
    exit 0
  fi
  if printf '%s\n' "$out" | grep -q 'ABORT:'; then
    echo "[deploy] 部署失败（见 $REMOTE_LOG）" >&2
    exit 1
  fi
  alive=$(ssh -p "$PORT" "$HOST" "pgrep -f '[b]ackend-remote.sh' >/dev/null && echo yes || echo no")
  if [ "$alive" != "yes" ]; then
    echo "[deploy] 远端部署脚本已退出但没有终态标记，判定为失败（见 $REMOTE_LOG）" >&2
    exit 1
  fi
  if [ "$(date +%s)" -ge "$deadline" ]; then
    echo "[deploy] 等待部署终态超时（${POLL_TIMEOUT}s），远端日志：$REMOTE_LOG" >&2
    exit 1
  fi
  sleep "$POLL_INTERVAL"
done
