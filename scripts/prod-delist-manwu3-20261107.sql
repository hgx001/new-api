-- new-api 生产库：下架 3 个模型（2026-11-07 老板指令）
--   gemini-web-video  ← 渠道 20「漫屋ArcReel」(type 66)
--   wan2.7-图生        ← 渠道 20「漫屋ArcReel」
--   wan2.7-编辑        ← 渠道 20「漫屋ArcReel」
--
-- 执行前逐项核实（2026-11-07 02:2x 生产库只读查询）：
--   · 三个名字只挂渠道 20；渠道 20 共 10 个模型，摘掉这 3 个后仍剩 7 个
--     （Nano Banana Pro / Seedream 4.5 / Seedream 5.0 Flash / jimeng-video-reverse /
--       seedance-2-0-fast / seedance-2-5 / seedance-2.0）→ **渠道 20 保留，不禁用**
--   · abilities：3 行（default 组 / channel_id=20 / enabled=true）
--   · models 广场卡片：3 行（id 36 gemini-web-video、49 wan2.7-图生、50 wan2.7-编辑）
--   · 价格表：只有 ModelPrice 有这 3 个键（按次计费 ¥1.0 / ¥0.30 / ¥0.30 的 USD 等值）；
--     ModelRatio / CompletionRatio 不含这三个键（防御性一并摘除）
--   · 在途任务 0：tasks 表只有 SUCCESS/FAILURE 两种终态，无 pending
--   · 最近 7 天调用：gemini-web-video 2 / wan2.7-图生 2 / wan2.7-编辑 1；最后一次 2026-11-05
--   · tokens.model_limits、channels.model_mapping 均不含这三个名字
--
-- 为什么必须动 channels.models 而不只是删 abilities：
--   model/ability.go 的 FixAbility()（管理端「修复能力」按钮、main.go 启动兜底）
--   会按 channels.models 重建 abilities。只删 abilities，一次修复就把这三个模型复活。
--
-- 代码侧**不动**（沿用 2026-10-25 同类下架口径）：
--   model/pricing.go 的 isAllowedPricingModel / setting/ratio_setting/model_ratio.go /
--   relay/channel/task/manwu/adaptor.go 的模型常量
--   ——没有 ability 就没有路由，白名单/倍率不会被走到；真删干净要连 adaptor 重构，
--   不属于「下架」范围。定价列表来自 enableAbilities（model/pricing.go updatePricing），
--   删掉 ability 即从 /api/pricing 消失。
--
-- 客户可见文档另一次前端发版同步（web/default/src/features/tutorials/content-models.ts）。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_delist_manwu3_20261107_channels AS
  SELECT * FROM channels WHERE id = 20;
CREATE TABLE IF NOT EXISTS _bak_delist_manwu3_20261107_abilities AS
  SELECT * FROM abilities
   WHERE model IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑');
CREATE TABLE IF NOT EXISTS _bak_delist_manwu3_20261107_models AS
  SELECT * FROM models
   WHERE model_name IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑');
CREATE TABLE IF NOT EXISTS _bak_delist_manwu3_20261107_options AS
  SELECT * FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');

-- ---------- ① 从渠道模型列表里逐项摘除（不整体覆写 channels.models）----------
UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'gemini-web-video'), ',')
 WHERE models ~ '(^|,)gemini-web-video(,|$)';

UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'wan2.7-图生'), ',')
 WHERE models ~ '(^|,)wan2\.7-图生(,|$)';

UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'wan2.7-编辑'), ',')
 WHERE models ~ '(^|,)wan2\.7-编辑(,|$)';

-- ---------- ② 删能力行（否则这三个名字仍可路由）----------
DELETE FROM abilities
 WHERE model IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑');

-- ---------- ③ 删广场卡片（备份表里有原文，可原样恢复）----------
DELETE FROM models
 WHERE model_name IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑');

-- ---------- ④ 摘价格键 ----------
-- ModelPrice 是这三个模型的计费来源（quota_type 按次）；
-- ModelRatio / CompletionRatio 目前不含这三个键，一并防御性摘除。
UPDATE options
   SET value = (value::jsonb - 'gemini-web-video' - 'wan2.7-图生' - 'wan2.7-编辑')::text
 WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND (value::jsonb ? 'gemini-web-video'
        OR value::jsonb ? 'wan2.7-图生'
        OR value::jsonb ? 'wan2.7-编辑');

-- ---------- ⑤ 渠道 20 摘后仍剩 7 个模型 → 保持 status=1，不做禁用 ----------
-- （对照 2026-10-25：只有当某渠道被摘空时才置 status=2。本次不适用。）

COMMIT;

-- ---------- 复核（事务外只读，脚本跑完必须逐项核对）----------
\echo '--- ① 渠道 20 现状：status 应仍为 1，models 应剩 7 个（不含那 3 个）---'
SELECT id, name, status, models FROM channels WHERE id = 20;

\echo '--- ② 能力表：三个名字必须 0 行 ---'
SELECT model, channel_id, "group", enabled
  FROM abilities
 WHERE model IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑');

\echo '--- ③ 广场卡片：必须 0 行 ---'
SELECT id, model_name, status FROM models
 WHERE model_name IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑');

\echo '--- ④ 价格键：必须 0 行 ---'
SELECT o.key AS opt_key, e.key AS model_key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND e.key IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑');

\echo '--- ⑤ 空 model 名的垃圾能力行：必须 0 行 ---'
SELECT count(*) AS empty_model_abilities FROM abilities WHERE COALESCE(model, '') = '';

\echo '--- ⑥ 渠道 20 的剩余能力（确认 7 个模型都在且 enabled）---'
SELECT channel_id, model, enabled FROM abilities WHERE channel_id = 20 ORDER BY model;

\echo '--- ⑦ 全库残留检查（channels / abilities / models / 价格表 四处都必须 0）---'
SELECT 'channels' AS src, count(*) FROM channels
 WHERE models LIKE '%gemini-web-video%' OR models LIKE '%wan2.7-图生%' OR models LIKE '%wan2.7-编辑%'
UNION ALL
SELECT 'abilities', count(*) FROM abilities
 WHERE model IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑')
UNION ALL
SELECT 'models', count(*) FROM models
 WHERE model_name IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑')
UNION ALL
SELECT 'ModelPrice', count(*) FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key IN ('ModelPrice','ModelRatio','CompletionRatio')
   AND e.key IN ('gemini-web-video', 'wan2.7-图生', 'wan2.7-编辑');


-- =====================================================================
-- 回滚（整体恢复到本脚本执行前）
-- =====================================================================
--
-- BEGIN;
-- -- 渠道 20 的 models 原文
-- UPDATE channels c
--    SET models = b.models, status = b.status
--   FROM _bak_delist_manwu3_20261107_channels b
--  WHERE c.id = b.id;
-- -- 能力行
-- INSERT INTO abilities SELECT * FROM _bak_delist_manwu3_20261107_abilities
--   ON CONFLICT DO NOTHING;
-- -- 广场卡片
-- INSERT INTO models SELECT * FROM _bak_delist_manwu3_20261107_models
--   ON CONFLICT (id) DO NOTHING;
-- -- 价格键（优先用 jsonb_set 逐键补，避免覆盖期间其它调价）
-- UPDATE options
--    SET value = jsonb_set(
--          jsonb_set(
--            jsonb_set(value::jsonb, '{gemini-web-video}', to_jsonb(1.0 / 7.3::numeric)),
--            '{wan2.7-图生}', to_jsonb(0.3 / 7.3::numeric)),
--          '{wan2.7-编辑}', to_jsonb(0.3 / 7.3::numeric))::text
--  WHERE key = 'ModelPrice';
-- COMMIT;
--
-- ⚠️ 前端教程文案是另一次发版（commit）改的，回滚数据不会把文档带回来，
--    要恢复文档得 revert 那个 commit 并重新发前端。
