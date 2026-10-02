#!/usr/bin/env bash
# 找到误建任务的计费记录并退款
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

echo "=== logs 表结构 ==="
q "SELECT column_name, data_type FROM information_schema.columns WHERE table_name='logs' ORDER BY ordinal_position"

echo
echo "=== quota=171232 的日志（该任务预扣额）==="
q "SELECT id, user_id, created_at, type, quota, LEFT(content, 160) FROM logs WHERE quota = 171232 ORDER BY id DESC LIMIT 5"

echo
echo "=== 最近 10 条日志 ==="
q "SELECT id, user_id, created_at, type, quota, LEFT(COALESCE(content,''), 90) FROM logs ORDER BY id DESC LIMIT 10"

echo
echo "=== 令牌与用户额度现状 ==="
q "SELECT t.id AS token_id, t.user_id, t.name, t.used_quota, u.quota AS user_quota FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.id=1"

echo
echo "=== 该任务关联的日志（按 token 反查最近消费）==="
q "SELECT id, user_id, type, quota, LEFT(COALESCE(content,''), 200) FROM logs WHERE user_id=2 ORDER BY id DESC LIMIT 5"
