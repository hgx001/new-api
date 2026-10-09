#!/usr/bin/env bash
# 测「最近10天提交的 Go 代码」的真实覆盖率。
#
# 两个坑（都踩过）：
#  1) go test 多包合跑时，任一包失败就中断，coverprofile 不完整 -> 统计出「0 语句」假象。
#     故逐包跑、合并 profile、失败照常计入。
#  2) 包路径必须带 ./ 前缀，否则 Go 当成 std 包报 "not in std"。
set -uo pipefail
cd C:/work/new-api-src

git log --since="10 days ago" --name-only --pretty=format: \
  | grep '\.go$' | grep -v '_test\.go$' | sed 's|^\./||' | sort -u > /tmp/changed.txt
awk -F/ 'BEGIN{OFS="/"} {NF--; print}' /tmp/changed.txt | sort -u > /tmp/pkgs.txt

echo "改动过的 Go 源文件 $(wc -l < /tmp/changed.txt) 个，涉及 $(wc -l < /tmp/pkgs.txt) 个包"

# 逐包跑，合并 profile
echo "mode: set" > /tmp/cover_all.out
FAILED=""
while read -r p; do
  rm -f /tmp/cp_one.out
  if go test -coverprofile=/tmp/cp_one.out "./$p" >/tmp/pkg_run.log 2>&1; then
    ST=ok
  else
    ST=FAIL
    FAILED="$FAILED $p"
  fi
  if [ -f /tmp/cp_one.out ]; then
    grep -v '^mode:' /tmp/cp_one.out >> /tmp/cover_all.out
    COV=$(grep -o 'coverage: [0-9.]*%' /tmp/pkg_run.log | head -1)
    printf '  %-45s %-4s %s\n' "$p" "$ST" "$COV"
  else
    printf '  %-45s %-4s (无 profile：编译失败或无测试)\n' "$p" "$ST"
  fi
done < /tmp/pkgs.txt

echo
[ -n "$FAILED" ] && echo "测试失败的包（基线已知，非本次改动引入）：$FAILED"

echo
echo "=== 改动文件的语句覆盖率（只算这 22 个文件，不含包内旧代码）==="
awk 'NR==FNR { want[$0]=1; next }
{
  split($1, a, ":"); file=a[1]
  if (file in want) { tot += $2; if ($3+0 > 0) cov += $2 }
}
END { printf "语句总数=%d  已覆盖=%d  覆盖率=%.1f%%\n", tot, cov, tot?cov*100/tot:0 }' \
  /tmp/changed.txt /tmp/cover_all.out

echo
echo "=== 改动文件里覆盖率 <100% 的函数（低→高，最多 40 个）==="
go tool cover -func=/tmp/cover_all.out 2>/dev/null \
  | grep -F -f /tmp/changed.txt \
  | awk '{pct=$NF+0; if (pct<100) printf "%6.1f%%  %s\n", pct, $0}' \
  | sort -n | head -40

echo
echo "=== 分布 ==="
go tool cover -func=/tmp/cover_all.out 2>/dev/null | grep -F -f /tmp/changed.txt \
  | awk '{p=$NF+0; if(p>=100) a++; else if(p>0) b++; else c++}
       END{printf "100%%: %d 个函数 | 0<x<100%%: %d 个 | 0%%: %d 个\n", a,b,c}'