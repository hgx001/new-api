#!/usr/bin/env bash
# 退还我误建的探针任务 task_ikCDNMBdg5g3inYeomeQn1t1UvhnlAB0（wan2.7-r2v，¥0.5）。
# 沿用 refund-restart-verify.sh 的既定口径：同时改 users.quota 与 tokens.used_quota，
# 并在计费日志上留痕。改完比对 Redis 里的令牌额度，确认没有缓存分叉。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
TASK=task_ikCDNMBdg5g3inYeomeQn1t1UvhnlAB0

echo "=== 退款前 ==="
Q "SELECT 'task', id, status, quota, user_id, private_data->>'token_id' FROM tasks WHERE task_id='$TASK';"
Q "SELECT 'user', id, quota FROM users WHERE id=(SELECT user_id FROM tasks WHERE task_id='$TASK');"
Q "SELECT 'token', id, used_quota FROM tokens WHERE id=(SELECT (private_data->>'token_id')::int FROM tasks WHERE task_id='$TASK');"
Q "SELECT 'log', id, type, quota FROM logs WHERE content LIKE '%$TASK%' ORDER BY id;"

USER_ID=$(Q "SELECT user_id FROM tasks WHERE task_id='$TASK';" | tr -d ' ')
TOKEN_ID=$(Q "SELECT (private_data->>'token_id')::int FROM tasks WHERE task_id='$TASK';" | tr -d ' ')
QUOTA=$(Q "SELECT quota FROM tasks WHERE task_id='$TASK';" | tr -d ' ')

echo
echo "=== 执行退款 quota=$QUOTA  user=$USER_ID  token=$TOKEN_ID ==="
Q "UPDATE users SET quota = quota + $QUOTA WHERE id = $USER_ID;" >/dev/null
Q "UPDATE tokens SET used_quota = GREATEST(used_quota - $QUOTA, 0) WHERE id = $TOKEN_ID;" >/dev/null
Q "UPDATE logs SET content = content || ' [REFUNDED-BY-OPERATOR: diagnostic probe, task succeeded upstream]'
   WHERE content LIKE '%$TASK%' AND type = 2;" >/dev/null

echo
echo "=== 退款后 ==="
Q "SELECT 'user', id, quota FROM users WHERE id=$USER_ID;"
Q "SELECT 'token', id, used_quota FROM tokens WHERE id=$TOKEN_ID;"
Q "SELECT 'log', id, content FROM logs WHERE content LIKE '%$TASK%' AND type=2 ORDER BY id;"

echo
echo "=== Redis 令牌额度是否与 DB 分叉（分叉则需重启刷缓存）==="
RKEY=$(docker exec redis redis-cli --scan --pattern '*token*' 2>/dev/null | head -5)
echo "redis token 相关 key: ${RKEY:-（无）}"
docker exec redis redis-cli --scan --pattern "*${TOKEN_ID}*" 2>/dev/null | head -5 || true
