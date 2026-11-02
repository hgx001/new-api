#!/bin/bash
# 用原 docker run 参数把 new-api 容器换到指定镜像（默认 new-api-custom:latest）。
#
# 为什么需要它：容器是 docker run 起的（没有 compose 标签），
# `docker compose up -d new-api` 会报 "container name is already in use"。
# 本脚本严格复刻线上参数（127.0.0.1 绑定、root 用户、日志 100m*3、-w /data、
# 无内存限制），旧容器只 rename 不删除，健康检查失败自动回滚。
#
# 用法：
#   ./replace-container.sh                 # 换到 new-api-custom:latest
#   ./replace-container.sh <image:tag>     # 换到指定镜像
set -euo pipefail

IMG="${1:-new-api-custom:latest}"
NAME="new-api"
TS="$(date +%Y%m%d%H%M%S)"
OLD="${NAME}-prev-${TS}"
DEPLOY="/home/ubuntu/new-api"
BAKDIR="${DEPLOY}/backups"
ENVFILE="/tmp/newapi.env.${TS}"
PORT=3000

NEWDIGEST="$(sudo docker inspect "$IMG" --format '{{.Id}}' 2>/dev/null || echo '')"
[ -n "$NEWDIGEST" ] || { echo "镜像 $IMG 不存在"; exit 1; }

echo "目标镜像: ${IMG} (${NEWDIGEST})"

# 2026-11-02 事故二：脚本原本只认名为 "new-api" 的容器。
# 但前一次失败替换把真容器改名成了 new-api-prev-<ts>，
# 名字为 new-api 的那个是 Exited(255) 的空壳，真正占着 3000 端口的是 prev 容器。
# 于是脚本去 stop 一个本来就没跑的容器，端口当然不会释放，
# docker run 报 "port is already allocated" → 又一次回滚。
#
# 现在改成：**按端口占用者**定位活容器，与名字无关。
RUNNING=""
for c in $(sudo docker ps --filter "publish=${PORT}" --format '{{.Names}}'); do
  RUNNING="$c"
  break
done
# 兜底：publish 过滤没匹配上时，逐个扫 running 容器找带该端口映射的
if [ -z "$RUNNING" ]; then
  for c in $(sudo docker ps --format '{{.Names}}'); do
    if sudo docker inspect "$c" --format '{{range $p,$hp := .NetworkSettings.Ports}}{{if eq $p "3000/tcp"}}{{$hp}}{{end}}{{end}}' 2>/dev/null | grep -q "${PORT}"; then
      RUNNING="$c"
      break
    fi
  done
fi

CURDIGEST=""
if [ -n "$RUNNING" ]; then
  CURDIGEST="$(sudo docker inspect "$RUNNING" --format '{{.Image}}' 2>/dev/null || echo '')"
  echo "当前运行容器: $RUNNING (镜像 ${CURDIGEST})"
else
  echo "当前运行容器: <无占用 ${PORT} 端口的容器>"
fi

if [ -n "$CURDIGEST" ] && [ "$CURDIGEST" = "$NEWDIGEST" ] && [ "$RUNNING" = "$NAME" ]; then
  echo "容器已在跑目标镜像，无需替换。"
  exit 0
fi

mkdir -p "$BAKDIR"
if [ -n "$RUNNING" ]; then
  sudo docker inspect "$RUNNING" | sudo tee "${BAKDIR}/container-${RUNNING}-${TS}.json" >/dev/null
  echo "容器配置已备份: ${BAKDIR}/container-${RUNNING}-${TS}.json"
  echo "=== 导出 env ==="
  sudo docker inspect "$RUNNING" --format '{{range .Config.Env}}{{println .}}{{end}}' \
    | grep -v '^PATH=' | grep -v '^$' > "$ENVFILE"
else
  echo "=== 无现存容器，从 .env 生成 ==="
  cp "${DEPLOY}/.env" "$ENVFILE"
fi
chmod 600 "$ENVFILE"
echo "env 行数: $(wc -l < "$ENVFILE")"

if [ -n "$RUNNING" ]; then
  echo "=== 停旧容器并改名保留 ==="
  sudo docker stop "$RUNNING"
  if [ "$RUNNING" != "$NAME" ]; then
    # 名字被残留的空容器占着，先把它挪走
    sudo docker rename "$NAME" "${NAME}-stale-${TS}" 2>/dev/null || true
    echo "已把残留容器 ${NAME} 改名为 ${NAME}-stale-${TS}"
  fi
  sudo docker rename "$RUNNING" "$OLD"
  echo "旧容器已保留为: $OLD"

  # 2026-11-02 事故：docker stop 之后宿主机 3000 端口并未立刻释放，
  # 紧接着的 docker run -p 127.0.0.1:3000:3000 会报
  #   "Bind for 127.0.0.1:3000 failed: port is already allocated"
  # 导致新容器起不来（ExitCode=255），线上只剩旧容器在跑。
  # 这里显式等端口真正释放，且带超时，超时则报错退出（不猜）。
  echo "=== 等待 ${PORT} 端口释放 ==="
  PORT_FREE=0
  for i in $(seq 1 30); do
    if ! sudo ss -lnt "sport = :${PORT}" 2>/dev/null | grep -q ":${PORT}"; then
      echo "端口 ${PORT} 已释放（第 ${i} 次检查）"
      PORT_FREE=1
      break
    fi
    sleep 1
  done
  if [ "$PORT_FREE" != "1" ]; then
    echo "!!! 等待 ${PORT} 端口释放超时，当前占用："
    sudo ss -lntp "sport = :${PORT}" 2>/dev/null || true
    echo "!!! 回滚：把旧容器改回原名并启动"
    sudo docker rename "$OLD" "$NAME" 2>/dev/null || sudo docker rename "$OLD" "$RUNNING"
    sudo docker start "$NAME" 2>/dev/null || sudo docker start "$RUNNING"
    exit 1
  fi
fi

echo "=== 启动新容器 ==="
RUN_OK=0
for attempt in 1 2 3; do
  if sudo docker run -d --name "$NAME" \
    --restart always \
    --network new-api_new-api-network \
    --env-file "$ENVFILE" \
    -p 127.0.0.1:${PORT}:3000 \
    -v "${DEPLOY}/data:/data" \
    -v "${DEPLOY}/logs:/app/logs" \
    --log-driver json-file --log-opt max-size=100m --log-opt max-file=3 \
    -w /data \
    "$IMG" --log-dir /app/logs; then
    RUN_OK=1
    echo "启动成功（第 ${attempt} 次尝试）"
    break
  fi
  echo "!!! 启动失败（第 ${attempt} 次尝试），日志："
  sudo docker logs --tail 20 "$NAME" 2>&1 || true
  sudo docker rm -f "$NAME" 2>/dev/null || true
  # 端口又被别的进程占住时，等一下再试
  echo "等待 5 秒后重试..."
  sleep 5
done

if [ "$RUN_OK" != "1" ]; then
  echo "!!! 启动失败，回滚"
  if [ -n "$RUNNING" ]; then
    sudo docker rename "$OLD" "$NAME" 2>/dev/null || sudo docker rename "$OLD" "$RUNNING"
    sudo docker start "$NAME" 2>/dev/null || sudo docker start "$RUNNING"
  fi
  exit 1
fi

echo "=== 健康检查（最多 120s）==="
OK=0
for i in $(seq 1 24); do
  if curl -fsS --max-time 5 http://127.0.0.1:${PORT}/api/status 2>/dev/null | grep -q '"success": *true'; then
    echo "健康检查通过（第 ${i} 次）"; OK=1; break
  fi
  sleep 5
done

if [ "$OK" != "1" ]; then
  echo "!!! 健康检查超时，最近日志："
  sudo docker logs --tail 40 "$NAME" 2>&1 || true
  echo "!!! 回滚"
  sudo docker rm -f "$NAME" 2>/dev/null || true
  if [ -n "$RUNNING" ]; then
    sudo docker rename "$OLD" "$NAME" 2>/dev/null || sudo docker rename "$OLD" "$RUNNING"
    sudo docker start "$NAME" 2>/dev/null || sudo docker start "$RUNNING"
  fi
  exit 1
fi

sudo docker inspect "$NAME" --format 'Image={{.Image}}  Port={{range $p,$c := .NetworkSettings.Ports}}{{$p}}->{{range $c}}{{.HostIp}}:{{.HostPort}}{{end}}{{end}}'
echo "替换完成。回滚：sudo docker rm -f $NAME && sudo docker rename $OLD $NAME && sudo docker start $NAME"
