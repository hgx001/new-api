#!/usr/bin/env bash
# gemini-web-video 端到端真机验证（**会真建单并扣 ¥1.0**，与 probe-manwu-dola-dual.sh 的零成本探针不同）。
#
# 用途：确认「ArcReel 托管产物（相对路径 sourceUrl）→ new-api /content 代理回源」这条链路
# 真的能出片。历史事故 task 238（2026-10-03）就是在这条链路上被判 24h 超时失败，
# 而上游 job gen-6682493b9ae987d87365ed97 其实在 10 小时后成功出了片。
#
# 前置检查：ArcReel worker 必须 online 且 gemini 账号 can_submit=1，否则建单会一直
# 排队到 24h 超时（那正是 task 238 的失败原因，不是 new-api 的 bug）。
set -uo pipefail

HOST="https://api.heibaidao.cn"
SSH=(ssh -p 877 ubuntu@119.29.253.97)
PG='docker exec postgres psql -U root -d new-api -t -A'

run_sql() { "${SSH[@]}" "$PG -c \"$1\"" | tr -d '\r\n '; }
"${SSH[@]}" "cat > /tmp/arc_q.py" < "$(dirname "$0")/arc_q.py"
arc_sql() { "${SSH[@]}" "python3 /tmp/arc_q.py \"$1\"" | tr -d '\r'; }

echo "=== 前置：worker / gemini 账号 ==="
arc_sql "select worker_id,status,last_heartbeat_at,running_count from remote_worker_nodes where status='online'"
arc_sql "select account_id,status,can_submit,running_count,max_concurrent from remote_worker_accounts where platform_id='gemini'"
ONLINE=$(arc_sql "select count(*) from remote_worker_nodes where status='online'")
[[ "$ONLINE" == "1" ]] || { echo "ABORT: 没有 online worker，建单必排队到超时"; exit 1; }

TK=$(run_sql "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1")
[[ -n "$TK" ]] || { echo "ABORT: no usable token"; exit 1; }

Q0=$(run_sql "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
OPEN0=$(run_sql "SELECT count(*) FROM tasks WHERE status NOT IN ('SUCCESS','FAILURE','CANCELLED')")
echo "=== 起始：sum(token.used_quota)=$Q0  非终态任务=$OPEN0 ==="

IDEM="gve2e-$(date -u +%Y%m%d%H%M%S)"
echo "=== 提交 gemini-web-video（idempotency_key=$IDEM，真实预扣 ¥1.0 = 68493 quota）==="
RESP=$(curl -s -X POST "$HOST/v1/videos" -H "Authorization: Bearer $TK" \
  -H 'Content-Type: application/json' \
  --data-raw "{\"model\":\"gemini-web-video\",\"prompt\":\"一只白色小猫在阳光草地上缓慢行走，电影感，自然光，画面稳定\",\"ratio\":\"16:9\",\"idempotency_key\":\"$IDEM\"}")
echo "$RESP" | head -c 400; echo
TASK_ID=$(echo "$RESP" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
[[ -n "$TASK_ID" ]] || { echo "ABORT: 提交未返回 task id"; exit 1; }
echo "task_id=$TASK_ID"

UPSTREAM=$(run_sql "SELECT coalesce(private_data->>'upstream_task_id','') FROM tasks WHERE task_id='$TASK_ID'")
echo "upstream_job=$UPSTREAM"

STATUS=""
for i in $(seq 1 80); do
  sleep 15
  Q=$(curl -s "$HOST/v1/videos/$TASK_ID" -H "Authorization: Bearer $TK")
  ST=$(echo "$Q" | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
  PR=$(echo "$Q" | sed -n 's/.*"progress"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
  echo "  [${i}] $ST $PR"
  [[ "$ST" == "completed" || "$ST" == "failed" ]] && { STATUS="$ST"; break; }
  STATUS="$ST"
done

echo "=== 最终查询响应 ==="
curl -s "$HOST/v1/videos/$TASK_ID" -H "Authorization: Bearer $TK" | head -c 800; echo

echo "=== /content 代理回源（这是 ArcReel 托管产物的唯一交付路径）==="
CODE=$(curl -s -o /tmp/gve2e.mp4 -w '%{http_code}' "$HOST/v1/videos/$TASK_ID/content" -H "Authorization: Bearer $TK")
SIZE=$(wc -c < /tmp/gve2e.mp4 | tr -d ' ')
CTYPE=$(curl -s -o /dev/null -D - "$HOST/v1/videos/$TASK_ID/content" -H "Authorization: Bearer $TK" 2>/dev/null | grep -i '^content-type' | tr -d '\r')
echo "HTTP=$CODE  bytes=$SIZE  $CTYPE"
head -c 200 /tmp/gve2e.mp4 | tr -d '\0' ; echo

echo "=== ArcReel 侧 job 终态 ==="
arc_sql "select job_id,status,source_url,error_message from remote_generation_jobs where job_id='$UPSTREAM'"

Q1=$(run_sql "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
OPEN1=$(run_sql "SELECT count(*) FROM tasks WHERE status NOT IN ('SUCCESS','FAILURE','CANCELLED')")
echo "=== 结束：sum(token.used_quota)=$Q1（差值 $((Q1-Q0))，¥1.0 = 68493 quota = 0.1369863 USD）  非终态任务=$OPEN1 ==="
echo "RESULT: status=$STATUS  content_http=$CODE  bytes=$SIZE"
