-- =============================================================================
-- 修正 wan3.0-720p-按次 的对外文案：改为「原生 720P + 固定 30 秒」
-- 日期：2026-11-01
--
-- 背景：上架时（prod-add-xinbiapi-ppc-20261101.sql）卡片写的是
--       「固定输出 720P，时长 2-30 秒」，与最终定稿不符：
--         · 时长**固定 30 秒**（代码已改为传其它值直接 400）
--         · 对外口径是**原生 720P**，不得出现交付链路的实现细节
--         · 参考素材只承诺**参考图**：上游 schema 虽支持 reference_video /
--           reference_audio，但我们的兼容提交口只透传 reference_images；
--           且参考视频在上游是**按秒加收**（最多 15 秒），会直接击穿按次定价。
--
-- 幂等：直接按 model_name 覆盖 description / remark，重复执行结果一致。
--       脚本自带备份表，回滚见文末。
--
-- 执行：psql -U root -d new-api -f 本文件
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_models_ppc_card_20261101 AS
  SELECT model_name, description, updated_time
    FROM models
   WHERE model_name = 'wan3.0-720p-按次' AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS _bak_channels_ppc_remark_20261101 AS
  SELECT id, remark FROM channels WHERE type = 68;

-- ---------- 1. 模型广场卡片 ----------
UPDATE models
   SET description = 'Wan3.0 官方模型，原生 720P 输出，固定时长 30 秒。支持文生视频、图生视频与多张参考图（≤9 张），6 档画面比例（自适应 / 16:9 / 9:16 / 1:1 / 4:3 / 3:4）。特点：按次计费 ¥4.50/次，不随时长与分辨率变化；支持稳定高并发，适合批量出片。⚠️ 不支持负向提示词（negativePrompt）与 seed 参数。',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'wan3.0-720p-按次'
   AND deleted_at IS NULL;

-- ---------- 2. 渠道备注（后台可见，同样不能写实现细节）----------
UPDATE channels
   SET remark = 'Wan3.0 原生 720P，固定 30 秒；按次 ¥4.5/次'
 WHERE type = 68;

-- ---------- 校验 ----------
\echo '=== 卡片（应含「原生 720P」「固定时长 30 秒」，不应含「超分」「提升」）==='
SELECT model_name,
       description LIKE '%原生 720P%'   AS ok_native_720p,
       description LIKE '%固定时长 30 秒%' AS ok_fixed_30s,
       description LIKE '%超分%'          AS bad_sr,
       description LIKE '%提升%'          AS bad_upscale
  FROM models
 WHERE model_name = 'wan3.0-720p-按次' AND deleted_at IS NULL;

\echo '=== 渠道备注 ==='
SELECT id, type, name, remark FROM channels WHERE type = 68;

COMMIT;

-- =============================================================================
-- 回滚（按需手执行）：
--   UPDATE models m SET description = b.description, updated_time = b.updated_time
--     FROM _bak_models_ppc_card_20261101 b
--    WHERE m.model_name = b.model_name AND m.deleted_at IS NULL;
--   UPDATE channels c SET remark = b.remark
--     FROM _bak_channels_ppc_remark_20261101 b WHERE c.id = b.id;
-- ⚠️ 改完要等 ~60 秒配置缓存过期才生效。
-- =============================================================================
