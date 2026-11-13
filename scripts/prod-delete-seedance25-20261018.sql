-- new-api 生产库：彻底删除模型 seedance-2.5（漫屋 ArcReel · Dola 官网，固定 30s/720P）
-- 2026-10-18 指令：该模型已不可用（历史 38 单：consume 20 / refund 16 / err 2；
--   最近 10-18 00:28 一单扣费 54794 后 9 分钟退款，10-16 21:14 两单 404 Not Found），
--   要求「下架 + 直接删除」。
--
-- ⚠️ 与 seedance-2-5（连字符，豆包官网，¥3/次）是两个模型，本脚本只动点号版。
--
-- 三处同删（缺一不可，原理见 prod-hide-seedance25-20261010.sql 头部注释）：
--   ① channels.models 摘除 → 路由源头（否则渠道被编辑时 UpdateAbilities 会按 models 重建，能力行复活）
--   ② abilities 删行      → 广场 /api/pricing + 下游 /v1/models 立即消失
--   ③ models 删卡片        → 模型广场元数据（描述/图标/标签）
--
-- 生效时延：/v1/models 立即；广场 pricing TTL 1min；路由缓存 ≤60s。无需重启、无需发版。
--
-- 回滚（依赖本次备份表）：
--   BEGIN;
--   UPDATE channels SET models = (SELECT models FROM _bak_del_seedance25_channels_20261018) WHERE id = 20;
--   INSERT INTO abilities SELECT * FROM _bak_del_seedance25_abilities_20261018
--     ON CONFLICT ("group", model, channel_id) DO NOTHING;
--   INSERT INTO models SELECT * FROM _bak_del_seedance25_models_20261018
--     ON CONFLICT (id) DO NOTHING;
--   COMMIT;

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_del_seedance25_channels_20261018 AS
  SELECT * FROM channels WHERE id = 20;

CREATE TABLE IF NOT EXISTS _bak_del_seedance25_abilities_20261018 AS
  SELECT * FROM abilities WHERE model = 'seedance-2.5';

CREATE TABLE IF NOT EXISTS _bak_del_seedance25_models_20261018 AS
  SELECT * FROM models WHERE model_name = 'seedance-2.5';

-- ---------- ① 渠道模型表：摘除独立项 seedance-2.5 ----------
-- 正则只匹配「开头或逗号 + seedance-2.5 + 逗号或结尾」，不会误伤 seedance-2-5
UPDATE channels
   SET models = trim(both ',' from regexp_replace(models, '(^|,)seedance-2\.5(,|$)', '\1', 'g'))
 WHERE id = 20
   AND models ~ '(^|,)seedance-2\.5(,|$)';

-- ---------- ② 能力表：删行 ----------
DELETE FROM abilities WHERE model = 'seedance-2.5';

-- ---------- ③ 模型卡片：硬删除 ----------
DELETE FROM models WHERE model_name = 'seedance-2.5';

COMMIT;

-- ---------- 复核 ----------
\echo '--- 渠道 20 models（应无 seedance-2.5，仍保留 seedance-2-5）---'
SELECT id, name, models FROM channels WHERE id = 20;
\echo '--- abilities 残留（应为 0 行）---'
SELECT "group", model, channel_id FROM abilities WHERE model = 'seedance-2.5';
\echo '--- models 卡片残留（应为 0 行）---'
SELECT id, model_name FROM models WHERE model_name = 'seedance-2.5';
\echo '--- 同族模型对照（应仍有 seedance-2-5 / seedance-2.0 / seedance-2-0-fast）---'
SELECT model_name FROM models WHERE model_name ILIKE '%seedance%' ORDER BY 1;
