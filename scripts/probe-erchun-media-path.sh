#!/usr/bin/env bash
# 零成本决定性探针：参考视频 URL 指向必然下载失败的地址。
#   若 adaptor 真的在读 media[] → BuildRequestBody 里先下载并失败 → 400，不建单、不扣费
#   若 media 被静默忽略 → 会当纯文生视频建单成功（那就证明我之前的说法是错的）
# 断言：必须 400 且 tasks/quota 零变化。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A | tr -d '\r\n '; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;")
B_T=$(Q "SELECT count(*) FROM tasks;"); B_Q=$(Q "SELECT quota FROM users WHERE id=2;")

echo "=== 参考视频 URL 指向 404（不存在域），期望 400 且不建单 ==="
code=$(curl -s -o /tmp/vr.json -w '%{http_code}' -X POST "$HOST/v1/videos" \
  -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d '{"model":"wan3.0-480p","prompt":"probe: does the media path run at all","seconds":"2","resolution":"480p","media":[{"type":"reference_video","url":"https://nonexistent-host-for-probe.invalid/a.mp4"}]}')
echo "HTTP $code"
head -c 300 /tmp/vr.json; echo

echo
echo "=== 对照：同样的 404 URL 放在 images 字段（已知会走 reference_image）==="
code2=$(curl -s -o /tmp/vr2.json -w '%{http_code}' -X POST "$HOST/v1/videos" \
  -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d '{"model":"wan3.0-480p","prompt":"probe images path","seconds":"2","resolution":"480p","images":["https://nonexistent-host-for-probe.invalid/a.png"]}')
echo "HTTP $code2"; head -c 200 /tmp/vr2.json; echo

echo
echo "=== 断言：两者都应 400，tasks/quota 零变化 ==="
A_T=$(Q "SELECT count(*) FROM tasks;"); A_Q=$(Q "SELECT quota FROM users WHERE id=2;")
echo "tasks $B_T -> $A_T    quota $B_Q -> $A_Q"
[ "$B_T" = "$A_T" ] && [ "$B_Q" = "$A_Q" ] && echo "PASS 零扣费" || echo "FAIL 有建单或扣费"
