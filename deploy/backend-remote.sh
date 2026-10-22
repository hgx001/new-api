#!/usr/bin/env bash
# new-api 后端部署脚本（服务器侧执行体），带 SHA 断言（避免"构建了旧 commit 却以为成功"）。
# ⚠️ 在生产服务器上执行，勿在本地直接运行。
# 本地入口: deploy/deploy-backend.sh <sha>（SSH 管道把本脚本送上服务器执行）
# 用法（服务器上）: bash deploy/backend-remote.sh <short-sha>
set -Eeuo pipefail
TARGET="${1:?need target sha}"
APP_DIR="${DEPLOY_APP_DIR:-/home/ubuntu/new-api-src}"
MIN_FREE_MB="${DEPLOY_MIN_FREE_MB:-2048}"
cd "$APP_DIR"

die() { echo "ABORT: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 0. 磁盘预检。
#    2026-10-22 10:24 事故：磁盘 99% 时 `docker inspect > env 文件` 因 ENOSPC
#    只写出 0 字节，脚本没校验就 `--env-file` 起容器 → 新容器没有 SQL_DSN →
#    new-api 回落到空 SQLite（users/channels/tokens 全 0 行），而 /api/status
#    仍然 success，HEALTH_OK 骗过所有人，站点数据面实际瘫痪。
# ---------------------------------------------------------------------------
free_mb=$(df -Pm / | awk 'NR==2 {print $4}')
[[ "$free_mb" =~ ^[0-9]+$ ]] || die "无法读取磁盘剩余空间"
if [ "$free_mb" -lt "$MIN_FREE_MB" ]; then
  die "磁盘剩余 ${free_mb}MB < ${MIN_FREE_MB}MB，先清理磁盘再发版（docker builder prune / 旧 release）"
fi
echo "DISK_OK: ${free_mb}MB free"

# github 偶尔连不上（本次就遇到）。只要本地已有该 commit 就继续，
# 否则必须报错退出——绝不能在代码不对的情况下构建。
if ! git fetch -q origin main 2>/dev/null; then
  echo "WARN: fetch 失败，检查本地是否已有 $TARGET"
  if ! git cat-file -e "${TARGET}^{commit}" 2>/dev/null; then
    echo "ABORT: 本地也没有 $TARGET，无法部署"
    exit 1
  fi
  echo "本地已有该 commit，继续"
fi
git checkout --detach "$TARGET"
GOT=$(git rev-parse HEAD)
# git 的 --short 长度不固定（7~9 位），用**完整 SHA 前缀**比较
if [[ "$GOT" != "$TARGET"* ]]; then
  echo "ABORT: HEAD=$GOT 不是请求的 $TARGET 的前缀 —— 不会用错误代码构建"
  exit 1
fi
echo "checkout OK: ${GOT:0:9} (requested $TARGET)"

TS=$(date +%Y%m%d%H%M%S)
BUILD_LOG="/tmp/newapi-build-$TS.log"
# 构建日志落盘：构建失败或写盘失败（no space left on device）必须中止，
# 不能让 `| tail -1` 把退出码吃掉。
if ! docker build -f Dockerfile.deploy -t new-api-custom:latest . > "$BUILD_LOG" 2>&1; then
  tail -20 "$BUILD_LOG" >&2
  die "docker build 失败"
fi
if grep -q "no space left on device" "$BUILD_LOG"; then
  tail -20 "$BUILD_LOG" >&2
  die "构建过程磁盘写满（no space left on device）"
fi
tail -1 "$BUILD_LOG"

# ---------------------------------------------------------------------------
# 1. 环境变量快照：容器必须拿到 SQL_DSN / SESSION_SECRET / CRYPTO_SECRET，
#    否则 new-api 会静默回落到 SQLite。快照为空或缺关键项 → 立刻中止。
# ---------------------------------------------------------------------------
ENVFILE="/tmp/newapi-$TS.env"
if ! docker inspect new-api --format '{{range .Config.Env}}{{println .}}{{end}}' > "$ENVFILE" 2>"$ENVFILE.err"; then
  cat "$ENVFILE.err" >&2
  die "docker inspect 取 env 失败"
fi
chmod 600 "$ENVFILE"
env_bytes=$(wc -c < "$ENVFILE")
if [ "$env_bytes" -lt 100 ]; then
  cat "$ENVFILE.err" >&2
  die "env 快照只有 ${env_bytes} 字节（写盘失败？），中止发版——绝不用空 env 起容器"
fi
for k in SQL_DSN SESSION_SECRET CRYPTO_SECRET; do
  grep -q "^$k=" "$ENVFILE" || die "env 快照缺少 $k，中止发版"
done
echo "ENV_OK: ${env_bytes} bytes"

docker stop new-api >/dev/null
docker rename new-api "new-api-prev-$TS"
OLD_NAME="new-api-prev-$TS"
echo "OLD_KEPT: $OLD_NAME"

docker run -d --name new-api --restart always -p 127.0.0.1:3000:3000 \
  -v /home/ubuntu/new-api/data:/data -v /home/ubuntu/new-api/logs:/app/logs \
  --network new-api_new-api-network --env-file "$ENVFILE" \
  new-api-custom:latest --log-dir /app/logs >/dev/null

rollback() {
  echo "[deploy] 回滚：把 $OLD_NAME 恢复为 new-api"
  docker rm -f new-api >/dev/null 2>&1 || true
  docker rename "$OLD_NAME" new-api >/dev/null 2>&1 || true
  docker start new-api >/dev/null 2>&1 || true
  echo "[deploy] 已回滚到旧容器"
}

# ---------------------------------------------------------------------------
# 2. 健康检查：光看 "success":true 不够（空 SQLite 也 success）。
#    必须同时满足：/api/status 的 setup=true、日志里没有回落到 SQLite、
#    新容器仍带 SQL_DSN；任一不满足 → 自动回滚到旧容器。
# ---------------------------------------------------------------------------
for i in $(seq 1 24); do
  status=$(wget -q -O - http://127.0.0.1:3000/api/status 2>/dev/null || true)
  if grep -q '"success": *true' <<<"$status" && grep -q '"setup": *true' <<<"$status"; then
    if docker logs new-api 2>&1 | grep -q "using SQLite as database"; then
      docker logs new-api --tail 15 >&2
      rollback
      die "新容器回落到 SQLite（env 丢失）"
    fi
    if ! docker inspect new-api --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -q '^SQL_DSN='; then
      rollback
      die "新容器缺少 SQL_DSN"
    fi
    echo "HEALTH_OK ${i}x5s (setup=true, PostgreSQL)"
    break
  fi
  if [ "$i" = 24 ]; then
    docker logs new-api --tail 15 >&2
    rollback
    die "健康检查失败"
  fi
  sleep 5
done

echo "FINAL_SHA: $(git rev-parse --short HEAD)  (requested $TARGET)"
docker ps --filter name=^/new-api$ --format "container: {{.Status}}"

# 保留最近 3 个旧容器，更早的删掉，避免磁盘堆积
mapfile -t OLD < <(docker ps -a --filter 'name=new-api-prev-' --format '{{.Names}}' | sort -r)
if [ "${#OLD[@]}" -gt 3 ]; then
  for n in "${OLD[@]:3}"; do echo "prune old container: $n"; docker rm -f "$n" >/dev/null; done
fi
echo "kept old containers: ${OLD[*]:0:3}"
