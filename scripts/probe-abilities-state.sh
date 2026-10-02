#!/usr/bin/env bash
# 只读：确认漫屋模型当前是否还可路由（abilities.enabled=false 的影响）
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

echo "=== 全部 abilities 的 enabled 分布 ==="
q "SELECT enabled, count(*) FROM abilities GROUP BY enabled"

echo
echo "=== 各渠道的 abilities 启用情况 ==="
q "SELECT channel_id, count(*) FILTER (WHERE enabled) AS on, count(*) FILTER (WHERE NOT enabled) AS off FROM abilities GROUP BY channel_id ORDER BY channel_id"

echo
echo "=== channels.status 与其 abilities 是否一致（不一致=有渠道被手工恢复但没恢复能力）==="
q "SELECT c.id, c.name, c.status, count(a.*) FILTER (WHERE a.enabled) AS abilities_on, count(a.*) AS abilities_total
   FROM channels c LEFT JOIN abilities a ON a.channel_id = c.id
   WHERE c.status = 1 GROUP BY c.id, c.name, c.status ORDER BY c.id"

echo
echo "=== 漫屋 token 实际能否路由到模型（零成本：本地校验先失败，不建单）==="
TK=$(q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 LIMIT 1")
for M in dola-seedance-2.5 manwu-image gemini-web-video jimeng-video-reverse; do
  R=$(curl -s -o /tmp/r.json -w "%{http_code}" -X POST https://api.heibaidao.cn/v1/videos \
    -H "Authorization: Bearer $TK" -H "Content-Type: application/json" \
    -d "{\"model\":\"$M\",\"prompt\":\"x\",\"n\":99}")
  printf '%-24s HTTP=%s %s\n' "$M" "$R" "$(head -c 150 /tmp/r.json | tr -d '\n')"
done
