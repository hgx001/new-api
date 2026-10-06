#!/usr/bin/env bash
# erchun 渠道零成本烟测。全部用**必然被本地校验拦下**的入参：
# 命中即证明「路由 → 适配器 → 校验」整条链路通了，且不建单、不耗上游额度。
# （若渠道/能力没接好，这些请求会 503 model_not_found 或落到别的渠道，就能区分出来。）
set -uo pipefail
HOST=https://api.heibaidao.cn
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A | tr -d '\r\n '; }
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;")
B_T=$(Q "SELECT count(*) FROM tasks;"); B_Q=$(Q "SELECT quota FROM users WHERE id=2;")

probe() { printf '%-40s ' "$1"
  code=$(curl -s -o /tmp/e.json -w '%{http_code}' -X POST "$HOST/v1/videos" \
    -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' -d "$2")
  printf 'HTTP %s  %s\n' "$code" "$(head -c 150 /tmp/e.json)"; }

probe "720P（应被拒：只开放 480P）" \
  '{"model":"wan3.0-480p","prompt":"x","seconds":"2","resolution":"720p"}'
probe "1080P（应被拒）" \
  '{"model":"wan3.0-480p","prompt":"x","seconds":"2","resolution":"1080p"}'
probe "非法比例 21:9（应被拒）" \
  '{"model":"wan3.0-480p","prompt":"x","seconds":"2","resolution":"480p","metadata":{"ratio":"21:9"}}'
probe "未知素材类型（应被拒）" \
  '{"model":"wan3.0-480p","prompt":"x","seconds":"2","resolution":"480p","media":[{"type":"hologram","url":"https://cdn.example/a.png"}]}'

echo
echo "=== 断言：以上必须全是 400，且 tasks/quota 零变化 ==="
A_T=$(Q "SELECT count(*) FROM tasks;"); A_Q=$(Q "SELECT quota FROM users WHERE id=2;")
echo "tasks $B_T -> $A_T    quota $B_Q -> $A_Q"
[ "$B_T" = "$A_T" ] && [ "$B_Q" = "$A_Q" ] && echo "PASS 零扣费" || echo "FAIL 有扣费，需排查"
