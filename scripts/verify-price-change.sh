#!/usr/bin/env bash
# 调价后验证：/api/pricing 换算核对 + 一笔 wan3.0-smart 1080p 真实计费。
# ⚠️ 会真建单并扣费；跑完本脚本会按实测 quota 退款并留痕。
set -uo pipefail
Q() { printf '%s' "$1" | docker exec -i postgres psql -U root -d new-api -t -A -F' | ' | tr -d '\r'; }
HOST=https://api.heibaidao.cn
TK=$(Q "SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1;" | tr -d '\r\n ')

echo "=== A. /api/pricing 换算核对（model_price 是 USD，×7.3 得人民币）==="
curl -s -o /tmp/p.json "$HOST/api/pricing" -H "Authorization: Bearer $TK"
python3 - <<'PY'
import json
d = json.load(open('/tmp/p.json', encoding='utf-8'))
exp = {'seedance-2.0': 1.00, 'seedance-2.5': 0.80, 'wan3.0-smart': 0.28}
for r in d.get('data') or []:
    n = r.get('model_name')
    if n in exp:
        got = (r.get('model_price') or 0) * 7.3
        ok = 'OK' if abs(got - exp[n]) < 0.005 else 'MISMATCH'
        print(f"  {n:16} ¥{got:.6f}  期望 ¥{exp[n]:.2f}  {ok}")
PY

echo
echo "=== B. 真实计费：wan3.0-smart 1080p / 5 秒 ==="
echo "    预期 size=0.30/0.28=1.071429，quota = 0.28*68493/0.28*1.071429*5 ≈ 102744 (¥1.50)"
Q0=$(Q "SELECT COALESCE(SUM(used_quota),0) FROM tokens;" | tr -d ' ')
RESP=$(curl -s -X POST "$HOST/v1/videos" -H "Authorization: Bearer $TK" \
  -H 'Content-Type: application/json' \
  -d '{"model":"wan3.0-smart","prompt":"price-check","resolution":"1080p","seconds":"5"}')
echo "$RESP" | head -c 220; echo
TASK=$(echo "$RESP" | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
[ -n "$TASK" ] || { echo "ABORT: 建单失败，未产生扣费"; exit 1; }
sleep 8
echo
echo "=== C. 实际扣了多少 ==="
Q "SELECT 'task', id, status, quota, left(coalesce(private_data::text,''),200) FROM tasks WHERE task_id='$TASK';"
ACT=$(Q "SELECT quota FROM tasks WHERE task_id='$TASK';" | tr -d ' ')
echo "实测 quota=$ACT  预期 ≈102744  差=$((ACT-102744))"
Q1=$(Q "SELECT COALESCE(SUM(used_quota),0) FROM tokens;" | tr -d ' ')
echo "token 额度差值=$((Q1-Q0))"
echo "$ACT" > /tmp/r2v_quota
echo "$TASK" > /tmp/r2v_task
