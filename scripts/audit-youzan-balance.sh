#!/usr/bin/env bash
# 有赞上游余额对账（ch17）。上游没有余额查询 API，余额只能人工抄；
# 本脚本算「节点后系统记录的上游扣费」，与人工抄的余额对比即可发现异常扣款。
#
# 用法（生产服务器上执行，或本地 ssh 过去跑）：
#   bash scripts/audit-youzan-balance.sh              # 默认节点
#   bash scripts/audit-youzan-balance.sh 3100         # 传入最近人工抄的余额
#   bash scripts/audit-youzan-balance.sh 3100 <epoch> # 换对账节点（起点）
set -uo pipefail

# 对账节点：2026-11-12 14:16:44 +08，余额 ¥3100（用户人工抄数），
# 节点前累计：596 条 charged、ΣquotedAmount ¥1193.86。
NODE_EPOCH="${2:-1794464204}"
LAST_SEEN_BALANCE="${1:-}"

Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }

echo "=== 对账节点：$(date -d "@$NODE_EPOCH" '+%F %T %z') ==="
echo

echo "=== 节点后新增的上游扣费（charged 任务）==="
Q "SELECT count(*),
          round(coalesce(sum((data::jsonb->'task'->>'quotedAmount')::numeric),0),2)
     FROM tasks
    WHERE channel_id = 17
      AND data::jsonb->'task'->>'billingState' = 'charged'
      AND created_at > $NODE_EPOCH;"

echo
echo "=== 全量累计（含节点前 596 条 / ¥1193.86，用于核对起点没漂移）==="
Q "SELECT count(*),
          round(coalesce(sum((data::jsonb->'task'->>'quotedAmount')::numeric),0),2),
          to_timestamp(min(created_at))::text,
          to_timestamp(max(created_at))::text
     FROM tasks
    WHERE channel_id = 17
      AND data::jsonb->'task'->>'billingState' = 'charged';"

echo
echo "=== 报价与单价不一致的任务（quotedAmount ≠ unitPrice×quantity，按任务多扣的嫌疑）==="
Q "SELECT id, task_id,
          data::jsonb->'task'->>'unitPrice'   AS unit_price,
          data::jsonb->'task'->>'quantity'     AS quantity,
          data::jsonb->'task'->>'quotedAmount' AS quoted
     FROM tasks
    WHERE channel_id = 17
      AND data::jsonb->'task'->>'billingState' = 'charged'
      AND created_at > $NODE_EPOCH
      AND abs(
            (data::jsonb->'task'->>'quotedAmount')::numeric
            - (data::jsonb->'task'->>'unitPrice')::numeric
              * (data::jsonb->'task'->>'quantity')::numeric
          ) > 0.005
    ORDER BY created_at;"

echo
if [ -n "$LAST_SEEN_BALANCE" ]; then
  SINCE=$(Q "SELECT coalesce(round(sum((data::jsonb->'task'->>'quotedAmount')::numeric),2),0)
               FROM tasks
              WHERE channel_id = 17
                AND data::jsonb->'task'->>'billingState' = 'charged'
                AND created_at > $NODE_EPOCH;")
  EXPECTED=$(awk -v b="$LAST_SEEN_BALANCE" -v s="$SINCE" 'BEGIN{printf "%.2f", b-s}')
  echo "人工抄的余额：¥$LAST_SEEN_BALANCE"
  echo "节点后系统记录扣费：¥$SINCE"
  echo "预期下次余额：¥$EXPECTED（人工抄数与此差 >¥0.05 即有未经任务的扣款，需查上游）"
else
  echo "未传入余额。抄一次上游管理台余额后执行："
  echo "  bash scripts/audit-youzan-balance.sh <余额>"
fi
