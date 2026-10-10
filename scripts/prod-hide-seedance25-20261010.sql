-- new-api 生产库：隐藏模型 seedance-2.5（漫屋 ArcReel · Dola 官网 Seedance 2.5）
-- 目标 = 模型广场不展示 + 下游 /v1/models 不可见 + 不可路由（即彻底下架）。
--
-- 为什么必须"两处同改"（三条链路各自的驱动源，2026-10-10 核对代码）：
--   · 广场 /api/pricing  ← abilities(enabled=true) × isAllowedPricingModel 白名单
--   · 下游 /v1/models    ← abilities(enabled=true) × HasModelBillingConfig
--                          （model/ability.go GetGroupEnabledModels 直查该表）
--   · 路由（内存缓存开）  ← channels.models 字段本身
--                          （model/channel_cache.go InitChannelCache 直接读
--                           channel.Models 构建 group2model2channels，每
--                           SYNC_FREQUENCY=60s 重建；未开缓存时才走 abilities）
--   若只删 abilities：渠道一旦被编辑，model.UpdateAbilities 会按 channel.Models
--   "删光重建"，能力行立刻复活（广场与列表又出现）——故 channels.models 是源头。
--
-- 影响面：seedance-2.5 历史共 9 单（客户 user_id=36 共 5 单，最后 2026-10-08；
--         root user_id=28 共 4 单为测试）。下架后调用返回「无可用渠道」。
--         已核对：无任何 token 的 model_limits 白名单显式放行该模型。
--
-- 回滚（依赖本次备份表）：
--   BEGIN;
--   UPDATE channels SET models = (SELECT models FROM _bak_hide_seedance25_channels_20261010) WHERE id = 20;
--   INSERT INTO abilities SELECT * FROM _bak_hide_seedance25_abilities_20261010
--     ON CONFLICT ("group", model, channel_id) DO NOTHING;
--   COMMIT;
--   （或直接在管理后台编辑渠道 20，UpdateAbilities 会按 models 重建能力行）

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_hide_seedance25_channels_20261010 AS
  SELECT * FROM channels WHERE id = 20;

CREATE TABLE IF NOT EXISTS _bak_hide_seedance25_abilities_20261010 AS
  SELECT * FROM abilities WHERE channel_id = 20 AND model = 'seedance-2.5';

-- ---------- ① 渠道模型表：摘除 seedance-2.5（路由源头） ----------
-- 用正则摘除整个逗号分隔项，避免硬编码顺序漂移；只匹配独立项
-- （(^|,) ... (,|$)），不会误伤 db-seedance-2-5。
UPDATE channels
   SET models = trim(both ',' from regexp_replace(models, '(^|,)seedance-2\.5(,|$)', '\1', 'g'))
 WHERE id = 20
   AND models ~ '(^|,)seedance-2\.5(,|$)';

-- ---------- ② 能力表：删除该行（广场 + /v1/models 立即生效） ----------
DELETE FROM abilities
 WHERE channel_id = 20 AND model = 'seedance-2.5';

COMMIT;

-- ---------- 复核 ----------
\echo '--- 渠道 20 models（应无 seedance-2.5）---'
SELECT id, name, status, models FROM channels WHERE id = 20;
\echo '--- abilities 残留（应为 0 行）---'
SELECT "group", model, channel_id, enabled FROM abilities WHERE model = 'seedance-2.5';
\echo '--- 渠道 20 能力全貌 ---'
SELECT model, enabled FROM abilities WHERE channel_id = 20 ORDER BY model;
