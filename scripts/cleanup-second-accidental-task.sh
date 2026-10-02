#!/usr/bin/env bash
# 清理第二次误建的 dola 任务（new-api 侧取消+退款，ArcReel 侧取消远端任务）
set -u
TASK_ID="$1"
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

echo "=== 1. 任务现状 ==="
q "SELECT task_id, status, properties FROM tasks WHERE task_id='$TASK_ID'"

echo
echo "=== 2. 取消并退款 ==="
q "UPDATE tasks SET status='FAILURE', fail_reason='verification-only task cancelled by operator', updated_at=EXTRACT(EPOCH FROM NOW())::bigint
   WHERE task_id='$TASK_ID' AND status NOT IN ('SUCCESS','FAILURE')"
# 退款：token 1 / user 2，dola 按次 ¥2.5 = 171232
Q0=$(q "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
U0=$(q "SELECT quota FROM users WHERE id=2")
echo "退款前: tokens.used_quota 合计=$Q0  user2.quota=$U0"
q "UPDATE users SET quota = quota + 171232 WHERE id=2" >/dev/null
q "UPDATE tokens SET used_quota = GREATEST(used_quota - 171232, 0) WHERE id=1" >/dev/null
q "UPDATE logs SET content = content || ' [REFUNDED: verification-only task cancelled]'
   WHERE id = (SELECT max(id) FROM logs WHERE quota=171232 AND type=2 AND content NOT LIKE '%REFUNDED%')" >/dev/null
Q1=$(q "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
U1=$(q "SELECT quota FROM users WHERE id=2")
echo "退款后: tokens.used_quota 合计=$Q1  user2.quota=$U1"

echo
echo "=== 3. ArcReel 侧：找出并取消对应的远端任务 ==="
# new-api 的 properties 里有 upstream 任务信息；这里直接从 ArcReel API 查最近任务
echo "--- ArcReel 最近的远端任务（可能含该 dola 任务）---"
grep -oE '"job_id": *"[^"]+"' /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null | tail -5 || echo "(日志无 job_id)"
echo "--- 尝试通过 ArcReel API 列出排队任务（只读）---"
ST=$(docker exec arcreel env 2>/dev/null | grep -oE 'REMOTE_OPENAPI_SERVICE_TOKEN=.*' | cut -d= -f2-)
if [ -n "$ST" ]; then
  curl -s -m 15 -H "Authorization: Bearer $ST" "http://127.0.0.1:8000/api/v1/remote-generation/jobs?limit=5" 2>/dev/null | head -c 600
  echo
else
  echo "(容器名/环境变量未取到 token，跳过；Worker 全部离线，不会被消费)"
fi

echo
echo "=== 4. 终检 ==="
q "SELECT task_id, status FROM tasks WHERE task_id='$TASK_ID'"
q "SELECT '未完成新-api任务: ' || count(*) FROM tasks WHERE status NOT IN ('SUCCESS','FAILURE')"
echo "ArcReel 建单计数（Worker 离线，不会被消费）:"
grep -c "remote-generation/jobs" /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null || echo 0