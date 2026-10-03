#!/usr/bin/env bash
# 只读：查 models/abilities 表结构与现有 manwu 行（作为插入模板）
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

echo "=== models 表列 ==="
q "SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_name='models' ORDER BY ordinal_position"

echo
echo "=== abilities 表列 ==="
q "SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_name='abilities' ORDER BY ordinal_position"

echo
echo "=== 现有 manwu 模型行（插入模板）==="
q "SELECT model_name, quota_type, model_ratio, model_price, enable_groups, status, source FROM models WHERE model_name IN ('Nano Banana Pro','gemini-web-video','dola-seedance-2.5','jimeng-video-reverse')"

echo
echo "=== 现有 manwu abilities 行 ==="
q "SELECT model, \"group\", channel_id, enabled, priority, weight FROM abilities WHERE channel_id=20 ORDER BY model"

echo
echo "=== channel 1（MiniMax）概况：id/type/base_url/分组，不含 key ==="
q "SELECT id, name, type, status, base_url, \"group\", priority, weight FROM channels WHERE id=1"
q "SELECT length(key) AS key_len, left(key, 4) AS key_prefix FROM channels WHERE id=1"

echo
echo "=== DB options 里的 ModelRatio 是否已含 H3 系列 ==="
q "SELECT e.key || ' = ' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key='ModelRatio' AND lower(e.key) LIKE '%h3%'"

echo
echo "=== DB options 里的 ModelPrice 是否已含 H3 系列 ==="
q "SELECT e.key || ' = ' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key='ModelPrice' AND lower(e.key) LIKE '%h3%'"

echo
echo "=== options 表 ModelRatio/ModelPrice 是否有值（判断是否会覆盖代码默认值）==="
q "SELECT key, length(value::text) FROM options WHERE key IN ('ModelRatio','ModelPrice')"
