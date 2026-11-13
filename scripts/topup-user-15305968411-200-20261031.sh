#!/usr/bin/env bash
# 给用户 15305968411 (id=36) 充值 ¥200 = 13698630 quota。
# 换算口径（与 scripts/topup-user-by-login.sh / topup-admin-1000-20261025.sh 一致）：
#   QuotaPerUnit=500000（common/constants.go）、USD2RMB=7.3（setting/ratio_setting/model_ratio.go）
#   200 / 7.3 * 500000 = 13698630.1369… → 13698630（一次性向下取整，不按 ¥1 先截断再乘）
# 走管理 API POST /api/user/manage + add_quota：IncreaseUserQuota 会同步 Redis
# 额度缓存并写审计日志；裸 SQL 会与额度缓存分叉（用户看到旧余额）。
#
# ⚠️ 管理 API 需要两个头：管理员 access_token（不是 API key）+ New-Api-User。
# ⚠️ 目标唯一性：'15305968411' 必须是唯一命中（username OR email），否则拒绝继续。
set -uo pipefail
Q() {
  local b64
  b64=$(printf '%s' "$1" | base64 -w0)
  ssh -p 877 -o ConnectTimeout=15 ubuntu@119.29.253.97 \
    "echo $b64 | base64 -d | docker exec -i postgres psql -U root -d new-api -t -A -F' | '" 2>/dev/null | tr -d '\r'
}
HOST=https://api.heibaidao.cn
TARGET='15305968411'
QUOTA=13698630       # ¥200
RATE=68493.15        # 每 ¥1 的 quota（仅用于展示换算）

ADMIN_ID=$(Q "SELECT u.id FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.role>=10 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')
ADMIN_AT=$(Q "SELECT access_token FROM users WHERE id=$ADMIN_ID AND access_token IS NOT NULL AND access_token<>'' ORDER BY id LIMIT 1;" | tr -d '\r\n ')
[ -n "$ADMIN_ID" ] && [ -n "$ADMIN_AT" ] || { echo "ABORT: 找不到管理员 access_token"; exit 1; }
echo "操作者: id=$ADMIN_ID  access_token 长度=${#ADMIN_AT}（不打印明文）"

echo
echo "=== 定位目标用户（username OR 邮箱 匹配 '$TARGET'）==="
Q "SELECT id, username, COALESCE(NULLIF(email,''),'(空)'), \"group\", status, quota FROM users WHERE username='$TARGET' OR email='$TARGET';"
N=$(Q "SELECT count(*) FROM users WHERE username='$TARGET' OR email='$TARGET';" | tr -d ' ')
[ "$N" = "1" ] || { echo "ABORT: 匹配到 $N 个用户，拒绝继续（避免充错人）"; exit 1; }
USER_ID=$(Q "SELECT id FROM users WHERE username='$TARGET' OR email='$TARGET';" | tr -d ' ')

BEFORE=$(Q "SELECT quota FROM users WHERE id=$USER_ID;" | tr -d ' ')
echo "user_id=$USER_ID  充值前余额=$BEFORE ≈ ¥$(awk "BEGIN{printf \"%.6f\", $BEFORE/$RATE}")"

echo
echo "=== 执行充值 +$QUOTA quota（¥200）==="
curl -s -o /tmp/topup-15305968411.json -w 'HTTP=%{http_code}\n' -X POST "$HOST/api/user/manage" \
  -H "Authorization: Bearer $ADMIN_AT" -H "New-Api-User: $ADMIN_ID" \
  -H 'Content-Type: application/json' \
  -d "{\"id\":$USER_ID,\"action\":\"add_quota\",\"mode\":\"add\",\"value\":$QUOTA}"
head -c 300 /tmp/topup-15305968411.json; echo

echo
echo "=== 充值后 ==="
AFTER=$(Q "SELECT quota FROM users WHERE id=$USER_ID;" | tr -d ' ')
echo "余额=$AFTER ≈ ¥$(awk "BEGIN{printf \"%.6f\", $AFTER/$RATE}")"
D=$((AFTER-BEFORE))
echo "差额=$D（预期 $QUOTA）"
[ "$D" = "$QUOTA" ] && echo "PASS 差额与预期一致" || { echo "FAIL 差额=$D ≠ 预期 $QUOTA"; exit 1; }

echo
echo "=== 审计日志（manage 留痕，最近 3 条）==="
Q "SELECT id, user_id, type, quota, left(COALESCE(content,''),80), to_timestamp(created_at)
   FROM logs WHERE user_id=$USER_ID ORDER BY id DESC LIMIT 3;"
