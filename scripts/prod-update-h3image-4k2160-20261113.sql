-- =============================================================================
-- h3-image 4K 判定扩展（2026-11-13 15:0x）：把 3840×2160 也纳入 4K 档
-- 接 scripts/prod-update-h3image-tiers-20261113.sql 之后执行。
--
-- 用户口径：「3840×2160 这个也算 4k，改下」。
-- 代码侧（commit 见部署 SHA）已把 3840x2160 加入 isH3Image4KSize，
-- 且显式像素尺寸**保留宽高比**（只把中文乘号换成 ASCII x），
-- 不会被改写成 4096x4096（否则 16:9 会被压成 1:1）。
--
-- 前置：双机已发版到含该改动的同一 SHA。本脚本只改描述，**不动价格**。
-- ⚠️ 两台生产库各自独立，老机与新机各执行一次。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

CREATE TABLE IF NOT EXISTS _bak_models_desc_20261113_4k2160 AS
  SELECT id, model_name, description, updated_time
    FROM models WHERE model_name = 'h3-image' AND deleted_at IS NULL;

UPDATE models
   SET description = 'MiniMax H3-Image 图片生成（文生图 / 图生图同一模型）。同步 POST /v1/images/generations，返回图片直链；只给 prompt 即文生图，附带 image 参考图（data URI）即自动按图生图处理，由服务端自行判断，无需切换模型。计费 ¥0.05/张；显式指定 4K（size=4k、4096×4096 或 3840×2160）按 ¥0.10/张；n 张按倍数累计。6 步蒸馏采样，默认出 2K 级静帧，原生最高 4096×4096。⚠️ H3-Image 没有负向提示词输入，negative_prompt / steps / cfg 会被忽略。',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'h3-image'
   AND deleted_at IS NULL;

\echo '=== 新描述（应含 3840×2160）==='
SELECT model_name,
       description LIKE '%3840×2160%' AS has_3840,
       left(description, 110) AS desc_head
  FROM models WHERE model_name = 'h3-image' AND deleted_at IS NULL;

\echo '=== 基础价必须仍是 0.00684931506849315 ==='
SELECT value::jsonb ->> 'h3-image' AS h3_image_base_price
  FROM options WHERE key = 'ModelPrice';

COMMIT;

-- =============================================================================
-- 回滚：
--   UPDATE models m SET description = b.description, updated_time = b.updated_time
--     FROM _bak_models_desc_20261113_4k2160 b
--    WHERE m.id = b.id AND m.model_name = 'h3-image';
-- =============================================================================
