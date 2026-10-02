#!/usr/bin/env bash
# 带 SHA 断言的 new-api 部署脚本（避免"构建了旧 commit 却以为成功"）
# 用法: bash deploy-new-api.sh <short-sha>
set -euo pipefail
TARGET="${1:?need target sha}"
cd /home/ubuntu/new-api-src

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

docker build -f Dockerfile.deploy -t new-api-custom:latest . 2>&1 | tail -1

TS=$(date +%Y%m%d%H%M%S)
docker inspect new-api --format '{{range .Config.Env}}{{println .}}{{end}}' > "/tmp/newapi-$TS.env"
chmod 600 "/tmp/newapi-$TS.env"
docker stop new-api >/dev/null
docker rename new-api "new-api-prev-$TS"
echo "OLD_KEPT: new-api-prev-$TS"

docker run -d --name new-api --restart always -p 127.0.0.1:3000:3000 \
  -v /home/ubuntu/new-api/data:/data -v /home/ubuntu/new-api/logs:/app/logs \
  --network new-api_new-api-network --env-file "/tmp/newapi-$TS.env" \
  new-api-custom:latest --log-dir /app/logs >/dev/null

for i in $(seq 1 24); do
  if wget -q -O - http://127.0.0.1:3000/api/status 2>/dev/null | grep -q '"success": *true'; then
    echo "HEALTH_OK ${i}x5s"; break
  fi
  [ "$i" = 24 ] && { docker logs new-api --tail 15; exit 1; }
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