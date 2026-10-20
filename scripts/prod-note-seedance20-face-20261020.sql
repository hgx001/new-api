-- =============================================================================
-- 给模型广场 seedance-2.0 卡片备注追加「卡人脸」
-- 日期：2026-10-20
-- 执行顺序：无需发版，直接在生产库执行即可（备注只读 models 表）
--
-- 背景：
--   models.description 就是模型广场卡片上显示的那段说明（/api/pricing 的 desc 字段）。
--   本次只在原有文案末尾追加「，卡人脸」三个字，其余内容一字不动。
--
-- 前置状态（执行前生产库实测）：
--   Seedance 2.0 文/图生视频，5/10/15 秒，6 档比例，≤10 张参考图
--
-- ⚠️ 注意：scripts/prod-scrub-model-descriptions-20261013.sql 里也硬编码了这一行
--   的原文案。如果以后有人重跑那个 scrub 脚本，会把本次追加的备注冲掉。
--   重跑前请同步修改该脚本第 28 行。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- 备份原行（幂等：已存在则不重复建）
CREATE TABLE IF NOT EXISTS _bak_models_seedance20_face_20261020 AS
SELECT * FROM models WHERE model_name = 'seedance-2.0';

-- 只在「已存在该模型」且「尚未追加过」时更新，避免重复执行叠加成「卡人脸，卡人脸」
UPDATE models
   SET description = description || '，卡人脸',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'seedance-2.0'
   AND description NOT LIKE '%卡人脸%';

-- 结果校验：必须恰好一行，且以「卡人脸」结尾
SELECT model_name,
       description,
       (description LIKE '%卡人脸') AS note_ok
  FROM models
 WHERE model_name = 'seedance-2.0';

COMMIT;
