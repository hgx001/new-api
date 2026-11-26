-- =============================================================================
-- 上架视频模型：magi-2（2026-11-25）
--
-- 授权：用户 2026-11-25「magi-2 有赞的这个模型接过来，不要测试，描述我有赞一致，
--       定价每个分辨率加一块」。
--
-- 公开名 = 上游 ID = `magi-2`（有赞 ch17，type=63）。上游文档（2026-11-25 版）：
--   - `POST /v1/videos` 提交；480p / 720p / 1080p **各支持 4–30 整数秒**；
--   - resolution 默认 1080p、ratio 默认 16:9；画幅 16:9 / 9:16 / 4:3 / 3:4 / 1:1 / 21:9；
--   - prompt ≤ 20,000 字符；纯文生 + 多模态参考；
--   - 素材走 image_urls / video_urls / audio_urls（公网 HTTPS），
--     最多 10 图 / 5 视频 / 5 音频，单段参考音视频 2–30 秒、累计 ≤30 秒；
--   - **按分辨率固定按条计费**（同档时长同价）。
--
-- 计价：上游 480p ⚡2.5 / 720p ⚡2.9 / 1080p ⚡2.9（每次）
--       → 每档 +¥1.0 = 对外 480p ¥3.5 / 720p ¥3.9 / 1080p ¥3.9（每次）。
--
--   ModelPrice 配 **1080p 基准**（上游缺省档，也是广场徽标显示的那个数）：
--   ¥3.9 ÷ 7.3 = 0.5342465753424658
--   480p 由 adaptor 的 magi2ResolutionSizeRatio 乘 3.5/3.9 ≈ 0.8974359 打折拿到，
--   720p / 1080p 倍率 1.0。
--   ⚠️ 改价时 ModelPrice 与那张倍率表必须一起改（否则档位价错位）。
--
-- ⚠️ 两台生产库（api.heibaidao.cn / api2.heibaidao.cn）各自独立：**两台各执行一次**（幂等）。
-- ⚠️ 执行顺序：**先发版，再改库**。库里有模型而代码不认识时会计不出价 → 静默免单。
-- ⚠️ 渠道 models 字段一律**并集合并**，绝不整体覆盖。
--
-- 回滚：
--   UPDATE channels SET models = (SELECT models FROM _bak_channels_20261125_magi2 WHERE id = 17) WHERE id = 17;
--   DELETE FROM abilities WHERE model = 'magi-2';
--   DELETE FROM models WHERE model_name = 'magi-2';
--   UPDATE options SET value = (SELECT value FROM _bak_modelprice_20261125_magi2) WHERE key = 'ModelPrice';
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261125_magi2 AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';
CREATE TABLE IF NOT EXISTS _bak_channels_20261125_magi2 AS
  SELECT id, models, remark FROM channels WHERE name = '有赞 Wan3';
CREATE TABLE IF NOT EXISTS _bak_abilities_20261125_magi2 AS
  SELECT * FROM abilities WHERE model = 'magi-2';
CREATE TABLE IF NOT EXISTS _bak_models_20261125_magi2 AS
  SELECT * FROM models WHERE model_name = 'magi-2';

-- ---------- 1. 渠道 models 字段：追加 magi-2（并集合并）----------
WITH ch AS (
  SELECT id, models FROM channels WHERE name = '有赞 Wan3'
),
base AS (
  SELECT btrim(t.m)::text AS m, t.ord::bigint AS ord
    FROM ch
    CROSS JOIN LATERAL unnest(string_to_array(ch.models, ',')) WITH ORDINALITY AS t(m, ord)
),
adds AS (
  SELECT u.m::text AS m, (10000 + u.ord)::bigint AS ord
    FROM unnest(ARRAY['magi-2']) WITH ORDINALITY AS u(m, ord)
   WHERE NOT EXISTS (SELECT 1 FROM base b WHERE b.m = u.m)
)
UPDATE channels c
   SET models = (
         SELECT string_agg(s.m, ',' ORDER BY s.ord)
           FROM (SELECT * FROM base UNION ALL SELECT * FROM adds) s
       )
 WHERE c.name = '有赞 Wan3';

-- ---------- 2. abilities（模型广场与路由都靠它）----------
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT a."group", a.model, c.id, true, c.priority, c.weight, c.tag
  FROM (VALUES ('default', 'magi-2')) AS a("group", model)
  JOIN channels c ON c.name = '有赞 Wan3'
 WHERE NOT EXISTS (
         SELECT 1 FROM abilities x
          WHERE x.model = a.model AND x.channel_id = c.id AND x."group" = a."group"
       );

-- ---------- 3. 模型广场卡片 ----------
-- 描述与上游（有赞）模型卡文案**逐字一致**（用户 2026-11-25 口径）。
-- 不写渠道/平台来源，不写内部模型名。
INSERT INTO models (
    model_name, description, icon, tags, vendor_id, endpoints,
    status, sync_official, created_time, updated_time, name_rule
)
SELECT v.model_name, v.description, '', 'video', NULL, '[]',
       1, 0,
       EXTRACT(EPOCH FROM NOW())::bigint,
       EXTRACT(EPOCH FROM NOW())::bigint,
       0
  FROM (VALUES
    ('magi-2',
     'MAGI-2 视频生成。480p、720p、1080p 均支持 4–30 秒，提示词最多 20,000 字符。支持 21:9 及图片、视频、音频直链参考。按分辨率固定按条计费。')
  ) AS v(model_name, description)
 WHERE NOT EXISTS (
         SELECT 1 FROM models mo
          WHERE mo.model_name = v.model_name AND mo.deleted_at IS NULL
       );

-- ---------- 3.1 修正卡片描述（幂等；已存在的卡片也改成有赞口径）----------
UPDATE models
   SET description = 'MAGI-2 视频生成。480p、720p、1080p 均支持 4–30 秒，提示词最多 20,000 字符。支持 21:9 及图片、视频、音频直链参考。按分辨率固定按条计费。',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'magi-2'
   AND deleted_at IS NULL
   AND description IS DISTINCT FROM 'MAGI-2 视频生成。480p、720p、1080p 均支持 4–30 秒，提示词最多 20,000 字符。支持 21:9 及图片、视频、音频直链参考。按分辨率固定按条计费。';

-- ---------- 4. 价格（只写缺失的键，绝不覆盖已有键）----------
UPDATE options
   SET value = jsonb_set(value::jsonb, '{magi-2}', to_jsonb(0.5342465753424658::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'magi-2');

COMMIT;
