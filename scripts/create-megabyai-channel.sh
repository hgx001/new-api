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
# ⚠️ 密钥里混进 BOM / 空白会导致上游 401「无效的令牌」，并**连锁触发渠道 AutoBan**
#    （2026-10-22 实测：PowerShell 管道写文件会带 UTF-8 BOM，渠道被自动禁用后
#    表现为「分组 default 下模型 wan-3.0 的可用渠道不存在」）。
#    这里直接拒绝含不可见字符的密钥，别让它进渠道。
if printf '%s' "$KEY" | LC_ALL=C grep -q '[^ -~]'; then
  echo "密钥含非 ASCII 可见字符（多半是 BOM/全角字符），拒绝写入渠道；请用无 BOM 的 UTF-8 文件"
  exit 1
fi
case "$KEY" in
  sk-*) ;;
  *) echo "密钥不像 new-api 令牌（应以 sk- 开头），请确认" ; exit 1 ;;
esac

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
  # ⚠️ 更新走 PUT /api/channel/：载荷就是渠道对象本身（不是 {mode, channel} 包装），
  #    且**不能带 status 字段**（controller.UpdateChannel 会直接报 invalid params）。
  #    渠道若被 AutoBan 置为 disabled，更新后还要用 scripts/restore-channel.sh 恢复
  #    （status 与 abilities.enabled 必须一起改，见 AGENTS.md）。
  PAYLOAD=$(cat <<JSON
{
  "id": $EXIST,
  "type": 64,
  "key": "$KEY",
  "name": "MegaAI-Wan3",
  "base_url": "$BASE_URL",
  "models": "$MODEL",
  "group": "default",
  "priority": 0,
  "weight": 0
}
JSON
)
  METHOD=PUT
else
  PAYLOAD=$(cat <<JSON
{
  "mode": "single",
  "channel": {
    "type": 64,
    "key": "$KEY",
    "name": "MegaAI-Wan3",
    "base_url": "$BASE_URL",
    "models": "$MODEL",
    "group": "default",
    "status": 1,
    "priority": 0,
    "weight": 0
  }
}
JSON
)
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
