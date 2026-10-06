#!/usr/bin/env bash
# 唯一一条授权的真实任务：wan3.0-480p / 480P / 2 秒 / 带视频参考。
# 用户明确授权「跑一条 480p 2 秒的视频试试，要带视频参考，只跑一条」。
# 预计扣费：base=int(0.038356164383561646*500000)=19178 × 2 秒 = 38356 quota = ¥0.56
#
# 退款账号一律从任务记录读 user_id / token_id，绝不写死 id
# （2026-10-06 task 314 教训：写死 id 把钱退给了错的账号）。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')
# 已验证：968KB / ftypisom / 10.00s，满足 catalog 限制（mp4、≤15s、≤100MB）
REF="https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4"
B_Q=$(Q "SELECT quota FROM users WHERE id=2;")

echo "=== 建单 ==="
RESP=$(curl -s -X POST "$HOST/v1/videos" -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d "{\"model\":\"wan3.0-480p\",\"prompt\":\"the rabbit continues running through the meadow, natural daylight, smooth camera follow\",\"seconds\":\"2\",\"resolution\":\"480p\",\"aspect_ratio\":\"16:9\",\"media\":[{\"type\":\"reference_video\",\"url\":\"$REF\"}]}")
echo "$RESP" | head -c 320; echo
TID=$(echo "$RESP" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\(task_[^"]*\)".*/\1/p' | head -1)
[ -n "$TID" ] || { echo "建单失败，未扣费"; exit 1; }
echo "task_id=$TID   预期扣费 38356 quota = ¥0.56"

echo; echo "=== 轮询（最多 8 分钟）==="
for i in $(seq 1 16); do
  sleep 30
  ROW=$(Q "SELECT status||'|'||coalesce(progress,'')||'|'||left(coalesce(fail_reason,''),46) FROM tasks WHERE task_id='$TID';")
  echo "[$(date +%H:%M:%S)] $ROW"
  ST=$(echo "$ROW" | cut -d'|' -f1)
  [ "$ST" = "SUCCESS" ] || [ "$ST" = "FAILURE" ] && break
done

echo; echo "=== 终态 ==="
Q "SELECT 'status', status FROM tasks WHERE task_id='$TID';"
Q "SELECT 'fail_reason', coalesce(fail_reason,'(none)') FROM tasks WHERE task_id='$TID';"
Q "SELECT '计费 seconds/size', coalesce(private_data->'billing_context'->'other_ratios'->>'seconds','-')||' / '||coalesce(private_data->'billing_context'->'other_ratios'->>'size','-') FROM tasks WHERE task_id='$TID';"
Q "SELECT '扣费 quota', quota FROM tasks WHERE task_id='$TID';"
Q "SELECT '产物(站内代理)', left(coalesce(private_data->>'result_url','-'),80) FROM tasks WHERE task_id='$TID';"
Q "SELECT 'user2 quota 变化', $B_Q||' -> '||(SELECT quota FROM users WHERE id=2);"
