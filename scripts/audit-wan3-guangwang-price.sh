#!/usr/bin/env bash
# 用真实成交账目反解 wan3.0-video-官网 各分辨率实际单价，与卡片文案比对。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }

echo "=== 近 30 天 wan3.0-video-官网 的计费明细（含倍率）==="
Q "SELECT t.id, t.quota,
       coalesce(t.private_data->'billing_context'->'other_ratios'->>'seconds','-') AS sec,
       coalesce(t.private_data->'billing_context'->'other_ratios'->>'size','-')    AS size,
       coalesce((t.data::jsonb->>'resolution'),'-') AS res,
       to_timestamp(t.created_at)
   FROM tasks t
   WHERE t.properties->>'origin_model_name' = 'wan3.0-video-官网'
     AND t.created_at > EXTRACT(EPOCH FROM now())::bigint - 2592000
   ORDER BY t.id DESC LIMIT 12;"

echo
echo "=== 反解：单价 = quota / (seconds × size) / 68493.15 ==="
Q "SELECT t.quota,
       coalesce(t.private_data->'billing_context'->'other_ratios'->>'seconds','-') AS sec,
       coalesce(t.private_data->'billing_context'->'other_ratios'->>'size','-')    AS size,
       round((t.quota::numeric /
              COALESCE(NULLIF(t.private_data->'billing_context'->'other_ratios'->>'seconds','')::numeric,1) /
              COALESCE(NULLIF(t.private_data->'billing_context'->'other_ratios'->>'size','')::numeric,1)
              / (500000.0/7.3))::numeric, 4) AS cny_per_second
   FROM tasks t
   WHERE t.properties->>'origin_model_name' = 'wan3.0-video-官网'
     AND t.created_at > EXTRACT(EPOCH FROM now())::bigint - 2592000
     AND t.quota > 0
   ORDER BY t.id DESC LIMIT 12;"

echo
echo "=== 卡片现文案 ==="
Q "SELECT model_name, description FROM models
   WHERE deleted_at IS NULL AND model_name IN ('wan3.0-video-官网','wan3.0-video','wan3.0-smart');"
