-- =============================================================================
-- ⚠️ 已被 scripts/prod-update-h3image-4k2160-20261113.sql 取代（2026-11-13 15:0x）：
--    4K 判定已扩展为 size=4k / 4096x4096 / 3840x2160。重跑本脚本会把描述回退到
--    只认前两种的旧口径，请改用新脚本。
-- =============================================================================
-- h3-image 二次调整（2026-11-13 下午）：两档计费 + 描述改写 + 文生/图生同一模型
-- 接 scripts/prod-add-h3image-20261113.sql 之后执行。
--
-- 用户口径：
--   * 「出 4k 的价格是 1 毛，2 段计费」→ 普通 ¥0.05/张、显式 4K ¥0.10/张
--   * 「描述去掉这个：GPU 服务器本地推理）」→ 卡片描述删掉该字样
--   * 「文生和图生弄成一个模型，服务器内部自己判断」→ 对外仍只有 h3-image，
--     带 image 参考图即图生图，网关 build_image_workflow 自动分派，无需新模型
--
-- 前置：双机已发版到含 relay/h3_image_size_billing.go 的同一 SHA。
--       **顺序仍然是先发版、再改描述**（本次不改价，无静默免单风险）。
--
-- ⚠️ 价格表 options.ModelPrice **保持 ¥0.05 不变**！4K 的 ¥0.10 是代码里
--    OtherRatios{"size_4k":2} 的倍率，不是另一条 price 记录。这里若把基础价
--    改成 ¥0.10，普通尺寸也会跟着涨，4K 更会变成 ¥0.20。
-- ⚠️ 两台生产库各自独立，老机 119.29.253.97 与新机 newapi 各执行一次。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_models_desc_20261113_h3image AS
  SELECT id, model_name, description, updated_time
    FROM models WHERE model_name = 'h3-image' AND deleted_at IS NULL;

-- ---------- 模型广场卡片描述 ----------
UPDATE models
   SET description = 'MiniMax H3-Image 图片生成（文生图 / 图生图同一模型）。同步 POST /v1/images/generations，返回图片直链；只给 prompt 即文生图，附带 image 参考图（data URI）即自动按图生图处理，由服务端自行判断，无需切换模型。计费 ¥0.05/张；显式指定 4K（size=4k 或 4096×4096）按 ¥0.10/张；n 张按倍数累计。6 步蒸馏采样，默认出 2K 级静帧，原生最高 4096×4096。⚠️ H3-Image 没有负向提示词输入，negative_prompt / steps / cfg 会被忽略。',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'h3-image'
   AND deleted_at IS NULL;

-- ---------- 校验（不输出任何 key）----------
\echo '=== 新描述 ==='
SELECT model_name, status, left(description, 100) AS desc_head
  FROM models WHERE model_name = 'h3-image' AND deleted_at IS NULL;

\echo '=== 价格必须仍是 0.00684931506849315（基础价未动）==='
SELECT value::jsonb ->> 'h3-image' AS h3_image_base_price
  FROM options WHERE key = 'ModelPrice';

\echo '=== 渠道/能力不应变动 ==='
SELECT c.id, c.name, c.models, c.model_mapping, a.model, a.enabled
  FROM channels c
  LEFT JOIN abilities a ON a.channel_id = c.id AND a.model = 'h3-image'
 WHERE c.name = 'GPU-H3-Image';

COMMIT;

-- =============================================================================
-- 回滚（按需手执行）：
--   UPDATE models m SET description = b.description, updated_time = b.updated_time
--     FROM _bak_models_desc_20261113_h3image b
--    WHERE m.id = b.id AND m.model_name = 'h3-image';
-- ⚠️ 描述读的是内存缓存，改完等 ~60 秒 SyncOptions 再判定，别急着重启。
-- =============================================================================
