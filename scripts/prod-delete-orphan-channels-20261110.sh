#!/usr/bin/env bash
# 生产：删除 5 条「零上广场模型」的渠道（2026-11-10）
#
# 老板指令：目前没有在模型广场展示的模型渠道全部删除。
# 这 5 条渠道的能力行为空（channels.models 为空），能力表里也没有任何 ability 行，
# 删掉不影响任何在售模型（广场卡片与在售能力行已逐条对账，见复核段）。
#
# 走管理 API DELETE /api/channel/{id}，**不裸 SQL 删 channels**：
#   · channel.Delete() 会连带删掉 abilities 行
#   · controller.DeleteChannel 会 model.InitChannelCache() 刷新内存路由缓存
#   · 裸 SQL 删 channels 不会刷新缓存，路由表里会残留已删渠道
#
# 幂等：脚本可重复执行，已删的渠道跳过。
set -uo pipefail
# ⚠️ 走服务器本地回环：外网域名走 nginx 会对 DELETE 方法做额外限制（返回
#    405 Method Not Allowed），本地端口是 new-api 容器的直连入口。
HOST="${NEWAPI_HOST:-http://127.0.0.1:3000}"
TARGET_IDS=(11 19 21 22 23)

Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }

# 管理 API 必须用管理员的 access_token + New-Api-User（不能用调用用的 API key）
AT=$(Q "SELECT access_token FROM users WHERE role>=10 AND status=1 ORDER BY role DESC LIMIT 1;" | tr -d '\r\n ')
[ -z "$AT" ] && { echo "取不到管理员 access_token"; exit 1; }
AUID=$(Q "SELECT id FROM users WHERE role>=10 AND status=1 ORDER BY role DESC LIMIT 1;" | tr -d '\r\n ')
[ -z "$AUID" ] && { echo "取不到管理员用户 id"; exit 1; }

echo "=== ① 删除前快照（备份进 _bak 表）==="
run_sql_bak() {
  docker exec -i postgres psql -U root -d new-api -v ON_ERROR_STOP=1 -f - 2>&1
}
run_sql_bak <<'SQL'
BEGIN;
CREATE TABLE IF NOT EXISTS _bak_delete_orphan_channels_20261110_channels AS
  SELECT * FROM channels WHERE id IN (11,19,21,22,23);
CREATE TABLE IF NOT EXISTS _bak_delete_orphan_channels_20261110_abilities AS
  SELECT * FROM abilities WHERE channel_id IN (11,19,21,22,23);
COMMIT;
SELECT id, name, type, status FROM channels WHERE id IN (11,19,21,22,23) ORDER BY id;
SQL

echo
echo "=== ② 逐条走管理 API 删除 ==="
for ID in "${TARGET_IDS[@]}"; do
  CUR=$(Q "SELECT id||' '||name||' '||status FROM channels WHERE id=$ID;" | tr -d '\r\n ')
  if [ -z "$CUR" ]; then
    echo "渠道 $ID 不存在，跳过"
    continue
  fi
  echo "--- 渠道 $ID：$CUR"
  RESP=$(curl -s -X DELETE "$HOST/api/channel/$ID" \
    -H "Authorization: Bearer $AT" -H "New-Api-User: $AUID")
  echo "API: $(printf '%s' "$RESP" | head -c 200)"
done

echo
echo "=== ③ 复核 ==="
docker exec -i postgres psql -U root -d new-api -v ON_ERROR_STOP=1 -f - 2>&1 <<'SQL'
\echo '--- ① channels：目标 5 条必须 0 行 ---'
SELECT id, name, type, status FROM channels WHERE id IN (11,19,21,22,23) ORDER BY id;
\echo '--- ② abilities：目标 5 条必须 0 行 ---'
SELECT channel_id, model, enabled FROM abilities WHERE channel_id IN (11,19,21,22,23);
\echo '--- ③ 剩余全部渠道 ---'
SELECT id, name, type, status, models FROM channels ORDER BY id;
\echo '--- ④ 剩余能力行数（应与删除前一致，5 条渠道本就没有能力行）---'
SELECT count(*) AS ability_rows FROM abilities WHERE enabled;
\echo '--- ⑤ 广场卡片应保持 22 张，一张不少 ---'
SELECT count(*) AS square_cards FROM models WHERE deleted_at IS NULL AND status = 1;
SQL
