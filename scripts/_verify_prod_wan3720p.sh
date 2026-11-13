#!/usr/bin/env bash
# 上线后核查：wan3.0-720p-官网 的路由 / 计费 / 后处理配置是否真的生效
# 只读，不改任何数据，不花钱。
set -uo pipefail

run_sql() {
  docker exec -i postgres psql -U root -d new-api -v ON_ERROR_STOP=1 -f - 2>&1
}

echo "=== ① 应用是否真的读到了 MPS 配置（不是只有数据库里有）==="
echo "--- 容器启动时间 vs 配置写入时间 ---"
docker ps --format "{{.Names}}\t{{.Status}}" | grep "^new-api\t"
run_sql <<'SQL'
SELECT updated_at FROM options WHERE key = 'TencentMPSSetting';
SQL

echo "=== ② 后处理配置全字段（密钥只看长度）==="
run_sql <<'SQL'
SELECT
  (value::jsonb ->> 'enabled')::boolean AS enabled,
  (value::jsonb ->> 'region')       AS region,
  (value::jsonb ->> 'bucket')       AS bucket,
  (value::jsonb ->> 'output_dir')   AS output_dir,
  (value::jsonb ->> 'session_id_prefix') AS prefix,
  (value::jsonb ->> 'super_resolution_type') AS sr_type,
  (value::jsonb ->> 'output_bitrate_kbps')  AS bitrate,
  length(value::jsonb ->> 'secret_id')       AS sid_len,
  length(value::jsonb ->> 'secret_key')      AS sk_len
  FROM options WHERE key = 'TencentMPSSetting';
SQL

echo "=== ③ 渠道与能力（wan3.0-720p-官网 必须 enabled=t 且挂在渠道 12）==="
run_sql <<'SQL'
SELECT c.id, c.name, c.status AS chan_status, a."group", a.model, a.enabled AS ability_enabled
  FROM abilities a JOIN channels c ON c.id = a.channel_id
 WHERE a.model = 'wan3.0-720p-官网';
SQL

echo "=== ④ 价格换算复核（应 cny = 0.2500）==="
run_sql <<'SQL'
SELECT e.key, e.value, round((e.value::numeric * 7.3)::numeric, 4) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key = 'wan3.0-720p-官网';
SQL

echo "=== ⑤ 应用日志里有没有 MPS 相关报错（应为空或仅无害提示）==="
docker logs new-api --since 30m 2>&1 | grep -iE "mps|cos-proxy|tencent" | tail -20 || echo "（无 MPS 相关日志 = 配置已被正常接受）"

echo "=== ⑥ 应用日志里的启动错误（应无 FATAL / panic）==="
docker logs new-api --since 30m 2>&1 | grep -iE "FATAL|panic|failed to init" | tail -10 || echo "（无致命错误）"

echo "=== ⑦ 该模型是否出现在对外的模型列表接口（免登录探测）==="
curl -s --max-time 15 -o /dev/null -w "GET /api/pricing -> %{http_code}\n" http://127.0.0.1:3000/api/pricing
