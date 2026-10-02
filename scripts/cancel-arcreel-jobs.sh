#!/usr/bin/env bash
# 取消 ArcReel 侧由验证误建的两个 dola 远端任务（否则 Worker 上线会真的执行）
set -u
TOKEN=$(grep -oE '^REMOTE_OPENAPI_SERVICE_TOKEN=.*' /home/ubuntu/arcreel/.env | cut -d= -f2- | tr -d '"'"'"' \r\n')
BASE="http://127.0.0.1:1241/api/v1"
AUTH="Authorization: Bearer $TOKEN"

for JOB in gen-a650346d1a91327dfc391a0d gen-6f8143b879672476944ebb45; do
  echo "=== $JOB ==="
  echo -n "  当前状态: "
  curl -s -m 15 -H "$AUTH" "$BASE/remote-generation/jobs/$JOB" \
    | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    print('(非 JSON 响应)'); sys.exit()
j = d.get('data') or d
print(j.get('status'), '| platform=', j.get('platform_id'), '| created=', j.get('created_at'), '| err=', (j.get('error') or '')[:60])
" 2>/dev/null || echo "(查询失败)"
  echo -n "  取消: "
  curl -s -m 20 -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"reason":"cancelled by operator: verification-only task"}' \
    "$BASE/remote-generation/jobs/$JOB/cancel" | head -c 200
  echo
  echo -n "  取消后状态: "
  curl -s -m 15 -H "$AUTH" "$BASE/remote-generation/jobs/$JOB" \
    | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    print('(非 JSON)'); sys.exit()
j = d.get('data') or d
print(j.get('status'))
" 2>/dev/null || echo "(查询失败)"
done

echo
echo "=== 剩余排队任务（Worker 上线会被接走）==="
curl -s -m 20 -H "$AUTH" "$BASE/remote-generation/jobs?limit=50" | head -c 400
echo
echo "--- Worker 状态 ---"
curl -s -m 15 -H "$AUTH" "$BASE/remote-generation/workers/status" | head -c 500