#!/usr/bin/env bash
# 给用户充值 ¥20 = 1369863 quota。
#   换算：QuotaPerUnit=500000（common/constants.go），USD2RMB=7.3（ratio_setting/model_ratio.go）
#         20 / 7.3 * 500000 = 1369863.0137 → 1369863（截断）
# 走管理 API POST /api/user/manage + add_quota：IncreaseUserQuota 会同步 Redis
# 并写审计日志；裸 SQL 会与令牌额度缓存分叉。
#
# ⚠️ 目标用户定位：本次 90666757@qq.com 是**用户名**而非邮箱，email 字段为空。
#    所以用户名和邮箱两个字段都要匹配，否则会「查不到人」或将来撞同名用户充错人。
# ⚠️ 管理 API 需要两个头：管理员 access_token（不是 API key）+ New-Api-User。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TARGET='90666757@qq.com'
QUOTA=1369863        # ¥20
RATE=68493.15        # 每 ¥1 的 quota

ADMIN_ID=$(Q "SELECT u.id FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.role>=10 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')
ADMIN_AT=$(Q "SELECT access_token FROM users WHERE id=$ADMIN_ID AND access_token IS NOT NULL AND access_token<>'' ORDER BY id LIMIT 1;" | tr -d '\r\n ')
[ -n "$ADMIN_ID" ] && [ -n "$ADMIN_AT" ] || { echo "ABORT: 找不到管理员 access_token"; exit 1; }
echo "管理员: id=$ADMIN_ID  access_token 长度=${#ADMIN_AT}（不打印明文）"

echo
echo "=== 定位目标用户（用户名 OR 邮箱 匹配）==="
Q "SELECT id, username, COALESCE(NULLIF(email,''),'(空)'), quota, status FROM users WHERE username='$TARGET' OR email='$TARGET';"
N=$(Q "SELECT count(*) FROM users WHERE username='$TARGET' OR email='$TARGET';" | tr -d ' ')
[ "$N" = "1" ] || { echo "ABORT: 匹配到 $N 个用户，拒绝继续（避免充错人）"; exit 1; }
USER_ID=$(Q "SELECT id FROM users WHERE username='$TARGET' OR email='$TARGET';" | tr -d ' ')

BEFORE=$(Q "SELECT quota FROM users WHERE id=$USER_ID;" | tr -d ' ')
echo "user_id=$USER_ID  充值前余额=$BEFORE ≈ ¥$(awk "BEGIN{printf \"%.6f\", $BEFORE/$RATE}")"

echo
echo "=== 执行充值 +$QUOTA quota（¥20）==="
curl -s -o /tmp/topup.json -w 'HTTP=%{http_code}\n' -X POST "$HOST/api/user/manage" \
  -H "Authorization: Bearer $ADMIN_AT" -H "New-Api-User: $ADMIN_ID" \
  -H 'Content-Type: application/json' \
  -d "{\"id\":$USER_ID,\"action\":\"add_quota\",\"mode\":\"add\",\"value\":$QUOTA}"
head -c 200 /tmp/topup.json; echo

echo
echo "=== 充值后 ==="
AFTER=$(Q "SELECT quota FROM users WHERE id=$USER_ID;" | tr -d ' ')
echo "余额=$AFTER ≈ ¥$(awk "BEGIN{printf \"%.6f\", $AFTER/$RATE}")"
D=$((AFTER-BEFORE))
echo "差额=$D"
[ "$D" = "$QUOTA" ] && echo "PASS 差额与预期一致" || { echo "FAIL 差额=$D ≠ 预期 $QUOTA"; exit 1; }

echo
echo "=== 审计日志（manage 留痕，最近 3 条）==="
Q "SELECT id, user_id, type, quota, left(COALESCE(content,''),80), to_timestamp(created_at)
   FROM logs WHERE user_id=$USER_ID ORDER BY id DESC LIMIT 3;"