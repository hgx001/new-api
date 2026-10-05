// 模型广场卡片价格 vs 实际扣费：逐行实测对账
// 输入：.tmp-rows.tsv（生产 logs 明细）、线上 /api/pricing
import fs from 'fs';

const QPU = 500000; // common.QuotaPerUnit
const USD2RMB = 7.3; // ratio_setting.USD2RMB
const yuan = (quota) => (quota / QPU) * USD2RMB;

const live = await (await fetch('https://api.heibaidao.cn/api/pricing')).json();
const price = new Map(live.data.map((m) => [m.model_name, m]));
const gr = live.group_ratio || {};

const rows = fs
  .readFileSync('.tmp-rows.tsv', 'utf8')
  .split('\n')
  .filter(Boolean)
  .map((l) => {
    const [model, ts, quota, group, content] = l.split('|');
    return { model, ts, quota: Number(quota), group, content: content || '' };
  });

const num = (s, re) => {
  const m = s.match(re);
  return m ? Number(m[1]) : null;
};

// 后端倍率表（与代码常量一致）
const SMART = { '480P': 1.0, '720P': 0.32 / 0.28, '1080P': 0.40 / 0.28 };
const H3RES = { '480p': 1.0, '768p': 1.2 };

const byModel = new Map();
for (const r of rows) {
  if (!byModel.has(r.model)) byModel.set(r.model, []);
  byModel.get(r.model).push(r);
}

const verdict = [];
for (const [model, rs] of [...byModel].sort((a, b) => a[0].localeCompare(b[0]))) {
  const m = price.get(model);
  const mp = m?.model_price ?? 0;
  const mr = m?.model_ratio ?? 0;
  const cr = m?.completion_ratio ?? 1;
  const perCall = mp * QPU;
  const lines = [];
  let mismatched = 0;
  let worst = null;

  for (const r of rs) {
    const g = gr[r.group] ?? 1;
    let expect = null;
    let label = '';
    const seconds = num(r.content, /seconds:\s*([\d.]+)/);
    const size = num(r.content, /size:\s*([\d.]+)/);
    if (seconds !== null) {
      const ratio = size ?? 1;
      expect = perCall * g * seconds * ratio;
      label = `×${seconds}s` + (size ? `×${size}` : '');
    } else if (m && m.quota_type === 0) {
      const n = num(r.content, /生成数量\s*(\d+)/);
      expect = null; // token 模型明细另算
      label = 'token';
    } else {
      expect = perCall * g;
      label = '按次';
    }
    if (expect === null) continue;
    const diff = Math.abs(Math.floor(expect) - r.quota);
    const ok = diff <= 2; // int() 截断误差
    if (!ok) {
      mismatched++;
      if (!worst || diff > worst.diff) worst = { ts: r.ts, quota: r.quota, expect: Math.floor(expect), diff, content: r.content };
    }
    if (lines.length < 3) lines.push(`    ${r.ts}  实付 ${r.quota} (¥${yuan(r.quota).toFixed(4)})  期望 ${Math.floor(expect)}  ${ok ? 'OK' : '✗'}  ${label}`);
  }
  verdict.push({ model, m, n: rs.length, mismatched, lines, worst });
}

console.log('模型'.padEnd(30), '次数'.padStart(5), '不一致'.padStart(7), ' 说明');
console.log('-'.repeat(120));
for (const v of verdict) {
  const flag = v.mismatched ? '✗ 有行对不上' : '✓ 全部一致';
  console.log(v.model.padEnd(30), String(v.n).padStart(5), String(v.mismatched).padStart(7), ` ${flag}`);
  for (const l of v.lines) console.log(l);
  if (v.worst)
    console.log(
      `    ↳ 最大偏差 ${v.worst.ts} 实付 ${v.worst.quota} vs 期望 ${v.worst.expect} | ${v.worst.content.slice(0, 110)}`
    );
}
