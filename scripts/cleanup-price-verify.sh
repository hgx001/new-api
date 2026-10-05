#!/usr/bin/env bash
# 清理调价验证产生的 wan3.0-smart 测试任务：查状态 → 退客户侧扣费 → 日志留痕。
# 上游有赞侧已真实消耗的点数无法撤销，只退客户侧并标记，避免脏账。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
TASKS="task_hhCJWV6Ngy6RHICG27Y9gbfM0uV3F08j task_7A1g0gVxKbO2FkKkQ96qP6VgDTd2GHg6 task_X6NLurpMU28tco9v4ceZbR0RoU8LoWPt task_Z5Nxdv4FtIi8CsFNEXgqq0Tvdvr9zBPg"

echo "=== 现状 ==="
Q "SELECT id, task_id, status, quota, user_id, private_data->>'token_id' AS tok, to_timestamp(created_at)
   FROM tasks WHERE task_id IN ($(echo $TASKS | tr ' ' ',' | sed 's/,/,/g;s/^/('/;s/$/)/')) ORDER BY id;"

TOTAL=0
for T in $TASKS; do
  QTY=$(Q "SELECT quota FROM tasks WHERE task_id='$T';" | tr -d ' ')
  [ -n "$QTY" ] || continue
  TOTAL=$((TOTAL+QTY))
done
echo "待退合计 quota=$TOTAL（≈¥$(awk "BEGIN{printf \"%.4f\", $TOTAL/68493}")）"

echo
echo "=== 退款：users.quota 与 tokens.used_quota 同步 ==="
Q "UPDATE users SET quota = quota + $TOTAL WHERE id = 2;"
Q "UPDATE tokens SET used_quota = GREATEST(used_quota - $TOTAL, 0) WHERE id = 1;"
Q "UPDATE logs SET content = content || ' [REFUNDED-BY-OPERATOR: pricing verification task $TASKS]'
   WHERE model_name='wan3.0-smart' AND type=2 AND created_at > EXTRACT(EPOCH FROM now())::bigint - 7200;"

echo
echo "=== 复核 ==="
Q "SELECT 'user2_quota', quota FROM users WHERE id=2;"
Q "SELECT 'token1_used', used_quota FROM tokens WHERE id=1;"
Q "SELECT '本轮退款日志', count(*) FROM logs WHERE content LIKE '%pricing verification task%';"
Q "SELECT '这些任务', id, status, quota FROM tasks WHERE task_id IN ($(echo $TASKS | tr ' ' ',' | sed 's/^/('/;s/$/)/')) ORDER BY id;"
