#!/usr/bin/env bash
# 审查：模型广场卡片（models 表 description）里写的价格，与生产 ModelPrice 是否一致。
# 卡片文案是运维字段走数据库，不跟代码走——调价后最容易漏改。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
U=68493.1506   # 1 元 = 500000/7.3 quota；ModelPrice 是 USD，×7.3 = 元

echo "=== 卡片描述里带价格的模型 ==="
Q "SELECT model_name, description FROM models
   WHERE deleted_at IS NULL AND (description LIKE '%¥%' OR description ~ '[0-9]+\\.[0-9]+/s' OR description LIKE '%元%')
   ORDER BY model_name;"

echo
echo "=== 对照生产 ModelPrice（USD → 元）==="
Q "SELECT o2.model_name, coalesce(e.value,'') AS usd,
       round((e.value::numeric * 7.3)::numeric, 4) AS cny
   FROM models o2
   LEFT JOIN options o ON o.key='ModelPrice'
   LEFT JOIN LATERAL jsonb_each_text(o.value::jsonb) e ON e.key = o2.model_name
   WHERE o2.deleted_at IS NULL
     AND (o2.description LIKE '%¥%' OR o2.description LIKE '%元%')
   ORDER BY o2.model_name;"

echo
echo "=== 有赞 wan3 三个卡片的完整描述 ==="
Q "SELECT model_name, description FROM models
   WHERE deleted_at IS NULL AND model_name IN ('wan3.0-smart','wan3.0-video-prime-1080p','wan2.7-r2v')
   ORDER BY model_name;"
