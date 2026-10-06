#!/usr/bin/env bash
# wan2.7-r2v 是否支持「视频参考」——零成本验证。
# 该校验在 ValidateRequestAndSetAction 里（计费预扣之前），所以带视频参考的请求
# 必然被本地 400 拦下，不会真的建单、不消耗上游点数。
# 参考素材取历史 SUCCESS 任务的产物 URL，不新建单。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')

BEFORE_TASKS=$(Q "SELECT count(*) FROM tasks;")
BEFORE_QUOTA=$(Q "SELECT quota FROM users WHERE id=2;")
echo "基线：tasks=$BEFORE_TASKS  user2_quota=$BEFORE_QUOTA"

# 历史 SUCCESS 任务的公网 mp4（有赞 CDN 直链，不带渠道密钥）
VID=$(Q "SELECT private_data->>'result_url' FROM tasks
        WHERE status='SUCCESS' AND private_data->>'result_url' LIKE '%.mp4%'
          AND private_data->>'result_url' NOT LIKE '%heibaidao%'
        ORDER BY id DESC LIMIT 1;" | tr -d '\r\n ')
echo "参考视频: ${VID:0:110}..."
echo

# ① r2v + 视频参考 → 期望 400 本地拒绝
echo "=== ① wan2.7-r2v + reference_video（5 秒）==="
curl -s -o /tmp/r1.json -w 'HTTP %{http_code}\n' -X POST "$HOST/v1/videos" \
  -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d "$(python3 -c "
import json,sys
print(json.dumps({'model':'wan2.7-r2v','prompt':'a person walking','seconds':'5',
 'reference_video':'$VID'}))")" 2>/dev/null || \
curl -s -o /tmp/r1.json -w 'HTTP %{http_code}\n' -X POST "$HOST/v1/videos" \
  -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d "{\"model\":\"wan2.7-r2v\",\"prompt\":\"a person walking\",\"seconds\":\"5\",\"reference_video\":\"$VID\"}"
head -c 300 /tmp/r1.json; echo; echo

# ② 对照：wan3.0-smart + 同一个视频参考 → 期望通过校验（不建单，只看是否被本地拒）
echo "=== ② 对照 wan3.0-smart + 同一个 reference_video（5 秒）==="
curl -s -o /tmp/r2.json -w 'HTTP %{http_code}\n' -X POST "$HOST/v1/videos" \
  -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d "{\"model\":\"wan3.0-smart\",\"prompt\":\"a person walking\",\"seconds\":\"5\",\"reference_video\":\"$VID\"}"
head -c 300 /tmp/r2.json; echo; echo

echo "=== 断言：①必须 400 且零扣费；②若返回 200/task_id 说明它会真建单，需立刻撤销 ==="
AFTER_TASKS=$(Q "SELECT count(*) FROM tasks;")
AFTER_QUOTA=$(Q "SELECT quota FROM users WHERE id=2;")
echo "tasks: $BEFORE_TASKS -> $AFTER_TASKS"
echo "user2_quota: $BEFORE_QUOTA -> $AFTER_QUOTA"
