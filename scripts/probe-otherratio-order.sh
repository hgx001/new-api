#!/usr/bin/env bash
# 验证假设：relay_task.go 的 OtherRatios 逐个 int() 截断 + Go map 随机迭代序，
# 导致同样的请求两次扣费可能不同。多打几次看是否出现两个值。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')

echo "理论值：base=int(0.038356164383561646*500000)=19178"
echo "  size 先: int(19178*0.30/0.28)=$((19178*30/28)) → 再 int(*5) = $((19178*30/28*5))"
echo "  seconds 先: int(19178*5)=$((19178*5)) → 再 int(*0.30/0.28) = $((19178*5*30/28))"
echo
for i in 1 2 3 4 5 6; do
  R=$(curl -s -X POST "$HOST/v1/videos" -H "Authorization: Bearer $TK" \
    -H 'Content-Type: application/json' \
    -d '{"model":"wan3.0-smart","prompt":"order-check","resolution":"1080p","seconds":"5"}')
  T=$(echo "$R" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$T" ] || { echo "[$i] 建单失败: $(echo "$R" | head -c 120)"; continue; }
  sleep 3
  QTY=$(Q "SELECT quota FROM tasks WHERE task_id='$T';" | tr -d ' ')
  echo "[$i] task=$T quota=$QTY  ≈¥$(awk "BEGIN{printf \"%.6f\", $QTY/68493}")"
done
