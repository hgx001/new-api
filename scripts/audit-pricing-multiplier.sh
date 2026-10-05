#!/usr/bin/env bash
# 模型广场价格审查：抓线上 pricing 数据 + 各 adaptor 的实际计费倍率
set -uo pipefail
cd /c/work/new-api-src

echo "=== A. 各 task adaptor 的 EstimateBilling（决定实际扣费 = 基础价 × 倍率）==="
for f in $(grep -rl 'func (a \*Adaptor) EstimateBilling' relay/channel/task --include=adaptor.go | sort); do
  echo "--- $f"
  awk '/func \(a \*Adaptor\) EstimateBilling/,/^}/' "$f" | grep -E 'Model|case |return |Ratio|ratio|n"|seconds|size' | head -n 18
done

echo
echo "=== B. relay_task.go 里 TASK_PRICE_PATCH / 倍率相乘逻辑 ==="
grep -n 'TASK_PRICE_PATCH' -A 12 relay/relay_task.go | head -n 40
grep -n 'BillingRatios\|EstimateBilling' relay/relay_task.go | head -n 20

echo
echo "=== C. 线上 /api/pricing 概况 ==="
curl -s https://api.heibaidao.cn/api/pricing -o /tmp/pricing.json
node -e '
const d = JSON.parse(require("fs").readFileSync("/tmp/pricing.json","utf8"));
const rows = d.data || [];
console.log("模型总数:", rows.length);
const byType = {};
for (const m of rows) byType[m.quota_type] = (byType[m.quota_type]||0)+1;
console.log("quota_type 分布:", JSON.stringify(byType), "(0=按token 1=按次)");
console.log("group_ratio:", JSON.stringify(d.group_ratio));
console.log("usable_group:", JSON.stringify(d.usable_group));
console.log("带 billing_expr 的模型:", rows.filter(m=>m.billing_expr).map(m=>m.model_name).join(", ") || "(无)");
console.log("带 billing_mode 的模型:", rows.filter(m=>m.billing_mode).map(m=>m.model_name+"="+m.billing_mode).join(", ") || "(无)");
console.log("异常：quota_type=0 但 model_price>0 =>", rows.filter(m=>m.quota_type===0 && m.model_price>0).map(m=>m.model_name).join(",")||"(无)");
console.log("异常：quota_type=1 但 model_ratio<=0 =>", rows.filter(m=>m.quota_type===1 && !(m.model_ratio>0)).map(m=>m.model_name).join(",")||"(无)");
console.log("异常：quota_type=1 但 model_price<=0 =>", rows.filter(m=>m.quota_type===1 && !(m.model_price>0)).map(m=>m.model_name).join(",")||"(无)");
'
