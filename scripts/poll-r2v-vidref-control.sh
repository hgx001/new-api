#!/usr/bin/env bash
# 收拾对照组误建的任务 task_EULw1MShH6kaZKSG9oBLtwyxJjWOwwJb（wan3.0-smart + 参考视频）。
# 与其删掉不如查清它到底通不通 —— 这正是用户要的「视频参考 5 秒」真机验证。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
TASK_ID=task_EULw1MShH6kaZKSG9oBLtwyxJjWOwwJb

echo "=== 轮询最多 10 分钟 ==="
for i in $(seq 1 20); do
  ROW=$(Q "SELECT status || '|' || coalesce(progress,'') || '|' || coalesce(private_data->'billing_context'->'other_ratios'->>'seconds','?') || '|' || coalesce(private_data->'billing_context'->'other_ratios'->>'size','?') || '|' || left(coalesce(fail_reason,''),50) || '|' || coalesce((data::jsonb->'result'->>'costPoints'),'-')
            FROM tasks WHERE task_id='$TASK_ID';")
  ST=$(echo "$ROW" | cut -d'|' -f1)
  echo "[$(date +%H:%M:%S)] $ROW"
  [ "$ST" = "SUCCESS" ] || [ "$ST" = "FAILURE" ] && break
  sleep 30
done

echo
echo "=== 终态明细 ==="
Q "SELECT 'status', status FROM tasks WHERE task_id='$TASK_ID';"
Q "SELECT 'fail_reason', coalesce(fail_reason,'(none)') FROM tasks WHERE task_id='$TASK_ID';"
Q "SELECT '上游 costPoints', coalesce((data::jsonb->'result'->>'costPoints'),'-') FROM tasks WHERE task_id='$TASK_ID';"
Q "SELECT '客户侧扣费 quota', quota FROM tasks WHERE task_id='$TASK_ID';"
Q "SELECT '产物', left(coalesce(private_data->>'result_url','-'),100) FROM tasks WHERE task_id='$TASK_ID';"
