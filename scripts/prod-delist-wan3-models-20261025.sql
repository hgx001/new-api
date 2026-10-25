-- new-api 生产库：下架 3 个模型（2026-10-25 老板指令）
--   wan3.0-1080p-慢速  ← 渠道 23「三元巡狩」
--   wan2.7-r2v         ← 渠道 17「有赞 Wan3」
--   wan3.0-480p        ← 渠道 22「二春Erchun-Wan3-480p」
--
-- 执行前逐项核实（2026-10-25 12:1x 本次查询）：
--   · 近 7 天调用量：三个都是 0（历史总量 1080p-慢速 1 / r2v 50 / 480p 2，最后一次 10-14）
--   · 在途任务：tasks 里 properties->>'model' 命中 0 条（无卡单，下架不影响任何轮询）
--   · tokens.model_limits：无任何 token 引用这三个名字
--   · 每个名字只挂在一个渠道上，且都不共享：
--       渠道 17 = wan3.0-smart + wan2.7-r2v   → 摘掉 r2v 后仍有 wan3.0-smart，渠道保留
--       渠道 23 = wan3.0-1080p-慢速 + wan3.0-video-prime-1080p → 摘掉后仍有 prime，渠道保留
--       渠道 22 = 只有 wan3.0-480p            → 摘掉后渠道为空，**同时置为禁用**（见 ⑥）
--
-- 为什么必须动 channels.models 而不只是删 abilities：
--   model/ability.go 的 FixAbility()（controller/channel.go 的「修复能力」按钮，
--   以及 main.go 启动时 InitChannelCache panic 的兜底）会 TRUNCATE abilities
--   再按 channels.models 重建。只删 abilities 而不摘 channels.models，
--   一次修复就会把这三个模型「复活」。所以摘 channels.models 是唯一持久的下架。
--
-- 代码侧**不动**（这些名字是 adaptor 的内部常量，不是可下架的商品名）：
--   relay/channel/task/sanyuan/constants.go  SlowPublicModel
--   relay/channel/task/erchun/constants.go   PublicModel
--   relay/channel/task/youzanwan3/adaptor.go 模型表
--   model/pricing.go 的 isAllowedPricingModel / setting/ratio_setting/model_ratio.go
--   这些保留是安全的：没有 ability 就没有路由，白名单/倍率不会被走到。
--   真要彻底删干净得连 adaptor 一起重构（erchun 的整个 adaptor 只服务 wan3.0-480p），
--   不属于「下架」范围。
--
-- 客户可见文档已同步（前端 tutorials/content-models.ts）：移除这两个有文档的模型
-- （wan3.0-480p 本来就没写进文档）。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_delist_wan3_20261025_channels AS
  SELECT * FROM channels WHERE id IN (17, 22, 23);
CREATE TABLE IF NOT EXISTS _bak_delist_wan3_20261025_abilities AS
  SELECT * FROM abilities
   WHERE model IN ('wan3.0-1080p-慢速', 'wan2.7-r2v', 'wan3.0-480p');
CREATE TABLE IF NOT EXISTS _bak_delist_wan3_20261025_models AS
  SELECT * FROM models
   WHERE model_name IN ('wan3.0-1080p-慢速', 'wan2.7-r2v', 'wan3.0-480p');
CREATE TABLE IF NOT EXISTS _bak_delist_wan3_20261025_options AS
  SELECT * FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');

-- ---------- ① 从渠道模型列表里摘掉（按名字逐项 array_remove，不整体覆写）----------
UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'wan3.0-1080p-慢速'), ',')
 WHERE models ~ '(^|,)wan3\.0-1080p-慢速(,|$)';

UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'wan2.7-r2v'), ',')
 WHERE models ~ '(^|,)wan2\.7-r2v(,|$)';

UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'wan3.0-480p'), ',')
 WHERE models ~ '(^|,)wan3\.0-480p(,|$)';

-- ---------- ② 删能力行（否则这三个名字仍可路由）----------
DELETE FROM abilities
 WHERE model IN ('wan3.0-1080p-慢速', 'wan2.7-r2v', 'wan3.0-480p');

-- ---------- ③ 删广场卡片（备份表里有原文，可原样恢复）----------
DELETE FROM models
 WHERE model_name IN ('wan3.0-1080p-慢速', 'wan2.7-r2v', 'wan3.0-480p');

-- ---------- ④ 摘价格键 ----------
-- ModelPrice 是这三个模型的计费来源（quota_type=1 按次/按秒）；
-- ModelRatio / CompletionRatio 目前不含这三个键，一并防御性摘除。
UPDATE options
   SET value = (value::jsonb - 'wan3.0-1080p-慢速' - 'wan2.7-r2v' - 'wan3.0-480p')::text
 WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND (value::jsonb ? 'wan3.0-1080p-慢速'
        OR value::jsonb ? 'wan2.7-r2v'
        OR value::jsonb ? 'wan3.0-480p');

-- ---------- ⑤ 渠道 22 已被摘空 → 置为「手动禁用」----------
-- 渠道 22 只服务 wan3.0-480p，摘完 models 变成空串。留着 status=1 的空渠道
-- 没有意义，而且空串是 model/ability.go 里一个已知坑：UpdateAbilities /
-- AddAbilities 都是 strings.Split(models, ",") 不做空值过滤，空串会生成一条
-- model='' 的垃圾能力行。置为禁用后即便有人误存该渠道，生成的也是 enabled=false。
UPDATE channels
   SET status = 2
 WHERE id = 22
   AND status = 1
   AND COALESCE(models, '') = '';

COMMIT;

-- ---------- 复核（事务外只读，脚本跑完必须逐项核对）----------
\echo '--- ① 三个渠道现状（17/23 应各剩原有模型，22 应为空且 status=2）---'
SELECT id, name, status, models FROM channels WHERE id IN (17, 22, 23) ORDER BY id;

\echo '--- ② 能力表：三个名字必须 0 行 ---'
SELECT model, channel_id, "group", enabled
  FROM abilities
 WHERE model IN ('wan3.0-1080p-慢速', 'wan2.7-r2v', 'wan3.0-480p');

\echo '--- ③ 广场卡片：必须 0 行 ---'
SELECT id, model_name, status FROM models
 WHERE model_name IN ('wan3.0-1080p-慢速', 'wan2.7-r2v', 'wan3.0-480p');

\echo '--- ④ 价格键：必须 0 行 ---'
SELECT o.key AS opt_key, e.key AS model_key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND e.key IN ('wan3.0-1080p-慢速', 'wan2.7-r2v', 'wan3.0-480p');

\echo '--- ⑤ 空 model 名的垃圾能力行：必须 0 行 ---'
SELECT count(*) AS empty_model_abilities FROM abilities WHERE COALESCE(model, '') = '';

\echo '--- ⑥ 渠道 17 / 23 的剩余能力（确认没被连带删掉）---'
SELECT channel_id, model, enabled FROM abilities WHERE channel_id IN (17, 23) ORDER BY channel_id, model;


-- =====================================================================
-- 回滚（整体恢复到本脚本执行前）
-- =====================================================================
--
-- BEGIN;
-- -- 渠道（含 status 与 models 原文）
-- UPDATE channels c
--    SET models = b.models, status = b.status
--   FROM _bak_delist_wan3_20261025_channels b
--  WHERE c.id = b.id;
-- -- 能力行
-- INSERT INTO abilities SELECT * FROM _bak_delist_wan3_20261025_abilities
--   ON CONFLICT DO NOTHING;
-- -- 广场卡片
-- INSERT INTO models SELECT * FROM _bak_delist_wan3_20261025_models
--   ON CONFLICT (id) DO NOTHING;
-- -- 价格键（整份 options 还原，注意会覆盖期间其它调价，优先用 jsonb_set 逐键补）
-- UPDATE options o
--    SET value = b.value
--   FROM _bak_delist_wan3_20261025_options b
--  WHERE o.key = b.key
--    AND b.key IN ('ModelRatio', 'CompletionRatio');
-- UPDATE options
--    SET value = jsonb_set(
--          jsonb_set(
--            jsonb_set(value::jsonb, '{wan3.0-1080p-慢速}', to_jsonb(2.0 / 7.3::numeric)),
--            '{wan2.7-r2v}', to_jsonb(0.1 / 7.3::numeric)),
--          '{wan3.0-480p}', to_jsonb(0.28 / 7.3::numeric))::text
--  WHERE key = 'ModelPrice';
-- COMMIT;
--
-- ⚠️ 前端教程文案是另一次发版（commit）改的，回滚数据不会把文档带回来，
--    要恢复文档得 revert 那个 commit 并重新发前端。
