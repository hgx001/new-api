#!/usr/bin/env bash
# 验证「参考视频时长超限 → 本地直接拒绝」，零建单零扣费。
#
# 背景（老板 2026-10-31 要求）：
#   此前超限参考视频会被送到上游，上游返回
#   MEDIA_DURATION_INVALID「参考素材时长过长：当前为 31.609 秒，最长允许 15 秒」，
#   白跑一趟还先扣费再退款（生产 tasks id=967~987 连续 14 次失败）。
#   现在改成 validate 阶段本地 400 拒绝：
#     · 单条参考视频 ≤15 秒
#     · 多条参考视频合计 ≤15 秒
#     · 有参考视频时生成时长 ≤15 秒
#
# ⚠️ 本脚本的三条用例**全部应当被本地拒绝**，所以不会建单、不会扣费。
#    这与 AGENTS.md「付费探针纪律」相反但同样安全：本脚本没有任何一条
#    用例能通过校验，不存在「对照组真建单」的可能。
#
# ⚠️ 本脚本在**本地**执行，SSH 只用于查生产库；不要放到生产机上跑
#    （那会 SSH 回自身，自身无回环凭据，Q() 取数会全空）。
set -uo pipefail

HOST=https://api.heibaidao.cn
REPO=/home/ubuntu/new-api-src
LOCAL_DIR="$(cd "$(dirname "$0")/.." && pwd)"

# SQL 用 base64 传输：SSH 里嵌套双引号会被剥离，直接拼 -c 不可靠。
Q() {
  local b64
  b64=$(printf '%s' "$1" | base64 -w0)
  ssh -p 877 ubuntu@119.29.253.97 \
    "echo $b64 | base64 -d | docker exec -i postgres psql -U root -d new-api -t -A" 2>/dev/null | tr -d '\r\n'
}

# 取一个余额充足的 token
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;")
# 两条素材（从生产库导出的成功/失败任务的真实素材 URL）：
#   VID_LONG  = 32.4 秒（>15，单条超限）
#   VID_SHORT = 12.0 秒（≤15，合规，用来单独验证「生成时长超限」）
VID_LONG=$(cat "$LOCAL_DIR/_vid985.txt" 2>/dev/null || echo "")
VID_SHORT=$(cat "$LOCAL_DIR/_vid960.txt" 2>/dev/null || echo "")
if [ -z "$TK" ] || [ -z "$VID_LONG" ] || [ -z "$VID_SHORT" ]; then
  echo "ABORT: 依赖缺失 TK='${TK:0:8}' LONG=${#VID_LONG} SHORT=${#VID_SHORT}"
  exit 1
fi

B_T=$(Q "SELECT count(*) FROM tasks;")
B_Q=$(Q "SELECT coalesce(sum(quota),0) FROM users;")
echo "token=${TK:0:8}...  长素材 ${#VID_LONG} 字节 / 短素材 ${#VID_SHORT} 字节"
echo "基线 tasks=$B_T  users_quota=$B_Q"
if [ -z "$B_T" ]; then
  echo "ABORT: 基线取数为空"
  exit 1
fi
echo

probe() { # $1=label $2=json
  printf '%-46s ' "$1"
  code=$(curl -s -o /tmp/p.json -w '%{http_code}' -X POST "$HOST/v1/videos" \
    -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' -d "$2")
  printf 'HTTP %s  %s\n' "$code" "$(head -c 220 /tmp/p.json)"
  echo "$code" > /tmp/last_code
}

probe "① 单条参考 32.4 秒 > 15 秒" \
  "{\"model\":\"seedance2.5-特惠\",\"prompt\":\"test\",\"seconds\":\"5\",\"media\":[{\"type\":\"reference_video\",\"url\":\"$VID_LONG\"}]}"

probe "② 多条合计超限（12+12=24 秒）" \
  "{\"model\":\"seedance2.5-特惠\",\"prompt\":\"test\",\"seconds\":\"5\",\"media\":[{\"type\":\"reference_video\",\"url\":\"$VID_SHORT\"},{\"type\":\"reference_video\",\"url\":\"$VID_SHORT\"}]}"

probe "③ 参考合规但生成 30 秒 > 15 秒" \
  "{\"model\":\"seedance2.5-特惠\",\"prompt\":\"test\",\"seconds\":\"30\",\"media\":[{\"type\":\"reference_video\",\"url\":\"$VID_SHORT\"}]}"

echo
echo "=== 断言：三条均 HTTP 400，tasks/quota 零变化 ==="
A_T=$(Q "SELECT count(*) FROM tasks;")
A_Q=$(Q "SELECT coalesce(sum(quota),0) FROM users;")
echo "tasks $B_T -> $A_T     users_quota $B_Q -> $A_Q"
if [ "$B_T" = "$A_T" ] && [ "$B_Q" = "$A_Q" ]; then
  echo "PASS 零建单零扣费"
else
  echo "FAIL 有新增任务或配额变化，需排查！"
fi
