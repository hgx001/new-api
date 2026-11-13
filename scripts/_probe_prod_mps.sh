#!/usr/bin/env bash
# 一次性核查脚本：生产库的 MPS 配置与 ModelPrice 现状（只读，不改任何数据）
set -uo pipefail

run_sql() {
  docker exec -i postgres psql -U root -d new-api -v ON_ERROR_STOP=1 -f - 2>&1
}

echo "=== ① options 里是否有 TencentMPSSetting ==="
run_sql <<'SQL'
SELECT key, length(value) AS value_len FROM options WHERE key = 'TencentMPSSetting';
SQL

echo "=== ② ModelPrice 里所有 wan3 相关键 ==="
run_sql <<'SQL'
SELECT e.key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key LIKE '%wan3%'
 ORDER BY e.key;
SQL

echo "=== ③ abilities 里 wan3 相关行 ==="
run_sql <<'SQL'
SELECT "group", model, channel_id, enabled, priority, weight
  FROM abilities WHERE model LIKE '%wan3%' ORDER BY channel_id, model;
SQL

echo "=== ④ models 广场里 wan3 相关卡片 ==="
run_sql <<'SQL'
SELECT model_name, status, sync_official, left(description, 50) AS desc_head
  FROM models WHERE model_name LIKE '%wan3%' AND deleted_at IS NULL ORDER BY model_name;
SQL

echo "=== ⑤ 渠道 12 详情 ==="
run_sql <<'SQL'
SELECT id, name, type, status, "group", priority, weight, models FROM channels WHERE id = 12;
SQL
