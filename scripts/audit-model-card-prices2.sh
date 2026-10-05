#!/usr/bin/env bash
# 逐个核对：卡片文案 vs 实际生效价（含 adaptor 的分辨率倍率），并标出哪些模型可路由。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }

echo "=== 这三个模型分别由谁服务（type → adaptor）==="
Q "SELECT a.model, a.channel_id, c.type,
       CASE c.type WHEN 63 THEN 'youzanwan3' WHEN 62 THEN 'dashscope' WHEN 66 THEN 'manwu' ELSE 'other' END AS adaptor
   FROM abilities a JOIN channels c ON c.id=a.channel_id
   WHERE a.model IN ('wan3.0-smart','wan3.0-video','wan3.0-video-官网','wan2.7-r2v','wan3.0-video-prime-1080p')
     AND a.enabled ORDER BY a.model;"

echo
echo "=== 实际生效价（ModelPrice 480P 基准 × 各 adaptor 的分辨率倍率）==="
Q "SELECT o2.model_name,
       round((e.value::numeric*7.3)::numeric,4) AS base_480p_cny,
       CASE WHEN o2.model_name IN ('wan3.0-smart','wan2.7-r2v') THEN 'youzanwan3 表'
            ELSE 'wan3 表 1:2:4' END AS ratio_table
   FROM models o2
   JOIN options o ON o.key='ModelPrice'
   JOIN LATERAL jsonb_each_text(o.value::jsonb) e ON e.key=o2.model_name
   WHERE o2.deleted_at IS NULL
     AND o2.model_name IN ('wan3.0-smart','wan3.0-video','wan3.0-video-官网','wan2.7-r2v')
   ORDER BY o2.model_name;"

echo
echo "=== wan3(DashScope) 表：1.0 / 2.0 / 4.0 → 实际三档 ==="
echo "  wan3.0-video       base 0.27 → 480p 0.27 / 720p 0.54 / 1080p 1.08"
echo "  wan3.0-video-官网  base 0.27 → 480p 0.27 / 720p 0.54 / 1080p 1.08"
echo "  youzanwan3 smart   base 0.28 → 480p 0.28 / 720p 0.28 / 1080p 0.30  （已调价）"
echo "  r2v 统一价          base 0.10 → 720p 0.10 / 1080p 0.10"

echo
echo "=== 这四个模型有真实成交记录吗（近 30 天）==="
Q "SELECT model_name, count(*), max(to_timestamp(created_at))
   FROM logs WHERE model_name IN ('wan3.0-smart','wan3.0-video','wan3.0-video-官网','wan2.7-r2v')
     AND created_at > EXTRACT(EPOCH FROM now())::bigint - 2592000
   GROUP BY 1 ORDER BY 1;"
