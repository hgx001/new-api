#!/usr/bin/env bash
# 定位 task 249 为何没被轮询：platform 取值 / 适配器是否存在 / 轮询日志。
set -uo pipefail
SSH=(ssh -p 877 -o ConnectTimeout=15 ubuntu@119.29.253.97)
Q() { printf '%s' "$1" | "${SSH[@]}" "docker exec -i postgres psql -U root -d new-api -t -A -F' | '" | tr -d '\r'; }

echo "=== A. 全部非终态任务的 platform（看 platform 是否为空/异常）==="
Q "SELECT id, status, platform, action, channel_id, properties->>'origin_model_name', progress FROM tasks WHERE status NOT IN ('SUCCESS','FAILURE','CANCELLED') ORDER BY id;"

echo
echo "=== B. 历史成功过的 wan2.7-r2v 任务用的 platform ==="
Q "SELECT DISTINCT platform, status, count(*) FROM tasks WHERE properties->>'origin_model_name'='wan2.7-r2v' GROUP BY 1,2;"

echo
echo "=== C. 渠道 17 的 type（决定 TaskPlatform 字符串）==="
Q "SELECT id, name, type, status FROM channels WHERE id=17;"

echo
echo "=== D. 轮询日志：最近 30 分钟的全部 '任务进度轮询' + 'Channel #' + 'UpdateVideoTasks fail' ==="
"${SSH[@]}" "docker logs new-api --since 30m 2>&1 | grep -E '任务进度轮询|Channel #[0-9]+ pending|UpdateVideoTasks fail|failed for task|error for task' | tail -25"

echo
echo "=== E. 容器启动至今，日志里出现过 channel 17 / 渠道 #17 吗 ==="
"${SSH[@]}" "docker logs new-api 2>&1 | grep -cE 'Channel #17|渠道 #17' || true"
"${SSH[@]}" "docker logs new-api 2>&1 | grep -E 'Channel #[0-9]+ pending' | sort | uniq -c | sort -rn | head -10"
