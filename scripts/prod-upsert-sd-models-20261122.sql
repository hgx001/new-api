-- =============================================================================
-- 上架两档视频模型：sd-seedance-2.0fast / sd-seedance-2.0（2026-11-22）
--
-- 授权：用户「把这些模型上架 newapi。定价 2.0fast 0.1元/秒，2.0 的 0.22元/秒，
--       固定 720p，固定 9:16，描述不要暴露渠道，参考 capcut」
--
-- 对外公开名用 **sd- 中性前缀**（模型 id 在 new-api 里对用户可见，
-- 前缀不能透露渠道；cp- 是 CapCut、el- 是 ElevenLabs、tk- 是原站点）。
--
-- 两档（对外公开名 → 上游名）：
--   sd-seedance-2.0fast  → tiktok-seedance-2.0-fast   ¥0.10/秒
--   sd-seedance-2.0      → tiktok-seedance-2.0        ¥0.22/秒
--
-- 参数口径（服务端定死，不对用户暴露选择）：
--   - 分辨率固定 **720p**
--   - 比例固定 **9:16**
--   - 时长三档 5 / 10 / 12 秒（官网原生下拉，无逐秒滑杆）
--   - 参考图 ≤4 张
--
-- 计价：ModelPrice 配 **元/秒 的 USD 等值**（÷7.3），站点展示时再乘 7.3 还原。
--   因为分辨率固定 720p，**不设 size 倍率**（倍率恒 1.0），
--   与 CapCut 那套「480p 基准 × 720p 倍率」不同 —— 改价只改这一处。
--
-- ⚠️ 两台生产库（api.heibaidao.cn / api2.heibaidao.cn）各自独立，
--    本脚本需在**两台各执行一次**（幂等，可重复跑）。
-- ⚠️ 执行顺序：**先发版，再改库**（AGENTS.md 生产运维硬约束）。
--    库里先有模型、后端还不认识时会计不出价 → 静默免单。
-- ⚠️ 前端渠道 models 字段一律**并集合并**，绝不整体覆盖。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261122_sd AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';
CREATE TABLE IF NOT EXISTS _bak_channels_20261122_sd AS
  SELECT id, models, remark FROM channels WHERE name = '漫屋ArcReel';
CREATE TABLE IF NOT EXISTS _bak_abilities_20261122_sd AS
  SELECT * FROM abilities WHERE model LIKE 'sd-seedance%';
CREATE TABLE IF NOT EXISTS _bak_models_20261122_sd AS
  SELECT * FROM models WHERE model_name LIKE 'sd-seedance%';

-- ---------- 1. 渠道 models 字段：追加两个公开名（合并，不覆盖）----------
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
    FROM unnest(ARRAY[
           'sd-seedance-2.0fast',
           'sd-seedance-2.0'
         ]) WITH ORDINALITY AS u(m, ord)
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
  FROM (VALUES
    ('default', 'sd-seedance-2.0fast'),
    ('default', 'sd-seedance-2.0')
  ) AS a("group", model)
  JOIN channels c ON c.name = '漫屋ArcReel'
 WHERE NOT EXISTS (
         SELECT 1 FROM abilities x
          WHERE x.model = a.model AND x.channel_id = c.id AND x."group" = a."group"
       );

-- ---------- 3. 模型广场卡片 ----------
-- ⚠️ 描述里**不写任何渠道/平台来源**（用户明确要求），
--    但**必须写清实际参数与计价口径**：固定 720p / 9:16 / 5·10·12 秒 / 按秒计费。
--    不写清会让客户按错误预期下单（历史卡片就因为写错分辨率口径返工过）。
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
    ('sd-seedance-2.0fast',
     'Seedance 2.0 Fast 图生视频（速度优先档）。时长 5 / 10 / 12 秒可选，固定 720p、9:16 竖屏，≤4 张参考图，按秒计费 ¥0.10/秒。'),
    ('sd-seedance-2.0',
     'Seedance 2.0 图生视频（画质优先档）。时长 5 / 10 / 12 秒可选，固定 720p、9:16 竖屏，≤4 张参考图，按秒计费 ¥0.22/秒。')
  ) AS v(model_name, description)
 WHERE NOT EXISTS (
         SELECT 1 FROM models mo
          WHERE mo.model_name = v.model_name AND mo.deleted_at IS NULL
       );

-- ---------- 4. 价格（合并，绝不整字段覆盖）----------
-- 元/秒 → 内部 USD 等值（÷7.3）。写字面量而不是 (x/7.3) 表达式：
-- 表达式在不同 PG 版本/浮点路径下写出的位数可能不同，双机对账时看着像「两边价不一样」。
--
--   ¥0.10/秒 ÷ 7.3 = 0.013698630137
--   ¥0.22/秒 ÷ 7.3 = 0.030136986301
--
-- ⚠️ 一条一写，不堆嵌套 jsonb_set（括号失衡会让整个事务回滚）。
-- ⚠️ 只写缺失的键（NOT (value::jsonb ? 'key')）：已有键绝不被覆盖。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{sd-seedance-2.0fast}', to_jsonb(0.013698630137::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'sd-seedance-2.0fast');

UPDATE options
   SET value = jsonb_set(value::jsonb, '{sd-seedance-2.0}', to_jsonb(0.030136986301::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'sd-seedance-2.0');

COMMIT;

-- ---------- 验收（执行后手工核对）----------
-- 1) 渠道是否带上两个模型：      SELECT models FROM channels WHERE name='漫屋ArcReel';
-- 2) 广场是否可见：            SELECT model_name FROM models WHERE model_name LIKE 'sd-seedance%' AND deleted_at IS NULL;
-- 3) 路由是否挂上：            SELECT model, channel_id, enabled FROM abilities WHERE model LIKE 'sd-seedance%';
-- 4) 价格（×7.3 应还原成 0.10 / 0.22）：
--    SELECT round((value::jsonb->>'sd-seedance-2.0fast')::numeric * 7.3, 4),
--           round((value::jsonb->>'sd-seedance-2.0')::numeric * 7.3, 4)
--      FROM options WHERE key='ModelPrice';
