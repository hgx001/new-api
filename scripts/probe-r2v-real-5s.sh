#!/usr/bin/env bash
# wan2.7-r2v 真机验证：**只跑 1 个**任务（AGENTS.md 付费探针纪律第 3 条）。
# 硬约束（constants.go）：时长仅 5s/10s、比例 16:9/9:16/1:1、参考图最多 3 张、分辨率仅 720p/1080p。
# 定价 ¥0.10/秒 × 5s = ¥0.50。
#
# 退款必须按任务记录里的真实 user_id / token_id 走，绝不写死 id
# —— 2026-10-06 教训：task 314 计在 user 28 上，退款脚本却硬编码 user 2，
# 钱退给了错账号，还把日志标成「已退款」，账面平的、实际没退。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')
IMG="https://picsum.photos/id/237/640/480.jpg"
B_T=$(Q "SELECT count(*) FROM tasks;")

echo "=== 建单：wan2.7-r2v / 5 秒 / 16:9 / 1 张参考图 ==="
RESP=$(curl -s -X POST "$HOST/v1/videos" -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d "{\"model\":\"wan2.7-r2v\",\"prompt\":\"the dog slowly turns its head toward the camera, natural light\",\"seconds\":\"5\",\"ratio\":\"16:9\",\"resolution\":\"720p\",\"media\":[{\"type\":\"reference_image\",\"url\":\"$IMG\"}]}")
echo "$RESP" | head -c 300; echo
TID=$(echo "$RESP" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\(task_[^"]*\)".*/\1/p' | head -1)
[ -n "$TID" ] || { echo "建单失败，未产生扣费"; exit 1; }
echo "task_id=$TID"

echo; echo "=== 轮询（最多 8 分钟）==="
for i in $(seq 1 16); do
  sleep 30
  ROW=$(Q "SELECT status||'|'||coalesce(progress,'')||'|'||left(coalesce(fail_reason,''),40) FROM tasks WHERE task_id='$TID';")
  echo "[$(date +%H:%M:%S)] $ROW"
  ST=$(echo "$ROW" | cut -d'|' -f1)
  [ "$ST" = "SUCCESS" ] || [ "$ST" = "FAILURE" ] && break
done

echo; echo "=== 终态 ==="
Q "SELECT 'status', status FROM tasks WHERE task_id='$TID';"
Q "SELECT 'fail_reason', coalesce(fail_reason,'(none)') FROM tasks WHERE task_id='$TID';"
Q "SELECT '实际计费 seconds/size', coalesce(private_data->'billing_context'->'other_ratios'->>'seconds','-')||' / '||coalesce(private_data->'billing_context'->'other_ratios'->>'size','-') FROM tasks WHERE task_id='$TID';"
Q "SELECT '客户侧扣费 quota', quota FROM tasks WHERE task_id='$TID';"
Q "SELECT '上游 costPoints', coalesce((data::jsonb->'result'->>'costPoints')::text,'-') FROM tasks WHERE task_id='$TID';"
Q "SELECT '产物', left(coalesce(private_data->>'result_url','-'),95) FROM tasks WHERE task_id='$TID';"
