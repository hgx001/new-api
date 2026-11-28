-- 天幕号池 GPT-6 家族收口 + 官网定价（2026-11-28）
--
-- 背景
--   上游 sub2api（GPT-Pool，channel 13）实测可用的 6.x 模型：
--     gpt-6 ✓  gpt-6-astra ✓  gpt-6-sol ✓  gpt-6-luna ✓  gpt-6.1-sol ✓  gpt-6.1-terra ✓
--   不支持：gpt-6.1（裸名）、gpt-6.1-luna、gpt-6.1-pro、gpt-6.2（均 model_not_found）
--   低于 6 的 gpt-5.x 按用户要求全部下架。
--
-- 定价：developers.openai.com/api/docs/pricing（Standard 档，2026-11-28 实查）
--   gpt-6-astra     $10.00 in / $50.00 out  per 1M  → ratio 5.00  completion_ratio 5
--   gpt-6.1-sol      $2.00 in / $10.00 out  per 1M  → ratio 1.00  completion_ratio 5
--   gpt-6.1-terra    $0.50 in /  $2.50 out  per 1M  → ratio 0.25  completion_ratio 5
--   gpt-6-luna       $0.05 in /  $0.25 out  per 1M  → ratio 0.025 completion_ratio 5
--   gpt-6-sol   官网无单独条目，sol 档统一 $2 → ratio 1.00  completion_ratio 5
--   gpt-6       官网无定价 → 不上架（未登记 ratio 会回落到 37.5 = $75/1M，绝不能带错价上架）
--
-- ratio 换算口径：ratio = 输入价($/1M) ÷ 2（与代码 defaultModelRatio 里 gpt-5=0.625 ⇔ $1.25 一致）
-- 计费公式：¥ = quota ÷ 500000 × 7.3，quota = token × ratio（输出侧再乘 completion_ratio）
BEGIN;

-- 1) 渠道模型清单：去掉 5.x，补齐 6.x（图片保持现状，等图片链路修复后再定名）
UPDATE channels
SET models = 'gpt-6-astra,gpt-6-sol,gpt-6-luna,gpt-6.1-sol,gpt-6.1-terra,gpt-image-2.5-官方'
WHERE id = 13;

-- 2) 清掉 5.6 的重定向映射，避免旧名静默换档
UPDATE channels
SET model_mapping = (model_mapping::jsonb - 'gpt-5.6-sol' - 'gpt-5.6-luna')::text
WHERE id = 13;

-- 3) 路由能力表：下架 5.6，新上 gpt-6-astra / gpt-6.1-terra
DELETE FROM abilities WHERE channel_id = 13 AND model IN ('gpt-5.6-sol', 'gpt-5.6-luna');

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag) VALUES
  ('default', 'gpt-6-astra',  13, true, 1, 0, NULL),
  ('default', 'gpt-6.1-terra', 13, true, 1, 0, NULL)
ON CONFLICT ("group", model, channel_id) DO UPDATE SET enabled = true, priority = 1;

-- 3b) gpt-6 无官网定价 → 不上架（未登记 ratio 会回落到 37.5）
DELETE FROM abilities WHERE channel_id = 13 AND model = 'gpt-6';
DELETE FROM models WHERE model_name = 'gpt-6';

-- 4) ModelRatio（官网价换算）
UPDATE options SET value = (
  value::jsonb
  || jsonb_build_object(
       'gpt-6-astra',   5.0,
       'gpt-6-sol',     1.0,
       'gpt-6.1-sol',   1.0,
       'gpt-6.1-terra', 0.25,
       'gpt-6-luna',    0.025
     )
)::text
WHERE key = 'ModelRatio';

-- 5) CompletionRatio：官方 output/input 都是 5 倍
INSERT INTO options (key, value)
VALUES ('CompletionRatio', '{}')
ON CONFLICT (key) DO NOTHING;

UPDATE options SET value = (
  value::jsonb
  || jsonb_build_object(
       'gpt-6-astra',   5.0,
       'gpt-6-sol',     5.0,
       'gpt-6-luna',    5.0,
       'gpt-6.1-sol',   5.0,
       'gpt-6.1-terra', 5.0
     )
)::text
WHERE key = 'CompletionRatio';

-- 6) 广场元数据（gpt-6 不在此列：无官价不上架）：开启 6.x 展示（status=1），补描述
-- models 表主键是 id，model_name 没有唯一约束 → 只能「先 UPDATE，再按 NOT EXISTS 补 INSERT」
UPDATE models SET status = 1, updated_time = EXTRACT(EPOCH FROM now())::bigint
WHERE model_name IN ('gpt-6-astra', 'gpt-6-sol', 'gpt-6-luna', 'gpt-6.1-sol', 'gpt-6.1-terra');

UPDATE models SET description = 'GPT-6 Astra 高容量档，长上下文与复杂推理，官网 $10/1M 输入、$50/1M 输出。', tags = 'text'
WHERE model_name = 'gpt-6-astra';
UPDATE models SET description = 'GPT-6 Luna 轻量档，$0.05/1M 输入、$0.25/1M 输出，适合大批量简单任务。', tags = 'text'
WHERE model_name = 'gpt-6-luna';
UPDATE models SET description = 'GPT-6.1 Sol 旗舰档，官网 $2/1M 输入、$10/1M 输出。', tags = 'text'
WHERE model_name = 'gpt-6.1-sol';
UPDATE models SET description = 'GPT-6.1 Terra 均衡档，官网 $0.50/1M 输入、$2.50/1M 输出。', tags = 'text'
WHERE model_name = 'gpt-6.1-terra';

INSERT INTO models (model_name, description, tags, status, updated_time)
SELECT v.model_name, v.description, 'text', 1, EXTRACT(EPOCH FROM now())::bigint
FROM (VALUES
  ('gpt-6-astra',   'GPT-6 Astra 高容量档，长上下文与复杂推理，官网 $10/1M 输入、$50/1M 输出。'),
  ('gpt-6-sol',     'GPT-6 旗舰文本模型，复杂推理档。'),
  ('gpt-6-luna',    'GPT-6 Luna 轻量档，$0.05/1M 输入、$0.25/1M 输出，适合大批量简单任务。'),
  ('gpt-6.1-sol',   'GPT-6.1 Sol 旗舰档，官网 $2/1M 输入、$10/1M 输出。'),
  ('gpt-6.1-terra', 'GPT-6.1 Terra 均衡档，官网 $0.50/1M 输入、$2.50/1M 输出。')
) AS v(model_name, description)
WHERE NOT EXISTS (SELECT 1 FROM models m WHERE m.model_name = v.model_name);

COMMIT;

-- 校验
SELECT model, "group", enabled FROM abilities WHERE channel_id = 13 ORDER BY model;
SELECT model_name, status FROM models WHERE model_name LIKE 'gpt-6%' ORDER BY model_name;
