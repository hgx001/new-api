#!/usr/bin/env bash
# 定位用户 15305968411，并确认生产既有的充值口径（quota/¥ 换算与记账方式）。
# 不做任何写操作。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }

echo "=== A. 按 username / phone / email 模糊找（15305968411）==="
Q "SELECT id, username, COALESCE(phone,''), COALESCE(email,''), \"group\", status, quota, to_timestamp(created_time)
   FROM users
   WHERE username LIKE '%15305968411%' OR COALESCE(phone,'') LIKE '%15305968411%'
      OR COALESCE(email,'') LIKE '%15305968411%'
   ORDER BY id;"

echo
echo "=== B. users 表有哪些联系方式列（防止漏查字段）==="
Q "SELECT string_agg(column_name, ',') FROM information_schema.columns WHERE table_name='users' AND (column_name LIKE '%phone%' OR column_name LIKE '%email%' OR column_name LIKE '%user%' OR column_name LIKE '%name%');"

echo
echo "=== C. 生产充值换算口径：最近的人工充值记录长什么样 ==="
Q "SELECT id, user_id, quota, delta_type, left(COALESCE(content,''),80), to_timestamp(created_time)
   FROM logs WHERE type = 4 AND user_id > 0 ORDER BY id DESC LIMIT 8;"

echo
echo "=== D. QuotaPerUnit 换算基准 ==="
Q "SELECT e.key, e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
   WHERE o.key IN ('QuotaPerUnit','DisplayInCurrencyEnabled','USDExchangeRate');"
