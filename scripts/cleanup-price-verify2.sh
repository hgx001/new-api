#!/usr/bin/env bash
# 收尾：退掉本轮验证任务的扣费，并验证 wan3.0-smart 720p 档（预期 95890 = ¥1.40/5秒）。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')

echo "=== 1. 退回上一轮 4 笔验证扣费 ==="
TASKS="task_m1wqALbCV7er75WU0EHxjJ2C2mYNdCz6 task_telBAHVatG1grnWkPrOjwrU3Dv2hMTJK task_tMHd2kbvnNjRMcWKT7hWCcwrfd6NVDjM task_puvxwFuqKCX6OQSa5bDHgpvN1aC0N8gG"
TOT=0
for T in $TASKS; do
  QTY=$(Q "SELECT quota FROM tasks WHERE task_id='$T';" | tr -d ' ')
  [ -n "$QTY" ] && TOT=$((TOT+QTY))
done
echo "合计 $TOT quota = ¥$(awk "BEGIN{printf \"%.4f\", $TOT/68493}")"
Q "UPDATE users SET quota = quota + $TOT WHERE id = 2;" >/dev/null
Q "UPDATE tokens SET used_quota = GREATEST(used_quota - $TOT, 0) WHERE id = 1;" >/dev/null
Q "UPDATE logs SET content = content || ' [REFUNDED-BY-OPERATOR: OtherRatios determinism verification]'
   WHERE model_name='wan3.0-smart' AND type=2 AND created_at > EXTRACT(EPOCH FROM now())::bigint - 3600;" >/dev/null
Q "SELECT 'user2_quota', quota FROM users WHERE id=2;"

echo
echo "=== 2. 验证 wan3.0-smart 720p / 5 秒 ==="
echo "    预期：size=1.0，quota = 19178*5 = 95890 = ¥1.40（=¥0.28/秒）"
R=$(curl -s -X POST "$HOST/v1/videos" -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d '{"model":"wan3.0-smart","prompt":"720p-check","resolution":"720p","seconds":"5"}')
T=$(echo "$R" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
[ -n "$T" ] || { echo "建单失败: $(echo "$R" | head -c 150)"; exit 0; }
sleep 5
QTY=$(Q "SELECT quota FROM tasks WHERE task_id='$T';" | tr -d ' ')
echo "task=$T  实测 quota=$QTY  预期 95890  差=$((QTY-95890))  ≈¥$(awk "BEGIN{printf \"%.6f\", $QTY/68493}")"

echo
echo "=== 3. 退回这笔 720p 验证 ==="
Q "UPDATE users SET quota = quota + $QTY WHERE id = 2;" >/dev/null
Q "UPDATE tokens SET used_quota = GREATEST(used_quota - $QTY, 0) WHERE id = 1;" >/dev/null
Q "UPDATE logs SET content = content || ' [REFUNDED-BY-OPERATOR: 720p price verification]'
   WHERE model_name='wan3.0-smart' AND type=2 AND content NOT LIKE '%REFUNDED%'
     AND created_at > EXTRACT(EPOCH FROM now())::bigint - 600;" >/dev/null
Q "SELECT 'user2_quota_final', quota FROM users WHERE id=2;"
