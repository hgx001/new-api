#!/usr/bin/env bash
# 用管理 API 建 MegaAI 渠道（不走裸 SQL：能力走内存缓存，裸 SQL 改 abilities 对
# 新建渠道不生效，见 AGENTS.md）。
#
# 幂等：脚本可重复执行，先按 base_url 找同名渠道，已存在则改为更新而非重建。
#
# ⚠️ API Key 不写进脚本（密钥不入库）：从 /tmp/megabyai.key 读（mode 600）。
#    写入方式（在服务器上）：
#      umask 077; cat > /tmp/megabyai.key   # 然后粘贴 key，Ctrl-D
set -uo pipefail
HOST="${MEGABYAI_HOST:-https://api.heibaidao.cn}"
BASE_URL="${MEGABYAI_BASE_URL:-https://newapi.megabyai.cc}"
MODEL="${MEGABYAI_MODEL:-wan-3.0}"
KEYFILE="${MEGABYAI_KEYFILE:-/tmp/megabyai.key}"
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }

[ -s "$KEYFILE" ] || { echo "缺少密钥文件 $KEYFILE"; exit 1; }
KEY=$(tr -d '\r\n ' < "$KEYFILE")
[ -n "$KEY" ] || { echo "密钥文件为空"; exit 1; }

# 管理 API 必须用管理员的 access_token + New-Api-User，不能用调用用的 API key。
AT=$(Q "SELECT access_token FROM users WHERE role>=10 AND status=1 ORDER BY role DESC LIMIT 1;" | tr -d '\r\n ')
[ -z "$AT" ] && { echo "取不到管理员 access_token"; exit 1; }
# New-Api-User 必须与 access_token 所属用户一致，否则报 Unauthorized
AUID=$(Q "SELECT id FROM users WHERE role>=10 AND status=1 ORDER BY role DESC LIMIT 1;" | tr -d '\r\n ')

echo "建渠道：type=64 base_url=$BASE_URL models=$MODEL"

PAYLOAD=$(cat <<JSON
{
  "mode": "single",
  "channel": {
    "type": 64,
    "key": "$KEY",
    "name": "MegaAI-Wan3",
    "base_url": "$BASE_URL",
    "models": "$MODEL",
    "groups": "default",
    "status": 1,
    "priority": 0,
    "weight": 0
  }
}
JSON
)

EXIST=$(Q "SELECT id FROM channels WHERE base_url='$BASE_URL' AND type=64;" | tr -d '\r\n ')
if [ -n "$EXIST" ]; then
  echo "已存在渠道 $EXIST，改为更新"
  PAYLOAD=$(printf '%s' "$PAYLOAD" | sed "s/\"mode\": \"single\"/\"mode\": \"update\"/")
  METHOD=PUT
else
  METHOD=POST
fi

RESP=$(curl -s -X "$METHOD" "$HOST/api/channel/" \
  -H "Authorization: Bearer $AT" -H "New-Api-User: $AUID" -H 'Content-Type: application/json' \
  -d "$PAYLOAD")
echo "API: $(printf '%s' "$RESP" | head -c 300)"

echo
echo "=== 复核 ==="
Q "SELECT 'id='||id, 'type='||type, 'status='||status, 'models='||coalesce(models,'(空)'),
        'base_url='||coalesce(base_url,'(空)'), 'group='||coalesce(\"group\",'(空)')
   FROM channels WHERE base_url='$BASE_URL' AND type=64;"
Q "SELECT 'ability: group='||\"group\"||' model='||model||' enabled='||enabled||' prio='||priority
   FROM abilities WHERE channel_id=(SELECT id FROM channels WHERE base_url='$BASE_URL' AND type=64 LIMIT 1);"
