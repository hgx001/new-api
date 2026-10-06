#!/usr/bin/env bash
# 用**正确入参形状**验证两件事，零成本（都在 ValidateRequestAndSetAction 拦下，不建单）：
#   ① r2v 只传 media[reference_video] → 应报「不支持视频参考」（验证 2026-10-06 的文案重排修复）
#   ② r2v 传 media[reference_video + reference_image] → 同样应报「不支持视频参考」
#   ③ wan3.0-smart 传 media[reference_video]：这条**会真建单**，默认不跑，需显式 ALLOW_PAID=1
# 严禁在这里放付费对照组 —— 2026-10-06 已因此连续误建 2 单（AGENTS.md 付费探针纪律第 3 条）。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')
B_T=$(Q "SELECT count(*) FROM tasks;"); B_Q=$(Q "SELECT quota FROM users WHERE id=2;")
VID=$(Q "SELECT private_data->>'result_url' FROM tasks WHERE status='SUCCESS'
        AND private_data->>'result_url' LIKE '%.mp4%' AND private_data->>'result_url' NOT LIKE '%heibaidao%'
        ORDER BY id DESC LIMIT 1;" | tr -d '\r\n ')
IMG="https://placehold.co/512x512.png"
echo "基线 tasks=$B_T quota=$B_Q"; echo "视频 ${VID:0:80}..."; echo

probe() { # $1=label $2=json
  printf '%-46s ' "$1"
  code=$(curl -s -o /tmp/p.json -w '%{http_code}' -X POST "$HOST/v1/videos" \
    -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' -d "$2")
  printf 'HTTP %s  %s\n' "$code" "$(head -c 160 /tmp/p.json)"
}

probe "① r2v  仅视频(media 形状)" \
  "{\"model\":\"wan2.7-r2v\",\"prompt\":\"x\",\"seconds\":\"5\",\"media\":[{\"type\":\"reference_video\",\"url\":\"$VID\"}]}"
probe "② r2v  视频+参考图" \
  "{\"model\":\"wan2.7-r2v\",\"prompt\":\"x\",\"seconds\":\"5\",\"media\":[{\"type\":\"reference_video\",\"url\":\"$VID\"},{\"type\":\"reference_image\",\"url\":\"$IMG\"}]}"
probe "②b r2v 仅视频(旧错误字段形状,对照)" \
  "{\"model\":\"wan2.7-r2v\",\"prompt\":\"x\",\"seconds\":\"5\",\"reference_video\":\"$VID\"}"

if [ "${ALLOW_PAID:-0}" = "1" ]; then
  echo; echo "!!! ALLOW_PAID=1 —— 下面这行会真建单并扣费"
  probe "③ wan3.0-smart 仅视频(付费)" \
    "{\"model\":\"wan3.0-smart\",\"prompt\":\"a person walking\",\"seconds\":\"5\",\"media\":[{\"type\":\"reference_video\",\"url\":\"$VID\"}]}"
else
  echo; echo "③ wan3.0-smart 视频参考：跳过（需 ALLOW_PAID=1）"
fi

echo; echo "=== 断言：①②应 400 且 tasks/quota 零变化 ==="
A_T=$(Q "SELECT count(*) FROM tasks;"); A_Q=$(Q "SELECT quota FROM users WHERE id=2;")
echo "tasks $B_T -> $A_T   quota $B_Q -> $A_Q"
[ "$B_T" = "$A_T" ] && [ "$B_Q" = "$A_Q" ] && echo "PASS 零扣费" || echo "FAIL 有扣费，需退款"
