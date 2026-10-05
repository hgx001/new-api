#!/usr/bin/env bash
# 同步生产 ModelPrice option：DB 里的值会覆盖 model_ratio.go 的代码默认值，
# 只改代码线上不变。调价必须两处同改。
#   seedance-2.0：¥1.50/次 → ¥1.00/次（0.2054794520547945 → 0.136986301369863）
#   seedance-2.5：¥1.00/次 → ¥0.80/次（0.136986301369863 → 0.10958904109589042）
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
BK="_bak_modelprice_20261005"

echo "=== 改前 ==="
Q "SELECT e.key, e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
   WHERE o.key='ModelPrice' AND e.key IN ('seedance-2.0','seedance-2.5','wan3.0-smart') ORDER BY e.key;"

echo
echo "=== 执行（先备份）==="
Q "CREATE TABLE IF NOT EXISTS $BK AS SELECT value FROM options WHERE key='ModelPrice';"
Q "UPDATE options SET value = jsonb_set(
       jsonb_set(value::jsonb, '{seedance-2.0}', '0.136986301369863'),
       '{seedance-2.5}', '0.10958904109589042')::text
   WHERE key='ModelPrice';"

echo
echo "=== 改后 ==="
Q "SELECT e.key, e.value FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
   WHERE o.key='ModelPrice' AND e.key IN ('seedance-2.0','seedance-2.5','wan3.0-smart') ORDER BY e.key;"

echo
echo "=== 等 option 热同步后核对 /api/pricing ==="
sleep 70
curl -s -o /tmp/p.json "https://api.heibaidao.cn/api/pricing" -H "Authorization: Bearer $(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')"
python3 - <<'PY'
import json
d = json.load(open('/tmp/p.json', encoding='utf-8'))
U = 500000 / 7.3
for r in d.get('data') or []:
    n = r.get('model_name')
    if n in ('seedance-2.0', 'seedance-2.5', 'wan3.0-smart'):
        p = r.get('model_price') or 0
        print(f"  {n:16} model_price={p!r:24} => CNY {p*U:.6f}")
PY
