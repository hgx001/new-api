-- 生产：下架 wan-3.0 / wan-3.0-特惠 / seedance2.5-特惠 三个模型（2026-10-26）
--
-- 老板指令：把这三个模型（模型广场显示名）的渠道删掉。
--   · wan-3.0 + seedance2.5-特惠 都只挂在**渠道 24「MegaAI-Wan3」**
--     （models = wan-3.0,seedance2.5-特惠）→ 整条删除该渠道（走管理 API
--     DELETE /api/channel/24，会连带删能力行并刷新路由缓存）。
--   · wan-3.0-特惠 挂在**渠道 20「漫屋ArcReel」**，但该渠道还服务 8 个在售模型
--     （seedance-2.0 / gemini-web-video / Nano Banana Pro / jimeng-video-reverse /
--      seedance-2-5 / seedance-2-0-fast / wan2.7-图生 / wan2.7-编辑）
--     → 按用户确认：**只摘掉 wan-3.0-特惠 及其旧名 wan-3.0-无限**，保留渠道 20。
--
-- 为什么必须动 channels.models 而不只是删 abilities：
--   model/ability.go 的 FixAbility()（后台「修复能力」按钮 + main.go 启动兜底）
--   会 TRUNCATE abilities 再按 channels.models 重建 —— 只删 abilities，
--   一次修复就把这些模型「复活」。所以摘 channels.models 是唯一持久的下架。
--
-- 代码侧不动：megabyai / manwu adaptor 仍保留这些名字（内部常量），没有 ability
-- 就没有路由；广场白名单与倍率表留着也无害（按 2026-10-25 下架批次的同一口径）。
--
-- 幂等：可重复执行。

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_delist_wan3_megaai_20261026_channels AS
  SELECT * FROM channels WHERE id IN (20, 24);
CREATE TABLE IF NOT EXISTS _bak_delist_wan3_megaai_20261026_abilities AS
  SELECT * FROM abilities
   WHERE model IN ('wan-3.0', 'wan-3.0-特惠', 'wan-3.0-无限', 'seedance2.5-特惠');
CREATE TABLE IF NOT EXISTS _bak_delist_wan3_megaai_20261026_models AS
  SELECT * FROM models
   WHERE model_name IN ('wan-3.0', 'wan-3.0-特惠', 'wan-3.0-无限', 'seedance2.5-特惠');
CREATE TABLE IF NOT EXISTS _bak_delist_wan3_megaai_20261026_options AS
  SELECT * FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');

-- ---------- ① 渠道 20：摘掉 wan-3.0-特惠 与旧名 wan-3.0-无限 ----------
UPDATE channels
   SET models = array_to_string(
         array_remove(
           array_remove(string_to_array(models, ','), 'wan-3.0-特惠'),
           'wan-3.0-无限'),
         ',')
 WHERE id = 20
   AND models ~ '(^|,)(wan-3\.0-特惠|wan-3\.0-无限)(,|$)';

-- ---------- ② 删这三个模型的能力行（渠道 24 由管理 API 删除时自动清理，这里兜底）----------
DELETE FROM abilities
 WHERE model IN ('wan-3.0', 'wan-3.0-特惠', 'wan-3.0-无限', 'seedance2.5-特惠');

-- ---------- ③ 删广场卡片 ----------
DELETE FROM models
 WHERE model_name IN ('wan-3.0', 'wan-3.0-特惠', 'wan-3.0-无限', 'seedance2.5-特惠');

-- ---------- ④ 摘价格键（只摘这四个名字，**不动** aliyun:wan-3.0）----------
UPDATE options
   SET value = (value::jsonb
        - 'wan-3.0' - 'wan-3.0-特惠' - 'wan-3.0-无限' - 'seedance2.5-特惠')::text
 WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND (value::jsonb ? 'wan-3.0'
        OR value::jsonb ? 'wan-3.0-特惠'
        OR value::jsonb ? 'wan-3.0-无限'
        OR value::jsonb ? 'seedance2.5-特惠');

COMMIT;

-- ---------- 复核（事务外只读）----------
\echo '--- ① 渠道 20 应剩 8 个在售模型（wan-3.0-特惠 / wan-3.0-无限 已摘）---'
SELECT id, name, status, models FROM channels WHERE id IN (20, 24) ORDER BY id;

\echo '--- ② 能力表：四个名字必须 0 行 ---'
SELECT model, channel_id, "group", enabled FROM abilities
 WHERE model IN ('wan-3.0', 'wan-3.0-特惠', 'wan-3.0-无限', 'seedance2.5-特惠');

\echo '--- ③ 广场卡片：必须 0 行 ---'
SELECT id, model_name, status FROM models
 WHERE model_name IN ('wan-3.0', 'wan-3.0-特惠', 'wan-3.0-无限', 'seedance2.5-特惠');

\echo '--- ④ 价格键：必须 0 行（aliyun:wan-3.0 是另一个模型，保留）---'
SELECT o.key AS opt_key, e.key AS model_key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND e.key IN ('wan-3.0', 'wan-3.0-特惠', 'wan-3.0-无限', 'seedance2.5-特惠', 'aliyun:wan-3.0');

\echo '--- ⑤ 渠道 20 剩余能力（确认 8 个在售模型没被连带删掉）---'
SELECT model, enabled FROM abilities WHERE channel_id = 20 ORDER BY model;

-- =====================================================================
-- 回滚
-- =====================================================================
-- BEGIN;
-- -- 渠道（渠道 24 若已删除，需用管理 API 重新创建：name/type/base_url/key/models/model_mapping）
-- UPDATE channels c SET models = b.models FROM _bak_delist_wan3_megaai_20261026_channels b
--  WHERE c.id = b.id;
-- -- 能力行
-- INSERT INTO abilities SELECT * FROM _bak_delist_wan3_megaai_20261026_abilities
--   ON CONFLICT DO NOTHING;
-- -- 广场卡片
-- INSERT INTO models SELECT * FROM _bak_delist_wan3_megaai_20261026_models
--   ON CONFLICT (id) DO NOTHING;
-- -- 价格键（逐键补回，不用整份覆盖，避免覆盖期间其它调价）
-- UPDATE options o
--    SET value = jsonb_set(
--          jsonb_set(
--            jsonb_set(
--              jsonb_set(o.value::jsonb, '{wan-3.0}', '0.4109589041095891'::jsonb),
--              '{wan-3.0-特惠}', '0.20547945205479452055'::jsonb),
--            '{wan-3.0-无限}', '0.20547945205479452055'::jsonb),
--          '{seedance2.5-特惠}', '0.410958904109589'::jsonb)::text
--  WHERE o.key = 'ModelPrice';
-- COMMIT;
-- ⚠️ 文档要一起回滚（前端教程另一次发版）。
-- ============================================================================
