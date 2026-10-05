// 模型广场卡片价格 vs 实际扣费：逐模型对齐
// 卡片展示规则来自 web/default/src/features/pricing/lib/price.ts + lib/model-helpers.ts
// 实际计费倍率来自各 adaptor 的 EstimateBilling
const USD2RMB = 7.3;

const res = await fetch('https://api.heibaidao.cn/api/pricing');
const d = await res.json();
const rows = d.data || [];
const groupRatio = d.group_ratio || {};

// 与前端 isPerSecondModel() 完全一致的判定
const isPerSecond = (n) => {
  if (n === 'wan3.0-video-prime-1080p') return false;
  return (
    n.startsWith('wan3.0-video') ||
    n.startsWith('wan3.0-smart') ||
    n.startsWith('wan2.7-r2v') ||
    n.startsWith('autodl:')
  );
};
const minRatio = (m) => {
  const gs = Array.isArray(m.enable_groups) ? m.enable_groups : [];
  if (!gs.length) return 1;
  let r = Infinity;
  for (const g of gs) if (groupRatio[g] !== undefined && groupRatio[g] < r) r = groupRatio[g];
  return r === Infinity ? 1 : r;
};

// 后端 EstimateBilling 倍率分类（按 adaptor 实现归纳）
function multiplier(n) {
  if (n === 'Nano Banana Pro' || n === 'manwu-image')
    return { kind: 'xN', note: '图片 n>1 时 ×张数' };
  if (n.startsWith('autodl:') || n.startsWith('hailuo-h3-'))
    return { kind: 'sec*size', note: '×秒 ×分辨率' };
  if (n === 'aliyun:wan-3.0')
    return { kind: 'sec*size', note: '×秒 ×分辨率(ProcessAliOtherRatios)' };
  if (n === 'wan3.0-video' || n === 'wan3.0-video-官网' || n === 'wan3.0-smart' || n === 'wan2.7-r2v')
    return { kind: 'sec*size', note: '×秒 ×分辨率' };
  if (n.startsWith('wan3.0-video-prime'))
    return { kind: 'none', note: 'youzanwan3 isPrimeModel → nil 按次' };
  if (n.startsWith('seedance2'))
    return { kind: 'cond', note: '仅 video_input≠1 时 ×ratio' };
  if (n.startsWith('seedance-2') || n === 'gemini-web-video' || n === 'jimeng-video-reverse')
    return { kind: 'none', note: 'manwu → nil 按次' };
  return { kind: 'none', note: '' };
}

const w = (s, n) => {
  s = String(s);
  let width = 0;
  for (const ch of s) width += ch.charCodeAt(0) > 255 ? 2 : 1;
  return s + ' '.repeat(Math.max(0, n - width));
};

console.log('模型总数:', rows.length, ' group_ratio =', JSON.stringify(groupRatio));
console.log('（卡片价格取该模型可用分组中的最小 ratio；vip=0.5 意味着卡片按 5 折展示）\n');
console.log(
  [w('模型', 32), w('类型', 5), w('卡片单位', 8), w('卡片价¥', 9), w('default实付¥', 12), w('实际倍率', 9), '备注'].join(' ')
);
console.log('-'.repeat(118));

const issues = [];
for (const m of [...rows].sort((a, b) => a.model_name.localeCompare(b.model_name))) {
  const isReq = m.quota_type === 1;
  const r = minRatio(m);
  let card, realDefault, unit, mult;
  if (isReq) {
    const price = m.model_price || 0;
    card = price * r * USD2RMB;
    realDefault = price * (groupRatio['default'] ?? 1) * USD2RMB;
    unit = isPerSecond(m.model_name) ? '/秒' : '/次';
    mult = multiplier(m.model_name);
  } else {
    card = (m.model_ratio || 0) * 2 * r * USD2RMB;
    realDefault = (m.model_ratio || 0) * 2 * (groupRatio['default'] ?? 1) * USD2RMB;
    unit = '/1M入';
    mult = { kind: 'token', note: `出×${m.completion_ratio ?? 1}，cache×${m.cache_ratio ?? '-'}` };
  }
  console.log(
    [w(m.model_name, 32), w(isReq ? '按次' : 'token', 5), w(unit, 8), w(card.toFixed(4), 9),
     w(realDefault.toFixed(4), 12), w(mult.kind, 9), mult.note].join(' ')
  );

  const labelSaysSecond = unit === '/秒';
  if (isReq && mult.kind === 'sec*size' && !labelSaysSecond)
    issues.push(`${m.model_name}：实际按「×秒 ×分辨率」扣费，卡片单位却是 ${unit}`);
  if (isReq && mult.kind !== 'sec*size' && mult.kind !== 'token' && labelSaysSecond)
    issues.push(`${m.model_name}：卡片单位是 ${unit}，但 EstimateBilling 返回 nil（实际按次）`);
  if (isReq && mult.kind === 'xN')
    issues.push(`${m.model_name}：卡片标 ${unit} ¥${realDefault.toFixed(2)}，但 n>1 时实付 ×n`);
  if (Math.abs(card - realDefault) > 1e-9)
    issues.push(`${m.model_name}：卡片 ¥${card.toFixed(4)}（min ratio ${r}）≠ default 组实付 ¥${realDefault.toFixed(4)}`);
}
console.log('\n=== 可疑项 ===');
console.log(issues.length ? issues.map((s) => ' * ' + s).join('\n') : ' * 无');
