-- GPT-6 补全 + GPT-6.2 两档上架（2026-11-28）
--
-- 上游实测（sub2api 0.2.21 直连）：
--   gpt-6 ✓   gpt-6.2-sol ✓   gpt-6.2-astra ✓   gpt-6-sol ✓
--   gpt-6-cyber ✗  gpt-6-rosalind ✗（账号 model_not_found，官网有价但号池不供）
--
-- 定价：developers.openai.com/api/docs/pricing（Standard，2026-11-28 实查）
--   gpt-6.2-sol    $2.00 in / $10.00 out per 1M → ratio 1.00  completion_ratio 5
--   gpt-6.2-astra  $8.00 in / $40.00 out per 1M → ratio 4.00  completion_ratio 5
--   gpt-6          官网无单独条目 → 取 sol 档 $2/$10（与 gpt-6-sol 同价，sub2api 自家
--                  fallback 亦为 $2/$10）→ ratio 1.00  completion_ratio 5
BEGIN;

UPDATE channels
SET models = 'gpt-6,gpt-6-astra,gpt-6-sol,gpt-6-luna,gpt-6.1-sol,gpt-6.1-terra,'
             'gpt-6.2-sol,gpt-6.2-astra,'
             'gpt-image-2.6-flare,gpt-image-2.6-sunburst,gpt-image-2.5-flare,gpt-image-2.5-sunburst'
WHERE id = 13;

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag) VALUES
  ('default', 'gpt-6',        13, true, 1, 0, NULL),
  ('default', 'gpt-6.2-sol',  13, true, 1, 0, NULL),
  ('default', 'gpt-6.2-astra', 13, true, 1, 0, NULL)
ON CONFLICT ("group", model, channel_id) DO UPDATE SET enabled = true, priority = 1;

UPDATE options SET value = (
  value::jsonb
  || jsonb_build_object(
       'gpt-6',         1.0,
       'gpt-6.2-sol',   1.0,
       'gpt-6.2-astra', 4.0
     )
)::text
WHERE key = 'ModelRatio';

UPDATE options SET value = (
  value::jsonb
  || jsonb_build_object(
       'gpt-6',         5.0,
       'gpt-6.2-sol',   5.0,
       'gpt-6.2-astra', 5.0
     )
)::text
WHERE key = 'CompletionRatio';

UPDATE models SET status = 1, tags = 'text', updated_time = EXTRACT(EPOCH FROM now())::bigint
WHERE model_name IN ('gpt-6', 'gpt-6.2-sol', 'gpt-6.2-astra');

UPDATE models SET description = 'GPT-6 通用文本模型，按 sol 档定价 $2/1M 输入、$10/1M 输出。'
WHERE model_name = 'gpt-6';

INSERT INTO models (model_name, description, tags, status, updated_time)
SELECT v.model_name, v.description, 'text', 1, EXTRACT(EPOCH FROM now())::bigint
FROM (VALUES
  ('gpt-6.2-sol',   'GPT-6.2 Sol 旗舰档，官网 $2/1M 输入、$10/1M 输出。'),
  ('gpt-6.2-astra', 'GPT-6.2 Astra 高容量档，官网 $8/1M 输入、$40/1M 输出。')
) AS v(model_name, description)
WHERE NOT EXISTS (SELECT 1 FROM models m WHERE m.model_name = v.model_name);

COMMIT;

SELECT model, enabled FROM abilities WHERE channel_id = 13 AND model LIKE 'gpt-6%' ORDER BY model;
SELECT model_name, status FROM models WHERE model_name LIKE 'gpt-6%' ORDER BY model_name;
