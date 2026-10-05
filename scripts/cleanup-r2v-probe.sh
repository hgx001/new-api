#!/usr/bin/env bash
# 检查并撤销刚才误建的 wan2.7-r2v 任务（探针入参意外通过校验）。
set -uo pipefail
SSH=(ssh -p 877 -o ConnectTimeout=15 ubuntu@119.29.253.97)
Q() { printf '%s' "$1" | "${SSH[@]}" "docker exec -i postgres psql -U root -d new-api -t -A -F' | '" | tr -d '\r'; }
TASK=task_ikCDNMBdg5g3inYeomeQn1t1UvhnlAB0

echo "=== 任务当前状态 ==="
Q "SELECT id, task_id, status, quota, properties->>'origin_model_name', private_data->>'upstream_task_id', left(coalesce(fail_reason,''),60) FROM tasks WHERE task_id='$TASK';"

ST=$(Q "SELECT status FROM tasks WHERE task_id='$TASK';")
UP=$(Q "SELECT coalesce(private_data->>'upstream_task_id','') FROM tasks WHERE task_id='$TASK';")
echo "status=$ST  upstream=$UP"

if [ "$ST" != "SUCCESS" ] && [ "$ST" != "FAILURE" ] && [ "$ST" != "CANCELLED" ]; then
  echo
  echo "=== 撤销：new-api 取消 ==="
  curl -s -o /tmp/c.json -w 'HTTP=%{http_code}\n' -X POST \
    "https://api.heibaidao.cn/v1/videos/$TASK/cancel" \
    -H "Authorization: Bearer $Q_TOKEN"
  head -c 200 /tmp/c.json; echo

  if [ -n "$UP" ]; then
    echo
    echo "=== 撤销：ArcReel 取消远端 job（只撤一侧的话 Worker 上线后仍会去跑）==="
    K=$(Q "SELECT key FROM channels WHERE id=17;")
    "${SSH[@]}" "curl -s -o /dev/null -w 'arcreel cancel HTTP=%{http_code}\n' -X POST \
      -H 'Authorization: Bearer $K' \
      'https://arcreel.heibaidao.cn/api/v1/remote-generation/jobs/$UP/cancel'"
    "${SSH[@]}" "python3 /tmp/arc_q.py \"select job_id,status,cancelled_by,updated_at from remote_generation_jobs where job_id='$UP'\"" 2>/dev/null || echo "(ArcReel 查不到该 job)"
  fi
fi

echo
echo "=== 撤销后复核：非终态任务 / 额度 ==="
Q "SELECT '非终态任务', count(*) FROM tasks WHERE status NOT IN ('SUCCESS','FAILURE','CANCELLED');"
Q "SELECT '本任务', id, status, quota, left(coalesce(fail_reason,''),60) FROM tasks WHERE task_id='$TASK';"
