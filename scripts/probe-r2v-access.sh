#!/usr/bin/env bash
# 确认 wan2.7-r2v 到底哪一环卡住：广场可见性 / 路由 / 入参校验 / 历史成败。
set -uo pipefail
SSH=(ssh -p 877 -o ConnectTimeout=15 ubuntu@119.29.253.97)
Q() { printf '%s' "$1" | "${SSH[@]}" "docker exec -i postgres psql -U root -d new-api -t -A -F' | '" | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;")

echo "=== A. 模型广场 /api/pricing 是否含 wan2.7-r2v ==="
curl -s -o /tmp/p.json "$HOST/api/pricing" -H "Authorization: Bearer $TK"
python - <<'PY'
import json, io
d = json.load(io.open('/tmp/p.json', encoding='utf-8'))
rows = d.get('data') or []
print('广场模型数:', len(rows))
for r in rows:
    if 'r2v' in r.get('model_name', '') or 'wan2.7' in r.get('model_name', ''):
        print('  HIT:', r.get('model_name'), '| quota_type=', r.get('quota_type'),
              '| model_price=', r.get('model_price'), '| endpoints=', r.get('supported_endpoint_types'))
else:
    if not any('r2v' in r.get('model_name','') for r in rows):
        print('  *** 广场里没有 wan2.7-r2v ***')
        print('  广场全部:', sorted(r.get('model_name') for r in rows))
PY

echo
echo "=== B. 真实调用：seconds 传字符串（符合 API 约定）+ 不带参考图 ==="
curl -s -o /tmp/r1.json -w 'HTTP=%{http_code}\n' -X POST "$HOST/v1/videos" \
  -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d '{"model":"wan2.7-r2v","prompt":"probe","seconds":"5"}'
head -c 300 /tmp/r1.json; echo

echo
echo "=== C. 真实调用：带一张公网参考图（该模型是参考生视频）==="
curl -s -o /tmp/r2.json -w 'HTTP=%{http_code}\n' -X POST "$HOST/v1/videos" \
  -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' \
  -d '{"model":"wan2.7-r2v","prompt":"probe","seconds":"5","input_reference":["https://picsum.photos/seed/1/512/512"]}'
head -c 300 /tmp/r2.json; echo

echo
echo "=== D. 历史使用记录（logs 表）==="
Q "SELECT type, count(*), max(to_timestamp(created_at)) FROM logs WHERE model_name='wan2.7-r2v' GROUP BY 1;"

echo
echo "=== E. 渠道 17 近期是否被 AutoBan / 有无错误日志 ==="
Q "SELECT id, status, left(coalesce(response_time::text,''),0), to_timestamp(created_time) FROM channels WHERE id=17;"
"${SSH[@]}" "docker logs new-api --since 72h 2>&1 | grep -c 'wan2.7-r2v' || true"
