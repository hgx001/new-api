#!/usr/bin/env bash
# 给用户 15305968411 (id=36) 充值 ¥2 = 136986 quota。
# 走管理 API POST /api/user/manage + add_quota（IncreaseUserQuota 会同步 Redis
# 并记审计日志），不用裸 SQL——裸 SQL 会与令牌额度缓存分叉。
#
# ⚠️ 管理 API 需要两个头：管理员的 access_token（不是 API key）+ New-Api-User。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
USER_ID=36
QUOTA=136986          # ¥2 × 68493（与 QuotaForNewUser 同一换算口径）

ADMIN_ID=$(Q "SELECT u.id FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.role>=10 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')
ADMIN_AT=$(Q "SELECT access_token FROM users WHERE id=$ADMIN_ID AND access_token IS NOT NULL AND access_token<>'' ORDER BY id LIMIT 1;" | tr -d '\r\n ')
[ -n "$ADMIN_ID" ] && [ -n "$ADMIN_AT" ] || { echo "ABORT: 找不到管理员 access_token（admin_id=$ADMIN_ID）"; exit 1; }
echo "管理员: id=$ADMIN_ID  access_token 长度=${#ADMIN_AT}（不打印明文）"

echo
echo "=== 充值前 ==="
Q "SELECT 'user', id, username, \"group\", status, quota, to_timestamp(created_at) FROM users WHERE id=$USER_ID;"
BEFORE=$(Q "SELECT quota FROM users WHERE id=$USER_ID;" | tr -d ' ')
echo "余额=$BEFORE ≈ ¥$(awk "BEGIN{printf \"%.6f\", $BEFORE/68493}")"

echo
echo "=== 执行充值 +$QUOTA quota ==="
curl -s -o /tmp/topup.json -w 'HTTP=%{http_code}\n' -X POST "$HOST/api/user/manage" \
  -H "Authorization: Bearer $ADMIN_AT" -H "New-Api-User: $ADMIN_ID" \
  -H 'Content-Type: application/json' \
  -d "{\"id\":$USER_ID,\"action\":\"add_quota\",\"mode\":\"add\",\"value\":$QUOTA}"
head -c 200 /tmp/topup.json; echo

echo
echo "=== 充值后 ==="
Q "SELECT 'user', id, quota, status FROM users WHERE id=$USER_ID;"
AFTER=$(Q "SELECT quota FROM users WHERE id=$USER_ID;" | tr -d ' ')
echo "余额=$AFTER ≈ ¥$(awk "BEGIN{printf \"%.6f\", $AFTER/68493}")"
echo "差额=$((AFTER-BEFORE))（预期 $QUOTA）"

echo
echo "=== 审计日志（manage 留痕）==="
Q "SELECT id, user_id, type, quota, left(COALESCE(content,''),90), to_timestamp(created_at)
   FROM logs WHERE user_id=$USER_ID ORDER BY id DESC LIMIT 4;"
