#!/usr/bin/env bash
# 建/更新 MegaAI（type=64）渠道并挂上 T3-seedance-2.5（2026-11-23 重新接入）。
#
# ⚠️ 为什么走管理 API 而不是裸 SQL：渠道新增/编辑会触发 UpdateAbilities，
#    abilities 是路由的第二半（channels.models 改完不写 abilities = 客户调不通）。
#    裸 SQL 建渠道还得手工补 abilities 行并等 60s 缓存重建。
#
# ⚠️ API Key 不写进脚本、不留盘：从 stdin 读（调用方式见下），用完立即从变量清掉。
#    用法（在服务器上，key 从本地管道传入，不落 shell history）：
#        cat /path/to/key | bash scripts/create-megabyai-t3-channel.sh
#
# 幂等：已存在同一 base_url + type=64 的渠道 → 改为 PUT 更新（models 合并）。
set -uo pipefail

HOST="${MEGABYAI_HOST:-http://127.0.0.1:3000}"
BASE_URL="${MEGABYAI_BASE_URL:-https://newapi.megabyai.cc}"
MODEL="T3-seedance-2.5"
NAME="${MEGABYAI_CHANNEL_NAME:-MegaAI}"

Q() { printf '%s' "$1" | sudo docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }

KEY=$(tr -d '\r\n ' )
[ -n "$KEY" ] || { echo "缺少 API Key（从 stdin 传入）"; exit 1; }

# 管理员令牌直接从库里取（不打印）
AT=$(Q "SELECT access_token FROM users WHERE role >= 100 AND access_token <> '' ORDER BY id LIMIT 1;" | tr -d '\r\n ')
AUID=$(Q "SELECT id FROM users WHERE role >= 100 AND access_token <> '' ORDER BY id LIMIT 1;" | tr -d '\r\n ')
[ -n "$AT" ] || { echo "找不到管理员 access_token"; exit 1; }

EXIST=$(Q "SELECT id FROM channels WHERE base_url='$BASE_URL' AND type=64;" | tr -d '\r\n ')

if [ -n "$EXIST" ]; then
  # 更新：载荷是渠道对象本身（不带 {mode,channel} 包装），且**不能带 status**
  # （controller.UpdateChannel 会把带 status 的更新判为 invalid params）。
  # models 用并集合并，绝不整体覆盖 —— 覆盖会把该渠道其它在售模型摘掉。
  CURRENT=$(Q "SELECT models FROM channels WHERE id=$EXIST;" | tr -d '\r\n')
  MERGED=$(python3 - "$CURRENT" "$MODEL" <<'PY'
import sys
cur = [m.strip() for m in (sys.argv[1] or '').split(',') if m.strip()]
add = sys.argv[2].strip()
out = []
for m in cur + [add]:
    if m and m not in out:
        out.append(m)
print(','.join(out))
PY
)
  PAYLOAD=$(printf '{"id":%s,"type":64,"key":"%s","name":"%s","base_url":"%s","models":"%s","group":"default","priority":0,"weight":0}' \
    "$EXIST" "$KEY" "$NAME" "$BASE_URL" "$MERGED")
  METHOD=PUT
  echo "已存在渠道 $EXIST，改为更新（models: $MERGED）"
else
  PAYLOAD=$(printf '{"mode":"single","channel":{"type":64,"key":"%s","name":"%s","base_url":"%s","models":"%s","group":"default","status":1,"priority":0,"weight":0}}' \
    "$KEY" "$NAME" "$BASE_URL" "$MODEL")
  METHOD=POST
  echo "新建渠道（models: $MODEL）"
fi

RESP=$(curl -s -X "$METHOD" "$HOST/api/channel/" \
  -H "Authorization: Bearer $AT" -H "New-Api-User: $AUID" -H 'Content-Type: application/json' \
  -d "$PAYLOAD")
unset KEY PAYLOAD AT
echo "API: $(printf '%s' "$RESP" | head -c 300)"

echo
echo "=== 复核 ==="
Q "SELECT 'id='||id, 'type='||type, 'status='||status, 'models='||coalesce(models,'(空)'),
        'base_url='||coalesce(base_url,'(空)'), 'group='||coalesce(\"group\",'(空)')
   FROM channels WHERE base_url='$BASE_URL' AND type=64;"
Q "SELECT 'ability: group='||\"group\"||' model='||model||' enabled='||enabled||' prio='||priority
   FROM abilities WHERE channel_id=(SELECT id FROM channels WHERE base_url='$BASE_URL' AND type=64 LIMIT 1);"
