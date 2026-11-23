-- =============================================================================
-- 补齐 seedance-2-5-flash（2026-11-23）
--
-- 背景：这个模型 2026-11-23 由并行会话上架，但**只写进了老机**
-- （ch20 漫屋ArcReel 的 models、abilities、models 卡片、ModelPrice 键），
-- 新机 api2.heibaidao.cn 完全没有 → 两台目录出现 1 个模型的漂移。
--
-- 本脚本把四处一起补齐，两台各跑一次（幂等，可重复执行）：
--   1. channels.models（追加，不覆盖其它模型）；
--   2. abilities（裸 SQL 改 channels.models **不会**自动生成能力行；
--      不补这一行 = 目录可见但调用 503）；
--   3. models 卡片（描述 / 标签 / 供应商 / 状态）；
--   4. ModelPrice 键（按秒 ¥0.25，内部 USD 等值 0.25/7.3）。
--
-- ⚠️ 顺手修正供应商：老机这行当初写成了 vendor_id=1（Moonshot），
--    Seedance 是字节跳动系 → 统一改成 6。两台一起改，避免再次漂移。
-- ⚠️ 表结构：channels 没有 deleted_at（硬删），models 有（软删）。
-- ⚠️ 生效时延：MEMORY_CACHE_ENABLED=true 时路由缓存 60s 重建、pricing 1min TTL，
--    改完不要立刻判定失败。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS bak_20261123_s25flash_channels AS
  SELECT id, models FROM channels WHERE name = '漫屋ArcReel';
CREATE TABLE IF NOT EXISTS bak_20261123_s25flash_abilities AS
  SELECT * FROM abilities WHERE model = 'seedance-2-5-flash';
CREATE TABLE IF NOT EXISTS bak_20261123_s25flash_models AS
  SELECT * FROM models WHERE model_name = 'seedance-2-5-flash';
CREATE TABLE IF NOT EXISTS bak_20261123_s25flash_modelprice AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';

-- ---------- 1. 渠道 models：追加（幂等、保序、不覆盖）----------
WITH ch AS (
  SELECT id, COALESCE(models, '') AS models FROM channels WHERE name = '漫屋ArcReel'
),
parts AS (
  SELECT ch.id,
         btrim(part.value) AS m,
         part.ord
    FROM ch
    CROSS JOIN LATERAL unnest(string_to_array(ch.models, ',')) WITH ORDINALITY AS part(value, ord)
),
current AS (
  SELECT id,
         string_agg(m, ',' ORDER BY ord) AS joined,
         bool_or(m = 'seedance-2-5-flash') AS has_model
    FROM parts
   GROUP BY id
)
UPDATE channels c
   SET models = CASE
                 WHEN current.has_model THEN current.joined
                 WHEN current.joined = '' THEN 'seedance-2-5-flash'
                 ELSE current.joined || ',seedance-2-5-flash'
               END
  FROM current
 WHERE c.id = current.id
   AND c.models IS DISTINCT FROM (CASE
                 WHEN current.has_model THEN current.joined
                 WHEN current.joined = '' THEN 'seedance-2-5-flash'
                 ELSE current.joined || ',seedance-2-5-flash'
               END);

-- ---------- 2. abilities：行不存在就补（存在则原样保留，不覆盖运营配置）----------
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT 'default', 'seedance-2-5-flash', c.id, true, 0, 0, NULL
  FROM channels c
 WHERE c.name = '漫屋ArcReel'
   AND NOT EXISTS (
     SELECT 1 FROM abilities a
      WHERE a.channel_id = c.id AND a.model = 'seedance-2-5-flash' AND a."group" = 'default'
   );

-- 渠道被 AutoBan 停过的话，能力行可能是 enabled=false —— 这一行必须在售
UPDATE abilities
   SET enabled = true
 WHERE model = 'seedance-2-5-flash'
   AND channel_id IN (SELECT id FROM channels WHERE name = '漫屋ArcReel' AND status = 1)
   AND enabled = false;

-- ---------- 3. 模型卡片 ----------
UPDATE models
   SET description = 'Seedance 2.5 Flash 文/图生视频。4–15 秒逐秒可选，固定 720p，¥0.25/秒；7 档比例（含 auto），≤10 张参考图',
       tags = 'video',
       vendor_id = (SELECT id FROM vendors WHERE name = '字节跳动' LIMIT 1),
       status = 1,
       updated_time = (EXTRACT(EPOCH FROM now()))::bigint
 WHERE model_name = 'seedance-2-5-flash'
   AND deleted_at IS NULL;

INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, name_rule, sort_weight,
                    created_time, updated_time, deleted_at)
SELECT 'seedance-2-5-flash',
       'Seedance 2.5 Flash 文/图生视频。4–15 秒逐秒可选，固定 720p，¥0.25/秒；7 档比例（含 auto），≤10 张参考图',
       NULL, 'video', (SELECT id FROM vendors WHERE name = '字节跳动' LIMIT 1), NULL,
       1, 0, 0, 0,
       (EXTRACT(EPOCH FROM now()))::bigint,
       (EXTRACT(EPOCH FROM now()))::bigint,
       NULL
 WHERE NOT EXISTS (
   SELECT 1 FROM models WHERE model_name = 'seedance-2-5-flash' AND deleted_at IS NULL
 );

-- ---------- 4. 价格键（¥0.25/秒 → 0.25/7.3）----------
UPDATE options
   SET value = jsonb_set(value::jsonb, '{seedance-2-5-flash}',
                         to_jsonb(0.03424657534246575::double precision))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'seedance-2-5-flash');

COMMIT;

-- ---------- 验收 ----------
-- 1) 渠道：   SELECT models FROM channels WHERE name='漫屋ArcReel';
-- 2) 能力行： SELECT model, channel_id, enabled FROM abilities WHERE model='seedance-2-5-flash';
-- 3) 卡片：   SELECT model_name, status, vendor_id, tags FROM models WHERE model_name='seedance-2-5-flash' AND deleted_at IS NULL;
-- 4) 价格：   SELECT round((value::jsonb->>'seedance-2-5-flash')::numeric * 7.3, 4) FROM options WHERE key='ModelPrice';
