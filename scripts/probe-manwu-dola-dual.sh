#!/usr/bin/env bash
# 漫屋 dola 双模型生产零成本探针。
# 规则（AGENTS.md「生产运维硬约束」）：**只用必然被本地校验拦下的入参**。
# 2026-10-04 教训：seedance-2.0 的最小入参是合法的，拿它当探针会真建单并扣费
# （已误建 1 条 task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz，两侧取消 + 退款 + 标记日志）。
# 跑完必须断言 used_quota 差值为 0、tasks 无非终态任务。
set -uo pipefail

HOST="https://api.heibaidao.cn"
SSH=(ssh -p 877 ubuntu@119.29.253.97)
PG='docker exec postgres psql -U root -d new-api -t -A'

SQL_TOKEN='SELECT t.key FROM tokens t JOIN users u ON u.id=t.user_id WHERE t.status=1 AND u.quota>100000 ORDER BY t.id LIMIT 1'
SQL_OPEN='SELECT count(*) FROM tasks WHERE status NOT IN ('"'"'succeeded'"'"','"'"'failed'"'"','"'"'cancelled'"'"')'

run_sql() { "${SSH[@]}" "$PG -c \"$1\"" | tr -d '\r\n '; }

TK=$(run_sql "$SQL_TOKEN")
[[ -n "$TK" ]] || { echo "ABORT: no usable token"; exit 1; }
Q0=$(run_sql "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
OPEN0=$(run_sql "$SQL_OPEN")
ARC0=$("${SSH[@]}" "grep -c 'remote-generation/jobs' /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null || echo 0" | tr -d '\r\n ')
echo "=== 起始：sum(token.used_quota)=$Q0  非终态任务=$OPEN0  ArcReel 建单日志数=$ARC0 ==="

probe() { # name  full-json-body
  local name="$1" body="$2" resp
  resp=$(curl -s -o /tmp/probe.json -w '%{http_code}' -X POST "$HOST/v1/videos" \
    -H "Authorization: Bearer $TK" -H 'Content-Type: application/json' --data-raw "$body")
  printf '%-38s HTTP %s  %s\n' "$name" "$resp" "$(head -c 150 /tmp/probe.json | tr -d '\n')"
}

probe "seedance-2.0 非法 seconds=20"  '{"model":"seedance-2.0","prompt":"probe","idempotency_key":"pd20-20261004","seconds":20}'
probe "seedance-2.0 非法 ratio"       '{"model":"seedance-2.0","prompt":"probe","idempotency_key":"pd21-20261004","ratio":"16:10"}'
probe "seedance-2.5 非法 duration=15" '{"model":"seedance-2.5","prompt":"probe","idempotency_key":"pd25-20261004","duration":15}'
probe "seedance-2.5 非法 seconds=10"  '{"model":"seedance-2.5","prompt":"probe","idempotency_key":"pd26-20261004","seconds":"10"}'
probe "seedance-2.5 非法 resolution"  '{"model":"seedance-2.5","prompt":"probe","idempotency_key":"pd27-20261004","resolution":"1080p"}'
probe "seedance-2.5 非法参考图"        '{"model":"seedance-2.5","prompt":"probe","idempotency_key":"pd28-20261004","input_reference":["not-a-url"]}'
probe "旧名 dola-seedance-2.5 应失效" '{"model":"dola-seedance-2.5","prompt":"probe","idempotency_key":"pd29-20261004","seconds":20}'
probe "未知模型应失效"                '{"model":"seedance-3.0","prompt":"probe","idempotency_key":"pd30-20261004","seconds":20}'
probe "Nano Banana Pro n 越界"         '{"model":"Nano Banana Pro","prompt":"probe","idempotency_key":"pd31-20261004","n":99}'
probe "gemini-web-video 非法 seconds" '{"model":"gemini-web-video","prompt":"probe","idempotency_key":"pd32-20261004","seconds":7}'

Q1=$(run_sql "SELECT COALESCE(SUM(used_quota),0) FROM tokens")
OPEN1=$(run_sql "$SQL_OPEN")
ARC1=$("${SSH[@]}" "grep -c 'remote-generation/jobs' /home/ubuntu/arcreel/logs/arcreel.log 2>/dev/null || echo 0" | tr -d '\r\n ')
echo "=== 结束：sum(token.used_quota)=$Q1  非终态任务=$OPEN1  ArcReel 建单日志数=$ARC1 ==="
RC=0
[[ "$Q0" == "$Q1" ]] && echo "OK: 无扣费（差值 $((Q1-Q0))）" || { echo "FAIL: used_quota 变化 $Q0 -> $Q1"; RC=1; }
[[ "$OPEN0" == "$OPEN1" ]] && echo "OK: 无新任务（$OPEN0 -> $OPEN1）" || { echo "FAIL: 非终态任务 $OPEN0 -> $OPEN1"; RC=1; }
[[ "$ARC0" == "$ARC1" ]] && echo "OK: ArcReel 无建单（差值 $((ARC1-ARC0))）" || { echo "FAIL: ArcReel 建单 +$((ARC1-ARC0))"; RC=1; }
exit $RC
