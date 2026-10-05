#!/usr/bin/env bash
# 只读：new-api 生产中所有 MiniMax H3 相关模型与渠道
set -u
q() { docker exec postgres psql -U root -d new-api -t -A -F' | ' -c "$1" 2>&1; }

echo "=== 1. abilities 里所有 h3/minimax 模型（含渠道）==="
q "SELECT a.model || '  ->  ch' || a.channel_id || '  ' || c.name || '  type=' || c.type || '  group=' || a.\"group\" || '  en=' || a.enabled
   FROM abilities a JOIN channels c ON c.id = a.channel_id
   WHERE lower(a.model) LIKE '%h3%' OR lower(a.model) LIKE '%minimax%'
   ORDER BY a.channel_id, a.model"

echo
echo "=== 2. 这些模型的价格（ModelPrice / ModelRatio）==="
q "SELECT 'price: ' || e.key || ' = ' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
   WHERE o.key='ModelPrice' AND (lower(e.key) LIKE '%h3%' OR lower(e.key) LIKE '%minimax%') ORDER BY e.key"
q "SELECT 'ratio: ' || e.key || ' = ' || e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
   WHERE o.key='ModelRatio' AND (lower(e.key) LIKE '%h3%' OR lower(e.key) LIKE '%minimax%') ORDER BY e.key"

echo
echo "=== 3. 渠道 11（AutoDL）的全部模型 ==="
q "SELECT models FROM channels WHERE id=11"

echo
echo "=== 4. 渠道 21（官方 H3）概况 ==="
q "SELECT id || ' | ' || name || ' | type=' || type || ' | base=' || base_url || ' | models=' || models FROM channels WHERE id=21"

echo
echo "=== 5. 广场可见的 H3 模型（/api/pricing 白名单过滤后）==="
curl -s -m 20 https://api.heibaidao.cn/api/pricing | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
except Exception as e:
    print('parse fail', e); sys.exit()
data = d.get('data') or []
names = [x.get('model_name','') for x in data]
for n in sorted(names):
    if 'h3' in n.lower() or 'minimax' in n.lower():
        item = [x for x in data if x.get('model_name')==n][0]
        print(' ', n, '| quota_type=', item.get('quota_type'), '| model_price=', item.get('model_price'), '| model_ratio=', item.get('model_ratio'))
" 2>/dev/null
