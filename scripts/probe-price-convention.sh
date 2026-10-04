#!/usr/bin/env bash
# 只读：确认「按时长计费的视频模型」在本部署里放 ModelRatio 还是 ModelPrice
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

echo "=== ModelRatio 里的视频模型（按时长计费的应在此）==="
q "SELECT e.key || ' = ' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
   WHERE o.key='ModelRatio' AND (lower(e.key) LIKE '%wan3%' OR lower(e.key) LIKE '%wan2%' OR lower(e.key) LIKE '%video%' OR lower(e.key) LIKE '%hailuo%')
   ORDER BY e.key"

echo
echo "=== ModelPrice 里的视频模型 ==="
q "SELECT e.key || ' = ' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
   WHERE o.key='ModelPrice' AND (lower(e.key) LIKE '%wan%' OR lower(e.key) LIKE '%video%' OR lower(e.key) LIKE '%hailuo%' OR lower(e.key) LIKE '%seedance%')
   ORDER BY e.key"

echo
echo "=== seedance-2.0 在哪个表（漫屋按次模型，作为对照）==="
q "SELECT 'ModelRatio: ' || COALESCE((SELECT e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key='ModelRatio' AND e.key='seedance-2.0'), '（无）')"
q "SELECT 'ModelPrice: ' || COALESCE((SELECT e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key='ModelPrice' AND e.key='seedance-2.0'), '（无）')"
q "SELECT 'ModelRatio(seedance-2.5): ' || COALESCE((SELECT e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key='ModelRatio' AND e.key='seedance-2.5'), '（无）')"
q "SELECT 'ModelPrice(seedance-2.5): ' || COALESCE((SELECT e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key='ModelPrice' AND e.key='seedance-2.5'), '（无）')"

echo
echo "=== code 侧 wan3 的 ModelRatio 默认值（对照官方价）==="
echo "(见 setting/ratio_setting/model_ratio.go，本地已查)"
