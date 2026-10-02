#!/usr/bin/env bash
# 只读核查：生产上 MiniMax/Hailuo 的平台与渠道现状（不改动任何数据）
set -u
q() { docker exec postgres psql -U root -d "${1:-new-api}" -t -A -c "$2" 2>&1; }

echo "=== 1. postgres 里有哪些库 ==="
q postgres-postgres "SELECT datname FROM pg_database WHERE datistemplate=false" | tr -d ' ' | paste -sd, -

echo
echo "=== 2. new-api: 名称/模型里带 minimax|hailuo|海螺 的渠道 ==="
q new-api "SELECT id || ' | ' || name || ' | type=' || type || ' | status=' || status FROM channels WHERE lower(name) ~ 'minimax|hailuo|海螺' OR lower(models) ~ 'minimax|hailuo'"

echo "(以上为空=生产无 MiniMax 渠道)"

echo
echo "=== 3. new-api: models 表里的 minimax/hailuo 模型 ==="
q new-api "SELECT model_name FROM models WHERE lower(model_name) ~ 'minimax|hailuo' LIMIT 20"

echo
echo "=== 4. new-api: 渠道类型清单（type -> 是否有视频任务能力）==="
q new-api "SELECT type || ' -> ' || count(*) FROM channels GROUP BY type ORDER BY type"

echo
echo "=== 5. ArcReel 库名探测 ==="
for db in $(q postgres-postgres "SELECT datname FROM pg_database WHERE datistemplate=false" | tr -d ' ' | grep -v '^new-api$'); do
  cnt=$(q "$db" "SELECT count(*) FROM information_schema.tables WHERE table_name IN ('platform','custom_provider_model')" 2>/dev/null | tr -d ' ')
  if [ "${cnt}" = "2" ]; then
    echo "ArcReel 库 = ${db}"
    echo "--- platform 表里 minimax/hailuo/海螺 ---"
    q "$db" "SELECT id || ' | ' || name || ' | owner=' || COALESCE(owner,'-') FROM platform WHERE lower(name) ~ 'minimax|hailuo|海螺'"
    echo "--- 这些平台的模型数 ---"
    q "$db" "SELECT p.name || ' -> ' || count(m.id) FROM platform p LEFT JOIN custom_provider_model m ON m.platform_id = p.id WHERE lower(p.name) ~ 'minimax|hailuo|海螺' GROUP BY p.name"
    echo "--- 对比：dola/heibaidao 平台现状 ---"
    q "$db" "SELECT p.name || ' -> ' || count(m.id) FROM platform p LEFT JOIN custom_provider_model m ON m.platform_id = p.id WHERE lower(p.name) ~ 'heibaidao|dola' GROUP BY p.name"
    break
  fi
done

echo
echo "=== 6. ArcReel 平台总览（前 20）==="
for db in $(q postgres-postgres "SELECT datname FROM pg_database WHERE datistemplate=false" | tr -d ' ' | grep -v '^new-api$'); do
  if [ "$(q "$db" "SELECT count(*) FROM information_schema.tables WHERE table_name='platform'" 2>/dev/null | tr -d ' ')" = "1" ]; then
    q "$db" "SELECT name FROM platform ORDER BY id LIMIT 20"
    break
  fi
done