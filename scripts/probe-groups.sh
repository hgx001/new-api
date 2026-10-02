#!/usr/bin/env bash
# 只读：分组覆盖情况（abilities.group vs 用户实际 group）
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

echo "=== 全部 abilities（model / group / channel / enabled）==="
q "SELECT a.model, a.\"group\", a.channel_id, a.enabled FROM abilities a ORDER BY a.channel_id, a.model"

echo
echo "=== 用户分组分布 ==="
q "SELECT \"group\", count(*) FROM users GROUP BY \"group\" ORDER BY 2 DESC"

echo
echo "=== 令牌所属用户分组（有余额的那几个）==="
q "SELECT t.id, t.user_id, u.\"group\", u.quota, t.name FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 ORDER BY t.id LIMIT 12"

echo
echo "=== 系统里定义了哪些用户分组倍率（GroupRatio）==="
q "SELECT e.key || '=' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e WHERE o.key IN ('GroupRatio')"

echo
echo "=== 渠道分组字段 ==="
q "SELECT id, name, \"group\" FROM channels ORDER BY id"
