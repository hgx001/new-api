#!/usr/bin/env bash
# 决定性验证：问 erchun 上游「你这条任务到底收到了什么」。
# new-api 侧日志不一定记录请求体，但上游会如实回报它入库的素材。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
BASE="https://api.erchun.youkou.cc"
KEY=$(printf '%s' "SELECT key FROM channels WHERE id=19;" | docker exec -i postgres psql -U root -d new-api -t -A | tr -d '\r\n ')
TID="task_EL5uGBsiedNkrCIwCnJYbFEJpZQ8MeJp"
UP=$(Q "SELECT coalesce(private_data->>'upstream_task_id','(无)') FROM tasks WHERE task_id='$TID';")
echo "本地 task_id : $TID"
echo "上游 task_id : $UP"
echo
echo "=== GET /v1/tasks/$UP ==="
curl -s -H "Authorization: Bearer $KEY" "$BASE/v1/tasks/$UP" | python3 -m json.tool 2>/dev/null | head -60
echo
echo "=== 容器名（供后续查日志）==="
docker ps --format '{{.Names}}' | head -5
