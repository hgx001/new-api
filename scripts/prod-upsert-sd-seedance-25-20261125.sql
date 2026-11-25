-- =============================================================================
-- 上架视频模型：sd-seedance-2.5（2026-11-25）
--
-- 授权：用户「把这个 2.5 的模型上架 newapi，定价 0.25 元/秒，固定 720p」。
--
-- 对外公开名 sd-seedance-2.5（`sd-` 中性前缀，不暴露渠道）。
-- 上游：higgsfield.ai 的 Seedance 2.5（经漫屋 ArcReel 派单，platformId=higgsfield）。
--
-- 参数口径（服务端定死/限死）：
--   - 分辨率固定 **720p**（显式传别的档位 → 400，不静默改规格）
--   - 时长 **4–30 秒**（官网滑杆 4..30，缺省 5 秒）
--   - 画面比例按官网白名单透传（16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9）
--   - 参考素材：图片 / 视频 / 音频（官网 accept 已含 video/* 与 audio/*）
--
-- 计价：ModelPrice = 元/秒 的 USD 等值（÷7.3），站点展示再乘 7.3 还原。
--   ¥0.25/秒 ÷ 7.3 = 0.034246575342
--   分辨率固定 720p → 不设 size 倍率（恒 1.0），改价只改这一处。
--
-- ⚠️ 两台生产库（api.heibaidao.cn / api2.heibaidao.cn）各自独立，本脚本要在
--    **两台各执行一次**（幂等，可重复跑）。
-- ⚠️ 执行顺序：**先发版，再改库**。库里先有模型而后端还不认识时会计不出价 → 静默免单。
-- ⚠️ 渠道 models 字段一律**并集合并**，绝不整体覆盖。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261125_hf AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';
CREATE TABLE IF NOT EXISTS _bak_channels_20261125_hf AS
  SELECT id, models, remark FROM channels WHERE name = '漫屋ArcReel';
CREATE TABLE IF NOT EXISTS _bak_abilities_20261125_hf AS
  SELECT * FROM abilities WHERE model = 'sd-seedance-2.5';
CREATE TABLE IF NOT EXISTS _bak_models_20261125_hf AS
  SELECT * FROM models WHERE model_name = 'sd-seedance-2.5';

-- ---------- 1. 渠道 models 字段：追加公开名（并集合并）----------
WITH ch AS (
  SELECT id, models FROM channels WHERE name = '漫屋ArcReel'
),
base AS (
  SELECT btrim(t.m)::text AS m, t.ord::bigint AS ord
    FROM ch
    CROSS JOIN LATERAL unnest(string_to_array(ch.models, ',')) WITH ORDINALITY AS t(m, ord)
),
adds AS (
  SELECT u.m::text AS m, (10000 + u.ord)::bigint AS ord
    FROM unnest(ARRAY['sd-seedance-2.5']) WITH ORDINALITY AS u(m, ord)
   WHERE NOT EXISTS (SELECT 1 FROM base b WHERE b.m = u.m)
)
UPDATE channels ch
   SET models = (
         SELECT string_agg(s.m, ',' ORDER BY s.ord)
           FROM (SELECT * FROM base UNION ALL SELECT * FROM adds) s
       )
 WHERE ch.name = '漫屋ArcReel';

-- ---------- 2. abilities（模型广场与路由都靠它）----------
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT a."group", a.model, c.id, true, c.priority, c.weight, c.tag
  FROM (VALUES ('default', 'sd-seedance-2.5')) AS a("group", model)
  JOIN channels c ON c.name = '漫屋ArcReel'
 WHERE NOT EXISTS (
         SELECT 1 FROM abilities x
          WHERE x.model = a.model AND x.channel_id = c.id AND x."group" = a."group"
       );

-- ---------- 3. 模型广场卡片 ----------
-- 描述不写渠道/平台来源，只写实际参数与计价口径。
INSERT INTO models (
    model_name, description, icon, tags, vendor_id, endpoints,
    status, sync_official, created_time, updated_time, name_rule
)
SELECT v.model_name, v.description, '', 'video', 6, '[]',
       1, 0,
       EXTRACT(EPOCH FROM NOW())::bigint,
       EXTRACT(EPOCH FROM NOW())::bigint,
       0
  FROM (VALUES
    ('sd-seedance-2.5',
     '图生视频 · 2.5 档。时长 4–30 秒（逐秒可选），分辨率固定 720p，画面比例可选 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，支持参考图片 / 参考视频 / 参考音频，按秒计费 ¥0.25/秒。')
  ) AS v(model_name, description)
 WHERE NOT EXISTS (
         SELECT 1 FROM models mo
          WHERE mo.model_name = v.model_name AND mo.deleted_at IS NULL
       );

-- ---------- 3.1 修正卡片描述（幂等；已存在的卡片也要改成最新口径）----------
UPDATE models
   SET description = '图生视频 · 2.5 档。时长 4–30 秒（逐秒可选），分辨率固定 720p，画面比例可选 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，支持参考图片 / 参考视频 / 参考音频，按秒计费 ¥0.25/秒。',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'sd-seedance-2.5'
   AND deleted_at IS NULL
   AND description IS DISTINCT FROM '图生视频 · 2.5 档。时长 4–30 秒（逐秒可选），分辨率固定 720p，画面比例可选 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，支持参考图片 / 参考视频 / 参考音频，按秒计费 ¥0.25/秒。';

-- ---------- 4. 价格（只写缺失的键，绝不覆盖已有键）----------
UPDATE options
   SET value = jsonb_set(value::jsonb, '{sd-seedance-2.5}', to_jsonb(0.034246575342::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'sd-seedance-2.5');

COMMIT;
