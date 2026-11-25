-- =============================================================================
-- MegaAI 渠道换货：上架 seedance-2.0-fast-m5 / seedance-2.0-m5，下架 T3-seedance-2.5
-- （2026-11-24）
--
-- 用户口径：
--   「seedance-2.0-fast-m5 / seedance-2.0-m5 这个平台的这 2 个模型对接到我们系统，
--     定价在上游的基础上各个分辨率加 4 分钱，然后下架同渠道 T3-seedance-2.5」
--
-- 上游价（newapi.megabyai.cc 的 /api/pricing，按秒 + 分辨率档）：
--   seedance-2.0-fast-m5：480p ¥0.16/秒、720p ¥0.35/秒
--   seedance-2.0-m5     ：480p ¥0.21/秒、720p ¥0.44/秒
-- 本站价 = 上游各档 + ¥0.04/秒：
--   seedance-2.0-fast-m5：480p ¥0.20/秒、720p ¥0.39/秒（倍率 1.95，在 adaptor 里）
--   seedance-2.0-m5     ：480p ¥0.25/秒、720p ¥0.48/秒（倍率 1.92，在 adaptor 里）
--
-- 台面四处一起改（缺一处就是「能调用但广场看不到」或「广场有但调 503」）：
--   1. channels 33（MegaAI, type=64）的 models；
--   2. abilities（裸改 channels.models 不会自动生成能力行）；
--   3. models 卡片（描述 / 标签 / 供应商 / 状态）；
--   4. options.ModelPrice（按秒模型的 480p 秒单价，USD 等值 = 元/7.3）。
--
-- ⚠️ 描述里**不得出现上游平台名/域名**（客户可见）。公开模型名带 `-m5` 是上游商品名，
--    老板指定沿用，不算渠道名。
-- ⚠️ 先发版、再跑本脚本：旧代码没有 m5 的能力表与计费分支，先改库会让
--    这两个模型按「未知模型」回落 wan-3.0 能力（素材 10/5/5、无时长必填）。
-- ⚠️ 双机：老机 api.heibaidao.cn 与新机 api2.heibaidao.cn 各执行一次（幂等）。
-- ⚠️ 生效时延：MEMORY_CACHE_ENABLED=true 时路由缓存 60s、pricing ~1min，改完别立刻判定失败。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS bak_20261124_m5_channels AS
  SELECT id, name, models FROM channels WHERE name = 'MegaAI';
CREATE TABLE IF NOT EXISTS bak_20261124_m5_abilities AS
  SELECT * FROM abilities WHERE channel_id IN (SELECT id FROM channels WHERE name = 'MegaAI');
CREATE TABLE IF NOT EXISTS bak_20261124_m5_models AS
  SELECT * FROM models WHERE model_name IN ('T3-seedance-2.5', 'seedance-2.0-fast-m5', 'seedance-2.0-m5');
CREATE TABLE IF NOT EXISTS bak_20261124_m5_modelprice AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';

-- ---------- 1. 渠道 models：去掉 T3，追加两个新模型（保序、幂等）----------
WITH ch AS (
  SELECT id, COALESCE(models, '') AS models FROM channels WHERE name = 'MegaAI'
),
parts AS (
  SELECT ch.id, btrim(part.value) AS m, part.ord
    FROM ch
    CROSS JOIN LATERAL unnest(string_to_array(ch.models, ',')) WITH ORDINALITY AS part(value, ord)
),
kept AS (
  SELECT id,
         string_agg(m, ',' ORDER BY ord) FILTER (WHERE m <> '') AS joined
    FROM parts
   WHERE m NOT IN ('T3-seedance-2.5')
   GROUP BY id
),
want AS (
  SELECT id,
         CASE
           WHEN COALESCE(joined, '') = '' THEN 'seedance-2.0-fast-m5,seedance-2.0-m5'
           ELSE joined || ',seedance-2.0-fast-m5,seedance-2.0-m5'
         END AS models
    FROM kept
)
UPDATE channels c
   SET models = want.models
  FROM want
 WHERE c.id = want.id
   AND c.models IS DISTINCT FROM want.models;

-- ---------- 2. abilities：删 T3，补两个新模型 ----------
DELETE FROM abilities WHERE model = 'T3-seedance-2.5';

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT 'default', m.model, c.id, true, 0, 0, NULL
  FROM channels c
 CROSS JOIN (VALUES ('seedance-2.0-fast-m5'), ('seedance-2.0-m5')) AS m(model)
 WHERE c.name = 'MegaAI'
   AND NOT EXISTS (
     SELECT 1 FROM abilities a
      WHERE a.channel_id = c.id AND a.model = m.model AND a."group" = 'default'
   );

UPDATE abilities
   SET enabled = true
 WHERE model IN ('seedance-2.0-fast-m5', 'seedance-2.0-m5')
   AND channel_id IN (SELECT id FROM channels WHERE name = 'MegaAI' AND status = 1)
   AND enabled = false;

-- ---------- 3. 模型卡片 ----------
-- 3a. 下架的 T3：留在库里但 status=0（可回滚），广场与路由都看不到。
UPDATE models
   SET status = 0,
       updated_time = (EXTRACT(EPOCH FROM now()))::bigint
 WHERE model_name = 'T3-seedance-2.5'
   AND deleted_at IS NULL
   AND status <> 0;

-- 3b. 两个新模型：描述不含上游平台名，只讲能力与价格。
UPDATE models
   SET description = 'Seedance 2.0 Fast 视频生成。时长 4–15 秒整数秒（必填），分辨率 480p / 720p，'
                     '比例支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，最多 9 张参考图 / 3 个参考视频 / 3 段参考音频'
                     '（素材须公网 http(s) 可访问，参考视频合计 ≤15 秒且不计费）。'
                     '按秒计费：480p ¥0.20/秒、720p ¥0.39/秒。',
       tags = 'video',
       vendor_id = (SELECT id FROM vendors WHERE name = '字节跳动' LIMIT 1),
       status = 1,
       updated_time = (EXTRACT(EPOCH FROM now()))::bigint
 WHERE model_name = 'seedance-2.0-fast-m5'
   AND deleted_at IS NULL;

UPDATE models
   SET description = 'Seedance 2.0 视频生成。时长 4–15 秒整数秒（必填），分辨率 480p / 720p，'
                     '比例支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，最多 9 张参考图 / 3 个参考视频 / 3 段参考音频'
                     '（素材须公网 http(s) 可访问，参考视频合计 ≤15 秒且不计费）。'
                     '按秒计费：480p ¥0.25/秒、720p ¥0.48/秒。',
       tags = 'video',
       vendor_id = (SELECT id FROM vendors WHERE name = '字节跳动' LIMIT 1),
       status = 1,
       updated_time = (EXTRACT(EPOCH FROM now()))::bigint
 WHERE model_name = 'seedance-2.0-m5'
   AND deleted_at IS NULL;

INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, name_rule, sort_weight,
                    created_time, updated_time, deleted_at)
SELECT m.model, m.description, NULL, 'video',
       (SELECT id FROM vendors WHERE name = '字节跳动' LIMIT 1), NULL,
       1, 1, 0, 0,
       (EXTRACT(EPOCH FROM now()))::bigint, (EXTRACT(EPOCH FROM now()))::bigint, NULL
  FROM (VALUES
        ('seedance-2.0-fast-m5',
         'Seedance 2.0 Fast 视频生成。时长 4–15 秒整数秒（必填），分辨率 480p / 720p，'
         '比例支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，最多 9 张参考图 / 3 个参考视频 / 3 段参考音频'
         '（素材须公网 http(s) 可访问，参考视频合计 ≤15 秒且不计费）。'
         '按秒计费：480p ¥0.20/秒、720p ¥0.39/秒。'),
        ('seedance-2.0-m5',
         'Seedance 2.0 视频生成。时长 4–15 秒整数秒（必填），分辨率 480p / 720p，'
         '比例支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，最多 9 张参考图 / 3 个参考视频 / 3 段参考音频'
         '（素材须公网 http(s) 可访问，参考视频合计 ≤15 秒且不计费）。'
         '按秒计费：480p ¥0.25/秒、720p ¥0.48/秒。')
       ) AS m(model, description)
 WHERE NOT EXISTS (
   SELECT 1 FROM models x WHERE x.model_name = m.model AND x.deleted_at IS NULL
 );

-- ---------- 4. 价表：写 480p 秒单价（USD 等值），清掉 T3 的键 ----------
-- ¥0.20/7.3 = 0.027397260273972605；¥0.25/7.3 = 0.034246575342465753
UPDATE options
   SET value = (jsonb_set(
                  jsonb_set(value::jsonb, '{seedance-2.0-fast-m5}',
                            to_jsonb(0.027397260273972605::double precision)),
                  '{seedance-2.0-m5}',
                  to_jsonb(0.034246575342465753::double precision))
                - 'T3-seedance-2.5')::text
 WHERE key = 'ModelPrice';

COMMIT;

-- ---------- 验收 ----------
-- 1) 渠道：  SELECT models FROM channels WHERE name='MegaAI';
--            期望 seedance-2.0-fast-m5,seedance-2.0-m5（且不含 T3）
-- 2) 能力行：SELECT model, channel_id, enabled FROM abilities WHERE channel_id=(SELECT id FROM channels WHERE name='MegaAI');
-- 3) 卡片：  SELECT model_name, status, left(description,40) FROM models
--            WHERE model_name IN ('seedance-2.0-fast-m5','seedance-2.0-m5','T3-seedance-2.5') AND deleted_at IS NULL;
--            期望两个 m5 status=1、T3 status=0；描述里不得出现 megabyai / MegaAI
-- 4) 价表：  SELECT key,
--              round((value::jsonb->>'seedance-2.0-fast-m5')::numeric * 7.3, 4) AS fast_480p,
--              round((value::jsonb->>'seedance-2.0-m5')::numeric * 7.3, 4) AS m5_480p,
--              (value::jsonb ? 'T3-seedance-2.5') AS has_t3
--            FROM options WHERE key='ModelPrice';
--            期望 0.2000 / 0.2500 / false
-- 5) 广场：  GET /api/pricing 里两个 m5 的 model_price 应为 0.027397260273972605 / 0.034246575342465753
--
-- 回滚（数据侧）：
--   UPDATE channels SET models='T3-seedance-2.5' WHERE name='MegaAI';
--   DELETE FROM abilities WHERE model IN ('seedance-2.0-fast-m5','seedance-2.0-m5');
--   INSERT INTO abilities("group",model,channel_id,enabled,priority,weight,tag)
--     SELECT 'default','T3-seedance-2.5',id,true,0,0,NULL FROM channels WHERE name='MegaAI';
--   UPDATE models SET status=1 WHERE model_name='T3-seedance-2.5';
--   UPDATE models SET status=0 WHERE model_name IN ('seedance-2.0-fast-m5','seedance-2.0-m5');
--   UPDATE options SET value = (jsonb_set(jsonb_set(value::jsonb,'{T3-seedance-2.5}',
--     to_jsonb(0.5479452054794521::double precision)),'{seedance-2.0-fast-m5}', 'null'::jsonb) - 'seedance-2.0-fast-m5'
--     - 'seedance-2.0-m5')::text WHERE key='ModelPrice';
--   （代码也要回滚到旧版本：数据与代码必须成对回滚）
