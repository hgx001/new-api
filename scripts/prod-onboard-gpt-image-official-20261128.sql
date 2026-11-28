-- GPT 官方图片模型上架（2026-11-28，号池升 0.2.21 + SUB2API_IMAGES_MAIN_MODEL=gpt-5.6-luna 后真机打通）
--
-- 上游实测（sub2api 直连，四个模型名各跑一次，全部 200 + PNG）：
--   gpt-image-2.6-flare / gpt-image-2.6-sunburst / gpt-image-2.5-flare / gpt-image-2.5-sunburst
-- 官方真名，客户侧原样展示（用户 2026-11-28 定：按真实图片名称命名）。
-- 定价：¥0.1/次 固定价 → ModelPrice = 0.1 ÷ 7.3 = 0.013698630136986301
-- 旧名 gpt-image-2.5-官方 已失效（映射到 gpt-image-2，Codex 链路驱动模型写死导致必失败），一并清除。
BEGIN;

-- 1) 渠道 13 模型清单：Text 6.x 保持，图片换成四个真实名
UPDATE channels
SET models = 'gpt-6-astra,gpt-6-sol,gpt-6-luna,gpt-6.1-sol,gpt-6.1-terra,'
             'gpt-image-2.6-flare,gpt-image-2.6-sunburst,gpt-image-2.5-flare,gpt-image-2.5-sunburst'
WHERE id = 13;

-- 2) m3only 图片通道 29 同步（原克隆的 gpt-image-2.5-官方 已删）
UPDATE channels
SET models = 'gpt-image-2.6-flare,gpt-image-2.6-sunburst,gpt-image-2.5-flare,gpt-image-2.5-sunburst'
WHERE id = 29;

-- 3) 路由能力表
DELETE FROM abilities WHERE channel_id IN (13, 29) AND model = 'gpt-image-2.5-官方';

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag) VALUES
  ('default', 'gpt-image-2.6-flare',    13, true, 1, 0, NULL),
  ('default', 'gpt-image-2.6-sunburst', 13, true, 1, 0, NULL),
  ('default', 'gpt-image-2.5-flare',    13, true, 1, 0, NULL),
  ('default', 'gpt-image-2.5-sunburst', 13, true, 1, 0, NULL),
  ('m3only',  'gpt-image-2.6-flare',    29, true, 1, 0, NULL),
  ('m3only',  'gpt-image-2.6-sunburst', 29, true, 1, 0, NULL),
  ('m3only',  'gpt-image-2.5-flare',    29, true, 1, 0, NULL),
  ('m3only',  'gpt-image-2.5-sunburst', 29, true, 1, 0, NULL)
ON CONFLICT ("group", model, channel_id) DO UPDATE SET enabled = true, priority = 1;

-- 4) 固定价：¥0.1/次
UPDATE options SET value = (
  value::jsonb
  || jsonb_build_object(
       'gpt-image-2.6-flare',    0.013698630136986301,
       'gpt-image-2.6-sunburst', 0.013698630136986301,
       'gpt-image-2.5-flare',    0.013698630136986301,
       'gpt-image-2.5-sunburst', 0.013698630136986301
     )
)::text
WHERE key = 'ModelPrice';

-- 5) 广场元数据（models 表主键是 id，model_name 无唯一约束 → UPDATE 后按 NOT EXISTS 补）
UPDATE models SET status = 1, tags = 'image', updated_time = EXTRACT(EPOCH FROM now())::bigint
WHERE model_name IN ('gpt-image-2.6-flare', 'gpt-image-2.6-sunburst',
                     'gpt-image-2.5-flare', 'gpt-image-2.5-sunburst');

INSERT INTO models (model_name, description, tags, status, updated_time)
SELECT v.model_name, v.description, 'image', 1, EXTRACT(EPOCH FROM now())::bigint
FROM (VALUES
  ('gpt-image-2.6-flare',    'GPT Image 2.6 Flare：偏写实与光影渲染的图片模型，按张计费 ¥0.1/张。支持文生图与参考图编辑，返回 PNG。'),
  ('gpt-image-2.6-sunburst', 'GPT Image 2.6 Sunburst：偏插画与色彩表现的图片模型，按张计费 ¥0.1/张。支持文生图与参考图编辑，返回 PNG。'),
  ('gpt-image-2.5-flare',    'GPT Image 2.5 Flare：上一代写实向图片模型，按张计费 ¥0.1/张。支持文生图与参考图编辑，返回 PNG。'),
  ('gpt-image-2.5-sunburst', 'GPT Image 2.5 Sunburst：上一代插画向图片模型，按张计费 ¥0.1/张。支持文生图与参考图编辑，返回 PNG。')
) AS v(model_name, description)
WHERE NOT EXISTS (SELECT 1 FROM models m WHERE m.model_name = v.model_name);

COMMIT;

SELECT model, "group", enabled FROM abilities WHERE channel_id IN (13,29) AND model LIKE 'gpt-image%' ORDER BY channel_id, model;
SELECT model_name, status FROM models WHERE model_name LIKE 'gpt-image-2.%' ORDER BY model_name;
