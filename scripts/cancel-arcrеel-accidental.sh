#!/usr/bin/env bash
# 找出并取消 ArcReel 侧由验证误建的远端 dola 任务（Worker 上线后会被真的执行）
set -u
echo "=== 1. ArcReel 部署形态 ==="
systemctl is-active arcreel 2>/dev/null && echo "(systemd 服务)"
ls -la /home/ubuntu/arcreel/.env 2>/dev/null | head -2

echo
echo "=== 2. 定位 service token（不打印明文，只显示长度）==="
TOKEN=""
for f in /home/ubuntu/arcreel/.env /etc/arcreel.env /home/ubuntu/arcreel/server/.env; do
  if [ -f "$f" ]; then
    T=$(grep -oE '^REMOTE_OPENAPI_SERVICE_TOKEN=.*' "$f" | cut -d= -f2- | tr -d '"'"'"' \r\n')
    if [ -n "$T" ]; then TOKEN="$T"; echo "found in $f (len=${#T})"; break; fi
  fi
done
if [ -z "$TOKEN" ]; then
  T=$(sudo -n systemctl show arcreel --property=Environment 2>/dev/null | grep -oE 'REMOTE_OPENAPI_SERVICE_TOKEN=[^ ]*' | cut -d= -f2-)
  [ -n "$T" ] && { TOKEN="$T"; echo "found in systemd env (len=${#T})"; }
fi
[ -z "$TOKEN" ] && { echo "未找到 token，改为直接查库"; }

PORT=$(systemctl show arcreel --property=ExecStart 2>/dev/null | grep -oE '\-\-port[= ][0-9]+' | grep -oE '[0-9]+' | head -1)
PORT=${PORT:-8000}
echo "ArcReel 端口: $PORT"

echo
echo "=== 3. 列出排队/进行中的远端任务（只读）==="
if [ -n "$TOKEN" ]; then
  curl -s -m 20 -H "Authorization: Bearer $TOKEN" \
    "http://127.0.0.1:${PORT}/api/v1/remote-generation/jobs?status=queued&limit=20" \
    | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
except Exception as e:
    print('parse failed:', e); sys.exit()
items = d.get('data') or d.get('items') or d.get('jobs') or []
if isinstance(items, dict): items = items.get('items', [])
print(f'共 {len(items)} 条')
for j in items:
    print(' ', j.get('job_id') or j.get('id'), '|', j.get('platform_id'), '|', j.get('status'), '|', j.get('created_at'), '|', (j.get('meta') or {}).get('source_task_id',''))
" 2>/dev/null || echo "(解析失败，打印原始前 800 字节)"
  curl -s -m 20 -H "Authorization: Bearer $TOKEN" \
    "http://127.0.0.1:${PORT}/api/v1/remote-generation/jobs?status=queued&limit=20" | head -c 800
else
  echo "(无 token，跳过 API 查询)"
fi

echo
echo "=== 4. 从日志里找验证时间窗内新建的任务 ==="
grep -nE "remote-generation/jobs" /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null | tail -12 | cut -c1-200