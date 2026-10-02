#!/usr/bin/env bash
# 只读核查 2：已存在的视频渠道类型 60/63 是什么，hailuo-h3 模型挂在哪
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -c "$1" 2>&1; }

echo "=== 1. 全部渠道（id / name / type / status / 前若干模型）==="
q "SELECT id || ' | ' || name || ' | type=' || type || ' | st=' || status || ' | ' || left(models, 90) FROM channels ORDER BY id"

echo
echo "=== 2. hailuo-h3-cankaosheng-* 挂在哪些渠道 ==="
q "SELECT c.id || ' | ' || c.name || ' | type=' || c.type || ' | ' || left(c.models, 120) FROM channels c WHERE lower(c.models) LIKE '%hailuo%'"

echo
echo "=== 3. abilities 表里 hailuo 模型的分组/优先级 ==="
q "SELECT model || ' | group=' || \"group\" || ' | ch=' || channel_id || ' | enabled=' || enabled FROM abilities WHERE lower(model) LIKE '%hailuo%'"

echo
echo "=== 4. ModelPrice 里 hailuo 相关定价 ==="
q "SELECT e.key || ' = ' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key = 'ModelPrice' AND lower(e.key) LIKE '%hailuo%'"

echo
echo "=== 5. 两个海螺模型的定价（任何 key 命中即可）==="
q "SELECT e.key || ' = ' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key = 'ModelPrice' AND lower(e.key) LIKE '%cankaosheng%'"
