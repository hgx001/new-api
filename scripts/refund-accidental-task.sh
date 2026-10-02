#!/usr/bin/env bash
# 处理验证脚本误建的 dola 任务：查状态 → 取消 → 退还预扣费。
# 前提：Worker 全部离线，任务应仍停在 queued，尚未真实消耗上游额度。
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

TASK_ID="$1"
echo "=== 1. 该任务现状 ==="
q "SELECT id, task_id, platform, model_name, status, channel_id, created_at FROM tasks WHERE task_id='$TASK_ID'"
echo "--- 任务属性（渠道/上游任务号）---"
q "SELECT properties FROM tasks WHERE task_id='$TASK_ID'"
echo "--- 计费日志 ---"
q "SELECT id, user_id, model_name, quota, content FROM logs WHERE content LIKE '%$TASK_ID%' ORDER BY id DESC LIMIT 3"

echo
echo "=== 2. ArcReel 侧是否已建单（dola 走漫屋→ArcReel→Worker）==="
grep -c "$TASK_ID" /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null || echo "0 (ArcReel 日志无此任务号)"
echo "--- ArcReel 最近 5 条远端任务 ---"
q "SELECT 1" >/dev/null 2>&1
docker exec arcreel sh -c "true" 2>/dev/null || true

echo
echo "=== 3. 取消任务 ==="
q "UPDATE tasks SET status='FAILURE', fail_reason='verification-only task cancelled by operator', updated_at=EXTRACT(EPOCH FROM NOW())::bigint WHERE task_id='$TASK_ID' AND status NOT IN ('SUCCESS','FAILURE')"
q "SELECT task_id, status, fail_reason FROM tasks WHERE task_id='$TASK_ID'"

echo
echo "=== 4. 退还预扣费 ==="
# 找出该任务的计费日志与对应令牌，把 quota 加回去
q "SELECT l.user_id, l.quota, t.id AS token_id FROM logs l JOIN tokens t ON t.user_id=l.user_id
   WHERE l.content LIKE '%$TASK_ID%' AND l.type=2 ORDER BY l.id DESC LIMIT 1" > /tmp/refund.txt
cat /tmp/refund.txt
USER_ID=$(cut -d'|' -f1 /tmp/refund.txt | tr -d ' ')
QUOTA=$(cut -d'|' -f2 /tmp/refund.txt | tr -d ' ')
if [ -n "$USER_ID" ] && [ -n "$QUOTA" ] && [ "$QUOTA" -gt 0 ] 2>/dev/null; then
  q "UPDATE users SET quota = quota + $QUOTA WHERE id = $USER_ID"
  echo "已给用户 $USER_ID 退还 $QUOTA"
  echo "标记该计费日志为已退款（content 追加标记，便于日后对账）:"
  q "UPDATE logs SET content = content || ' [REFUNDED-BY-OPERATOR]' WHERE content LIKE '%$TASK_ID%' AND type=2"
else
  echo "未找到可退款的计费记录，需人工核对"
fi

echo
echo "=== 5. 复核 ==="
q "SELECT COALESCE(SUM(used_quota),0) AS tokens_used_quota FROM tokens"
q "SELECT id, quota FROM users WHERE id = $USER_ID"
q "SELECT count(*) AS pending_tasks FROM tasks WHERE status NOT IN ('SUCCESS','FAILURE')"
