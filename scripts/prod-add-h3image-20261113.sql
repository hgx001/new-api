-- =============================================================================
-- 上架 h3-image（GPU 服务器 MiniMax H3-Image 文生图）
-- 日期：2026-11-13
-- 授权：用户「帮我把gpu服务器的图片模型转为api 配置到newapi的项目上，定价0.05元一张」
--       ＋「全套上线」＋「同时跑6张不同的风景人物的，人物要真人不要有ai感」
--
-- 前置：双机已发版到 279a6d479（含 h3-image 的价格/白名单/图片端点识别）。
--       顺序不能反 —— 库里先有模型、后端还不认识时会算不出价（静默免单）。
--
-- ⚠️ 两台生产库各自独立，本脚本需在老机 119.29.253.97 与新机 newapi 各执行一次。
-- ⚠️ 渠道 key 从 ch28 SeetaCloud 库内 INSERT...SELECT 复制（同一网关同一 token），
--    不进仓库、不进终端、不打印。
-- ⚠️ 复用同一 base_url（网关根 :8443），OpenAI 适配器会拼 /v1/images/generations。
--    网关裸 Token 鉴权同时兼容 Bearer 前缀，无需改网关。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261113_h3image AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';
CREATE TABLE IF NOT EXISTS _bak_channels_20261113_h3image AS
  SELECT id, type, key, status, name, base_url, models, model_mapping
    FROM channels WHERE name = 'GPU-H3-Image';
CREATE TABLE IF NOT EXISTS _bak_abilities_20261113_h3image AS
  SELECT * FROM abilities WHERE model = 'h3-image';

-- ---------- 1. 渠道（type 1 OpenAI；key/base_url 均从 ch28 复制）----------
-- 幂等：按 name 判重，重复执行不会建出第二个渠道。
INSERT INTO channels (
    type, key, status, name, created_time, base_url, models,
    "group", priority, weight, model_mapping, auto_ban, remark
)
SELECT
    1,
    c.key,
    1,
    'GPU-H3-Image',
    EXTRACT(EPOCH FROM NOW())::bigint,
    c.base_url,
    'h3-image',
    'default',
    0,
    0,
    '{"h3-image":"qwen_image_21"}',
    1,
    'GPU MiniMax H3-Image 文生图；同步 POST /v1/images/generations；按张 ¥0.05/张（n 倍率）；与尺寸无关'
FROM channels c
WHERE c.type = 70
  AND NOT EXISTS (SELECT 1 FROM channels x WHERE x.name = 'GPU-H3-Image')
ORDER BY c.id
LIMIT 1;

-- ---------- 2. abilities（模型广场与路由都靠它）----------
-- ⚠️ 直改 channels.models 不会自动同步 abilities；漏了这步表现为「模型不存在」。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT 'default', 'h3-image', c.id, true, c.priority, c.weight, c.tag
  FROM channels c
 WHERE c.name = 'GPU-H3-Image'
   AND NOT EXISTS (
       SELECT 1 FROM abilities a
        WHERE a.model = 'h3-image' AND a.channel_id = c.id
   );

-- ---------- 3. 模型广场卡片 ----------
INSERT INTO models (
    model_name, description, icon, tags, vendor_id, endpoints,
    status, sync_official, created_time, updated_time, name_rule
)
SELECT
    'h3-image',
    'MiniMax H3-Image 文生图（GPU 服务器本地推理）。同步 POST /v1/images/generations，返回图片直链；按张 ¥0.05/张，与尺寸无关，n=2 扣 ¥0.10。6 步蒸馏采样，出 2K 级静帧，原生最高 4096×4096。⚠️ H3-Image 没有负向提示词输入，negative_prompt / steps / cfg 会被忽略。',
    '',
    '',
    NULL,
    '',
    1,
    0,
    EXTRACT(EPOCH FROM NOW())::bigint,
    EXTRACT(EPOCH FROM NOW())::bigint,
    0
WHERE NOT EXISTS (
    SELECT 1 FROM models mo WHERE mo.model_name = 'h3-image' AND mo.deleted_at IS NULL
);

-- ---------- 4. 价格（合并，绝不整字段覆盖）----------
-- 0.05 / 7.3 = 0.006849315068493151（内部按 USD 等值存储，站内展示 ¥0.05/张）。
-- 与 setting/ratio_setting/model_ratio.go 的 defaultModelPrice 必须一致：
-- 代码默认值只在库里查不到时兜底，两处不一致会出现「新装环境一个价、生产另一个价」。
-- 张数由 relay/image_handler.go 的 OtherRatios{"n":N} 乘上去，不在这里单列。
UPDATE options
   SET value = jsonb_set(
         value::jsonb,
         '{h3-image}',
         to_jsonb((0.05 / 7.3)::double precision))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'h3-image');

-- ---------- 校验（不输出任何 key）----------
\echo '=== 渠道 ==='
SELECT id, type, name, status, base_url, models, model_mapping
  FROM channels WHERE name = 'GPU-H3-Image';

\echo '=== abilities ==='
SELECT "group", model, channel_id, enabled, priority, weight
  FROM abilities WHERE model = 'h3-image';

\echo '=== 卡片 ==='
SELECT model_name, status, left(description, 60) AS desc_head
  FROM models WHERE model_name = 'h3-image' AND deleted_at IS NULL;

\echo '=== 价格（应为 0.006849315068493151；官方图模型价格不变）==='
SELECT value::jsonb ->> 'h3-image' AS h3_image_price,
       value::jsonb ->> 'gpt-image-2.5-官方' AS gpt_image_official_unchanged
  FROM options WHERE key = 'ModelPrice';

COMMIT;

-- =============================================================================
-- 回滚（按需手执行）：
--   DELETE FROM abilities WHERE model = 'h3-image';
--   DELETE FROM models WHERE model_name = 'h3-image';
--   DELETE FROM channels WHERE name = 'GPU-H3-Image';
--   UPDATE options SET value = (SELECT value FROM _bak_modelprice_20261113_h3image WHERE key='ModelPrice')
--    WHERE key = 'ModelPrice';
-- ⚠️ 回滚/新建后同样要等 ~60 秒 SyncChannelCache 重建路由，别急着判定失败。
-- =============================================================================
