-- =============================================================================
-- 上架 / 补齐 CapCut（剪映国际版）五档模型（2026-11-14）
--
-- 授权：用户「把模型上架 newapi」
--
-- 背景：
--   2026-11-13 CapCut 接入时，老机（api.heibaidao.cn）已建好五档卡片与 abilities；
--   但**新机（api2.heibaidao.cn）库是独立的**，五档全缺 → 双机行为不一致，
--   用户从 api2 侧下单看不到模型、或落到没有该模型的渠道。
--
-- ⚠️ 两台生产库各自独立，本脚本需在**两台各执行一次**（幂等，可重复跑）。
-- ⚠️ 执行顺序：**先发版，再改库**（AGENTS.md 生产运维硬约束）。
--    库里先有模型、后端还不认识时会计不出价 → 静默免单。
--
-- 五档（对外公开名 → 上游真名）：
--   cp-seedance-2.0mini  → seedance_2.0_mini   4–15s  ¥0.05/秒  720p ×1.5
--   cp-seedance-2.0fast  → seedance_2.0_fast   4–15s  ¥0.08/秒  720p ×1.5
--   cp-seedance-2.0      → seedance_2.0        4–15s  ¥0.15/秒  720p ×2
--   cp-seedance-2.5flash → seedance_2.5_flash  4–30s  ¥0.15/秒  720p ×2
--   cp-seedance-2.5      → seedance_2.5        4–30s  ¥0.20/秒  720p ×2
--
-- 计价：ModelPrice 配 **480p 基准单价（元/秒）**，720p 由 manwu 适配器的
--   EstimateBilling 用 size 倍率乘回（capcut720PRatios）。
--   ⚠️ 改价时 ModelPrice 与 adaptor.go 的 capcut720PRatios 必须同时改。
--   内部按 USD 等值存储（¥1 = 1/7.3 USD），展示时再乘 7.3。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261114_capcut AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';
CREATE TABLE IF NOT EXISTS _bak_channels_20261114_capcut AS
  SELECT id, models, remark FROM channels WHERE name = '漫屋ArcReel';
CREATE TABLE IF NOT EXISTS _bak_abilities_20261114_capcut AS
  SELECT * FROM abilities WHERE model LIKE 'cp-seedance%';
CREATE TABLE IF NOT EXISTS _bak_models_20261114_capcut AS
  SELECT * FROM models WHERE model_name LIKE 'cp-seedance%';

-- ---------- 1. 渠道 models 字段：追加五个公开名（**合并，不覆盖**）----------
-- ⚠️ 用 string_agg 做并集，绝不整体覆盖：channels.models 是逗号分隔文本，
--    直接 PUT 会抹掉其他模型（AGENTS.md / 用户长期约定：批量更新先取现状再并集）。
-- ⚠️ 幂等：NOT EXISTS 去重，重复执行不会产生 'a,,b' 或重复项。
-- ⚠️ ord 用 10000+ 偏移把新增项排到末尾，同时保证已有项的相对顺序不变。
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
           'cp-seedance-2.0mini',
           'cp-seedance-2.0fast',
           'cp-seedance-2.0',
           'cp-seedance-2.5',
           'cp-seedance-2.5flash'
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
-- ⚠️ 直改 channels.models 不会自动同步 abilities；漏了这步表现为「模型不存在」。
-- ⚠️ 幂等：按 (group, model, channel_id) 去重。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT a."group", a.model, c.id, true, c.priority, c.weight, c.tag
  FROM (VALUES
    ('default', 'cp-seedance-2.0mini'),
    ('default', 'cp-seedance-2.0fast'),
    ('default', 'cp-seedance-2.0'),
    ('default', 'cp-seedance-2.5'),
    ('default', 'cp-seedance-2.5flash')
  ) AS a("group", model)
  JOIN channels c ON c.name = '漫屋ArcReel'
 WHERE NOT EXISTS (
         SELECT 1 FROM abilities x
          WHERE x.model = a.model AND x.channel_id = c.id AND x."group" = a."group"
       );

-- ---------- 3. 模型广场卡片 ----------
-- ⚠️ endpoints 用 '[]'（与既有 seedance-* 卡片一致），tags='video'，vendor_id=6（字节跳动）。
-- ⚠️ 描述里**必须写清实际计价口径**（按秒 / 480p 与 720p 两档），
--    不能写「仅 480p」「按次计费」—— 那与实际计费不符（历史卡片曾写错）。
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
    ('cp-seedance-2.0mini',
     'Seedance 2.0 Mini 文/图生视频（CapCut 最省档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.05/秒、720p ¥0.075/秒。'),
    ('cp-seedance-2.0fast',
     'Seedance 2.0 Fast 文/图生视频（CapCut 快速档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.08/秒、720p ¥0.12/秒。'),
    ('cp-seedance-2.0',
     'Seedance 2.0 文/图生视频（CapCut 旗舰档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。'),
    ('cp-seedance-2.5flash',
     'Seedance 2.5 Flash 文/图生视频（CapCut 快速旗舰档）。4–30 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。'),
    ('cp-seedance-2.5',
     'Seedance 2.5 文/图生视频（CapCut 最新旗舰）。4–30 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.20/秒、720p ¥0.40/秒。')
  ) AS v(model_name, description)
 WHERE NOT EXISTS (
         SELECT 1 FROM models mo
          WHERE mo.model_name = v.model_name AND mo.deleted_at IS NULL
       );

-- ---------- 4. 修正历史卡片描述（2026-11-13 写错口径）----------
-- ⚠️ 老机 2.0 / 2.0fast 的描述写「仅 480p」「按次计费」，
--    实际是 480p + 720p 两档、按秒计费 → 客户按描述理解会算错钱。
UPDATE models
   SET description = CASE model_name
         WHEN 'cp-seedance-2.0mini'  THEN 'Seedance 2.0 Mini 文/图生视频（CapCut 最省档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.05/秒、720p ¥0.075/秒。'
         WHEN 'cp-seedance-2.0fast'  THEN 'Seedance 2.0 Fast 文/图生视频（CapCut 快速档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.08/秒、720p ¥0.12/秒。'
         WHEN 'cp-seedance-2.0'      THEN 'Seedance 2.0 文/图生视频（CapCut 旗舰档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。'
         WHEN 'cp-seedance-2.5flash' THEN 'Seedance 2.5 Flash 文/图生视频（CapCut 快速旗舰档）。4–30 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。'
         WHEN 'cp-seedance-2.5'      THEN 'Seedance 2.5 文/图生视频（CapCut 最新旗舰）。4–30 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.20/秒、720p ¥0.40/秒。'
       END,
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name LIKE 'cp-seedance%'
   AND deleted_at IS NULL
   AND description IS DISTINCT FROM CASE model_name
         WHEN 'cp-seedance-2.0mini'  THEN 'Seedance 2.0 Mini 文/图生视频（CapCut 最省档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.05/秒、720p ¥0.075/秒。'
         WHEN 'cp-seedance-2.0fast'  THEN 'Seedance 2.0 Fast 文/图生视频（CapCut 快速档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.08/秒、720p ¥0.12/秒。'
         WHEN 'cp-seedance-2.0'      THEN 'Seedance 2.0 文/图生视频（CapCut 旗舰档）。4–15 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。'
         WHEN 'cp-seedance-2.5flash' THEN 'Seedance 2.5 Flash 文/图生视频（CapCut 快速旗舰档）。4–30 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.15/秒、720p ¥0.30/秒。'
         WHEN 'cp-seedance-2.5'      THEN 'Seedance 2.5 文/图生视频（CapCut 最新旗舰）。4–30 秒逐秒可选，6 档比例，≤9 张参考图，480p / 720p，按秒计费：480p ¥0.20/秒、720p ¥0.40/秒。'
       END;

-- ---------- 5. 价格（合并，绝不整字段覆盖）----------
-- 元/秒 → 内部 USD 等值（÷7.3），站点展示时再乘 7.3 还原成元/秒。
--
-- ⚠️ **写字面量，不写 (x/7.3) 表达式**：老机 2026-11-13 存的是手工截断到
--    12 位有效数字的值（如 0.006849315068，而不是 0.006849315068493151）。
--    用表达式算会在新机写出更长的小数 → 两台机器的 ModelPrice 字节不一致，
--    对账时看着像「两边价不一样」。这里直接用与老机**完全相同**的字面量，
--    保证双机一致（×7.3 还原后仍是 0.05 / 0.08 / 0.15 / 0.15 / 0.20）。
--
-- ⚠️ **一条一写**，不堆嵌套 jsonb_set：嵌套五层极易括号失衡（已踩过一次），
--    且一旦出错整个事务回滚、连前面的步骤一起白跑。分开写每条都是
--    「一次独立断言」，失败时能立刻看出是哪一个键。
-- ⚠️ **只写缺失的键**（NOT (value::jsonb ? 'key')）：已有键绝不被覆盖，
--    避免误改已在生产的价。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{cp-seedance-2.0mini}', to_jsonb(0.006849315068::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'cp-seedance-2.0mini');

UPDATE options
   SET value = jsonb_set(value::jsonb, '{cp-seedance-2.0fast}', to_jsonb(0.01095890411::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'cp-seedance-2.0fast');

UPDATE options
   SET value = jsonb_set(value::jsonb, '{cp-seedance-2.0}', to_jsonb(0.020547945205::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'cp-seedance-2.0');

UPDATE options
   SET value = jsonb_set(value::jsonb, '{cp-seedance-2.5flash}', to_jsonb(0.020547945205::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'cp-seedance-2.5flash');

UPDATE options
   SET value = jsonb_set(value::jsonb, '{cp-seedance-2.5}', to_jsonb(0.027397260274::double precision))::text
 WHERE key = 'ModelPrice' AND NOT (value::jsonb ? 'cp-seedance-2.5');

-- ---------- 校验（不输出任何 key）----------
\echo '=== 渠道 models（应含 5 个 cp-* 且原有模型未丢）==='
SELECT id, name, models FROM channels WHERE name = '漫屋ArcReel';

\echo '=== abilities（应为 5 行）==='
SELECT "group", model, channel_id, enabled, priority, weight
  FROM abilities WHERE model LIKE 'cp-seedance%' ORDER BY model;

\echo '=== 卡片（应为 5 行，描述含 720p）==='
SELECT model_name, status, tags, vendor_id,
       (description LIKE '%720p%') AS desc_has_720p,
       left(description, 40) AS desc_head
  FROM models WHERE model_name LIKE 'cp-seedance%' AND deleted_at IS NULL
 ORDER BY model_name;

\echo '=== 价格（内部 USD 等值；×7.3 应还原成元/秒）==='
SELECT value::jsonb ->> 'cp-seedance-2.0mini'  AS mini,
       value::jsonb ->> 'cp-seedance-2.0fast'  AS fast,
       value::jsonb ->> 'cp-seedance-2.0'      AS m20,
       value::jsonb ->> 'cp-seedance-2.5flash' AS m25flash,
       value::jsonb ->> 'cp-seedance-2.5'      AS m25
  FROM options WHERE key = 'ModelPrice';

COMMIT;

-- =============================================================================
-- 回滚（按需手执行）：
--   UPDATE options SET value = (SELECT value FROM _bak_modelprice_20261114_capcut WHERE key='ModelPrice')
--    WHERE key = 'ModelPrice';
--   DELETE FROM abilities WHERE model LIKE 'cp-seedance%';
--   DELETE FROM models WHERE model_name LIKE 'cp-seedance%';
--   UPDATE channels SET models = (SELECT models FROM _bak_channels_20261114_capcut WHERE name='漫屋ArcReel')
--    WHERE name = '漫屋ArcReel';
-- ⚠️ 回滚/新建后同样要等 ~60 秒 SyncChannelCache 重建路由，别急着判定失败。
-- =============================================================================
