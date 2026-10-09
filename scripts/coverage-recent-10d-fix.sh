#!/usr/bin/env bash
# 复用上一轮已生成的 /tmp/cover_all.out（不重跑测试），修掉路径前缀匹配问题：
# cover profile 里路径是 github.com/QuantumNous/new-api/constant/channel.go，
# 而 git 给的是 constant/channel.go —— 必须按后缀匹配，否则统计恒为 0。
set -uo pipefail
cd C:/work/new-api-src
[ -s /tmp/cover_all.out ] || { echo "profile 不在，请先跑 coverage-recent-10d.sh"; exit 1; }

echo "=== 改动文件：逐文件语句覆盖率 ==="
awk 'NR==FNR { want[$0]=1; next }
{
  split($1, a, ":"); path=a[1]
  for (w in want) {
    n=length(w)
    if (length(path) >= n && substr(path, length(path)-n+1) == w) {
      tot[w]+=$2; if ($3+0>0) cov[w]+=$2; break
    }
  }
}
END {
  T=0; C=0
  for (w in tot) { T+=tot[w]; C+=cov[w];
    printf "  %-42s %3d/%-3d  %5.1f%%\n", w, cov[w], tot[w], tot[w]?cov[w]*100/tot[w]:0 }
  printf "\n  合计 %d/%d 语句  =  %.1f%%\n", C, T, T?C*100/T:0
}' /tmp/changed.txt /tmp/cover_all.out | sort -k2 -r

echo
echo "=== 核实 controller / service 的失败是否基线已知 ==="
go test ./controller/ 2>&1 | grep -E '^\s*---? *FAIL|^FAIL' | head -6
go test ./service/   2>&1 | grep -E '^\s*---? *FAIL|^FAIL' | head -6