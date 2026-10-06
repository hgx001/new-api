#!/usr/bin/env bash
# 用管理 API 建 erchun 渠道（不走裸 SQL：能力走内存缓存，裸 SQL 改 abilities 对
# 新建渠道不生效，见 AGENTS.md）。
#
# 幂等：脚本可重复执行，先按 base_url 找同名渠道，已存在则改为更新而非重建。
set -uo pipefail
HOST=https://api.heibaidao.cn
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }

# 管理 API 必须用管理员的 access_token + New-Api-User，不能用调用用的 API key。
AT=$(Q "SELECT access_token FROM users WHERE role>=10 AND status=1 ORDER BY role DESC LIMIT 1;" | tr -d '\r\n ')
[ -z "$AT" ] && { echo "取不到管理员 access_token"; exit 1; }
# New-Api-User 必须与 access_token 所属用户一致，否则报 Unauthorized
AUID=$(Q "SELECT id FROM users WHERE role>=10 AND status=1 ORDER BY role DESC LIMIT 1;" | tr -d '\r\n ')
KEY=$(Q "SELECT key FROM channels WHERE id=19;" | tr -d '\r\n ')
echo "复用渠道 19 的密钥（长度 ${#KEY}）"

MODEL='wan3.0-480p'
PAYLOAD=$(cat <<JSON
{
  "mode": "single",
  "channel": {
    "type": 65,
    "key": "$KEY",
    "name": "二春Erchun-Wan3-480p",
    "base_url": "https://api.erchun.youkou.cc",
    "models": "$MODEL",
    "groups": "default",
    "status": 1,
    "priority": 10,
    "weight": 0,
    "model_mapping": "{\"$MODEL\":\"mdl_ec286eb1bc618249d65b87bc79366417\"}",
    "other": "{\"key_mode\":\"single\"}"
  }
}
JSON
)

EXIST=$(Q "SELECT id FROM channels WHERE base_url='https://api.erchun.youkou.cc' AND type=65;" | tr -d '\r\n ')
if [ -n "$EXIST" ]; then
  echo "已存在渠道 $EXIST，改为更新"
  PAYLOAD=$(printf '%s' "$PAYLOAD" | sed "s/\"mode\": \"single\"/\"mode\": \"update\"/")
fi

if [ -n "$EXIST" ]; then METHOD=PUT; else METHOD=POST; fi
RESP=$(curl -s -X "$METHOD" "$HOST/api/channel/" \
  -H "Authorization: Bearer $AT" -H "New-Api-User: $AUID" -H 'Content-Type: application/json' \
  -d "$PAYLOAD")
echo "API: $(echo "$RESP" | head -c 200)"

echo
echo "=== 复核 ==="
Q "SELECT 'id='||id, 'type='||type, 'status='||status, 'models='||coalesce(models,'(空)'),
          'mapping='||coalesce(model_mapping,'(空)')
   FROM channels WHERE base_url='https://api.erchun.youkou.cc' AND type=65;"
Q "SELECT 'ability: group='||\"group\"||' model='||model||' enabled='||enabled||' prio='||priority
   FROM abilities WHERE channel_id=(SELECT id FROM channels WHERE base_url='https://api.erchun.youkou.cc' AND type=65 LIMIT 1);"
