#!/usr/bin/env bash
# 只读调查：读 erchun v1 的 /v1/models、/v1/catalog、/v1/pricing。
# 密钥从生产库的渠道 19 读取，全程不打印、不落盘到本机、不进上下文。
set -uo pipefail
BASE="https://api.erchun.youkou.cc"
KEY=$(printf '%s' "SELECT key FROM channels WHERE id=19;" | docker exec -i postgres psql -U root -d new-api -t -A | tr -d '\r\n ')
if [ -z "$KEY" ]; then echo "渠道 19 密钥为空"; exit 1; fi
echo "密钥已读取（长度 ${#KEY}，前 7 字符 $(echo "$KEY" | cut -c1-7)***）"
echo

for EP in models catalog pricing; do
  echo "=== GET /v1/$EP ==="
  CODE=$(curl -s -o "/tmp/erchun_$EP.json" -w '%{http_code}' --max-time 30 \
    -H "Authorization: Bearer $KEY" "$BASE/v1/$EP")
  echo "HTTP $CODE   字节 $(stat -c%s "/tmp/erchun_$EP.json")"
  if [ "$CODE" != "200" ]; then head -c 400 "/tmp/erchun_$EP.json"; echo; echo; fi
done

echo "=== 各端点返回摘要 ==="
for EP in models catalog pricing; do
  echo "--- /v1/$EP 顶层结构 ---"
  python3 - "/tmp/erchun_$EP.json" <<'PY'
import json,sys
try:
    d=json.load(open(sys.argv[1]))
except Exception as e:
    print("  解析失败:", e); sys.exit()
def walk(o,p="",depth=0):
    if depth>2: return
    if isinstance(o,dict):
        for k,v in o.items():
            t=type(v).__name__
            n=f"[{len(v)}]" if isinstance(v,(list,dict)) else ""
            print(f"  {p}{k}: {t}{n}")
            if isinstance(v,(dict,list)) and depth<2: walk(v,"  ",depth+1)
    elif isinstance(o,list) and o:
        print(f"  {p}[0] 样例:"); walk(o[0],"  ",depth+1)
walk(d)
PY
  echo
done
