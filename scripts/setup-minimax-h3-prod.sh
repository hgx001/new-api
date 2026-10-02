#!/usr/bin/env bash
# 生产变更：① 修复漫屋 abilities 被 AutoBan 连带关闭导致的全模型 503
#          ② 新建 MiniMax H3 视频渠道并配置三个模型
# 全程不打印 key；每步可回滚（DB 备份 + 打印新 id）。
set -euo pipefail

q()  { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1"; }
qq() { docker exec postgres psql -U root -d new-api -t -A -c "$1"; }

echo "=== 0. 备份 ==="
TS=$(date +%Y%m%d%H%M%S)
mkdir -p /home/ubuntu/new-api/backups
docker exec postgres pg_dump -U root -d new-api > "/home/ubuntu/new-api/backups/pre-h3-${TS}.sql"
echo "备份: /home/ubuntu/new-api/backups/pre-h3-${TS}.sql"

echo
echo "=== 1. 修复漫屋 abilities（AutoBan 关渠道时连带关能力，恢复 status 时被漏掉）==="
q "SELECT channel_id, model, enabled FROM abilities WHERE channel_id=20 ORDER BY model"
q "UPDATE abilities SET enabled=true WHERE channel_id=20" >/dev/null
echo "修复后:"
q "SELECT channel_id, model, enabled FROM abilities WHERE channel_id=20 ORDER BY model"

echo
echo "=== 2. 克隆 channel 1（MiniMax 文本）建视频渠道，key 在 SQL 内复制不外泄 ==="
COLS=$(qq "SELECT string_agg(quote_ident(column_name), ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_name='channels' AND is_generated='NEVER' AND column_name <> 'id'")
# 覆盖项：名称/类型/base_url(用官方 v2 域名)/模型清单/描述类字段
OVERRIDE_NAME="'MiniMax-H3-Video'"
OVERRIDE_TYPE="35"
OVERRIDE_BASE="'https://api.minimax.io'"
OVERRIDE_MODELS="'MiniMax-H3,MiniMax-H3-Max,MiniMax-H3-Context-IR'"
OVERRIDE_DESC="'MiniMax 官方 v2 视频（H3/H3-Max）与 H3-Context-IR 提示词增强'"

SELECT_EXPR=""
IFS=',' read -ra ARR <<< "$COLS"
for c in "${ARR[@]}"; do
  case "$c" in
    name)     v="$OVERRIDE_NAME" ;;
    type)     v="$OVERRIDE_TYPE" ;;
    base_url) v="$OVERRIDE_BASE" ;;
    models)   v="$OVERRIDE_MODELS" ;;
    *)        v="$c" ;;
  esac
  if [ -z "$SELECT_EXPR" ]; then SELECT_EXPR="$v"; else SELECT_EXPR="$SELECT_EXPR, $v"; fi
done

EXIST=$(qq "SELECT id FROM channels WHERE name='MiniMax-H3-Video'" | grep -E '^[0-9]+$' | head -1)
if [ -n "$EXIST" ]; then
  NEWID="$EXIST"
  echo "渠道已存在，跳过创建: id=$NEWID"
  qq "UPDATE channels SET base_url='https://api.minimax.io', models='MiniMax-H3,MiniMax-H3-Max,MiniMax-H3-Context-IR', status=1 WHERE id=$NEWID" >/dev/null
else
  NEWID=$(qq "INSERT INTO channels (${COLS}) SELECT ${SELECT_EXPR} FROM channels WHERE id=1 RETURNING id" | grep -E '^[0-9]+$' | head -1)
  echo "新建渠道 id=$NEWID"
fi
echo "渠道概况（不含 key）:"
q "SELECT id, name, type, status, base_url, \"group\", models FROM channels WHERE id=$NEWID"

echo
echo "=== 3. models 表登记三个模型 ==="
NOW=$(date +%s)
for M in "MiniMax-H3|MiniMax H3 视频生成（768P/2K，4-15 秒）" \
         "MiniMax-H3-Max|MiniMax H3 Max 高速视频（480P/768P，5-15 秒）" \
         "MiniMax-H3-Context-IR|MiniMax H3 上下文解析（只产提示词，不产视频）"; do
  NAME="${M%%|*}"; DESC="${M##*|}"
  EX=$(qq "SELECT id FROM models WHERE model_name='$NAME'")
  if [ -n "$EX" ]; then
    qq "UPDATE models SET status=1, updated_time=$NOW WHERE model_name='$NAME'" >/dev/null
    echo "已存在，更新: $NAME"
  else
    qq "INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time, name_rule)
        VALUES ('$NAME', '$DESC', 1, 0, $NOW, $NOW, 0)" >/dev/null
    echo "新增: $NAME"
  fi
done
q "SELECT model_name, status, description FROM models WHERE model_name LIKE 'MiniMax-H3%' ORDER BY model_name"

echo
echo "=== 4. abilities 登记（group=default，与其它渠道一致）==="
for M in MiniMax-H3 MiniMax-H3-Max MiniMax-H3-Context-IR; do
  qq "DELETE FROM abilities WHERE channel_id=$NEWID AND model='$M' AND \"group\"='default'" >/dev/null
  qq "INSERT INTO abilities (\"group\", model, channel_id, enabled, priority, weight)
      VALUES ('default', '$M', $NEWID, true, 10, 1)" >/dev/null
done
q "SELECT model, \"group\", channel_id, enabled, priority, weight FROM abilities WHERE channel_id=$NEWID ORDER BY model"

echo
echo "=== 5. 写入 ModelPrice（DB 覆盖代码默认值；单位 USD 等值，×7.3 得人民币）==="
# H3 基础档 768P ¥0.50/秒；H3-Max 基础档 480P ¥0.365/秒；Context-IR 按次 ¥1.0（占位价）
qq "UPDATE options SET value = (value::jsonb || jsonb_build_object(
      'MiniMax-H3', 0.5/7.3,
      'MiniMax-H3-Max', 0.365/7.3,
      'MiniMax-H3-Context-IR', 1.0/7.3
    ))::text WHERE key='ModelPrice'" >/dev/null
q "SELECT e.key || ' = ' || e.value || '  (CNY ' || round((e.value::numeric)*7.3, 4) || ')' FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
   WHERE o.key='ModelPrice' AND e.key LIKE 'MiniMax-H3%' ORDER BY e.key"

echo
echo "=== 6. 变更后总览 ==="
q "SELECT c.id, c.name, c.status, count(a.*) FILTER (WHERE a.enabled) AS abilities_on
   FROM channels c LEFT JOIN abilities a ON a.channel_id=c.id
   WHERE c.id IN (1,20,$NEWID) GROUP BY c.id, c.name, c.status ORDER BY c.id"
echo "NEW_CHANNEL_ID=$NEWID"