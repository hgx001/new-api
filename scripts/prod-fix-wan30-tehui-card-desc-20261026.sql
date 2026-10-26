-- 生产：wan-3.0-特惠 广场卡片描述去掉「（Picsart 特惠档）」（2026-10-26）
--
-- 原因：卡片描述是客户可见文案，暴露上游站点名等于告诉客户我们用谁的免费档，
--       不需要对外披露；其余描述（档位、价格、比例、素材上限、出片时长）不变。
--
-- 只改 models 表这一张卡片的 description，不动 ModelPrice / abilities / 渠道配置。
-- 生效：/api/pricing 的广场卡片文案走 models 表，改后 ≤60s（pricing 缓存 TTL）可见。

\set ON_ERROR_STOP on

BEGIN;

DROP TABLE IF EXISTS _bak_wan30_tehui_card_20261026;
CREATE TABLE _bak_wan30_tehui_card_20261026 AS
SELECT id, model_name, description, now() AS backup_at
FROM models WHERE model_name = 'wan-3.0-特惠';

UPDATE models
SET description = 'Wan 3.0 视频生成。固定 30 秒一条：480P ¥1.50/次、720P ¥2.00/次（按请求的分辨率自动计价，未指定时按 720P）。支持 6 档比例（含自适应），≤10 张参考图，不收参考视频。⚠️ 官网串行出片，单条通常 15–25 分钟，急单请选其它模型',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'wan-3.0-特惠';

COMMIT;

-- 复核
SELECT id, model_name, description FROM models WHERE model_name = 'wan-3.0-特惠';
SELECT count(*) AS cards_still_mentioning_picsart
FROM models WHERE description ILIKE '%Picsart%';

-- ============================ 回滚 SQL ============================
--   UPDATE models SET description = (SELECT description FROM _bak_wan30_tehui_card_20261026)
--     WHERE model_name = 'wan-3.0-特惠';
-- ============================================================================
