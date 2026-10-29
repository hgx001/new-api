-- ============================================================================
-- 2026-10-29  seedance2.5-特惠 重新上架：作为 wan3.0-video-prime 的对外别名
--
-- 背景：
--   seedance2.5-特惠 原挂在 MegaAI 渠道（wan-3.0，按次 ¥5.0/条），
--   2026-10-26 随该渠道一起停售（commit 141d60c39），此后无渠道、无能力行、无卡片。
--   本次让这个名字复活，但**改路由到有赞 Wan3 的 wan3.0-video-prime**：
--   上游仍是同一份算力，只是对外换个名字卖。
--
-- 代码侧（必须先发版，否则路由报 unsupported Youzan Wan3 model）：
--   youzanwan3/constants.go  tehuiModel = "seedance2.5-特惠"
--   youzanwan3/adaptor.go    upstreamModelFor: tehuiModel -> primeModel
--                            isPrimeModel:     认 tehuiModel（档位/时长/倍率同 prime）
--   model/upstream_redact.go 该名字已在 upstreamAliasModels 里 → 日志/响应自动脱敏
--   model/pricing.go         白名单已放行
--
-- 计费与 wan3.0-video-prime 完全一致：
--   ModelPrice = 0.027397260273972602 USD/秒（×7.3 = ¥0.20/秒，720P 基准）
--   1080P 走 size 倍率 1.5 → ¥0.30/秒
--   所以 ModelPrice 必须与 prime **逐字相同**，否则两个名字同货不同价。
--
-- 本脚本只动四处：channels.models / abilities / models 卡片 / ModelPrice。
-- 不动代码、不动渠道状态（渠道 17 本来就是 status=1 在跑）。
-- ============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------------------------------------------------------------------------
-- 0. 备份（4 张快照，便于回滚；表名带日期，可重复执行前先删）
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS _bak_tehui_20261029_channels;
CREATE TABLE _bak_tehui_20261029_channels AS
SELECT id, name, models, status, model_mapping, now() AS snap_at
  FROM channels WHERE id = 17;

DROP TABLE IF EXISTS _bak_tehui_20261029_abilities;
CREATE TABLE _bak_tehui_20261029_abilities AS
SELECT * FROM abilities WHERE model IN ('seedance2.5-特惠', 'wan3.0-video-prime');

DROP TABLE IF EXISTS _bak_tehui_20261029_cards;
CREATE TABLE _bak_tehui_20261029_cards AS
SELECT * FROM models WHERE model_name IN ('seedance2.5-特惠', 'wan3.0-video-prime');

DROP TABLE IF EXISTS _bak_tehui_20261029_prices;
CREATE TABLE _bak_tehui_20261029_prices AS
SELECT key, value, now() AS snap_at
  FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');

-- ---------------------------------------------------------------------------
-- 1. channels.models：把别名挂到有赞渠道 17
--    幂等：已经存在就不重复追加（正则匹配「行首或逗号 + 名字 + 逗号或行尾」）
-- ---------------------------------------------------------------------------
UPDATE channels
   SET models = CASE
         WHEN COALESCE(models, '') = '' THEN 'seedance2.5-特惠'
         ELSE models || ',seedance2.5-特惠'
       END
 WHERE id = 17
   AND NOT (COALESCE(models, '') ~ '(^|,)seedance2\.5-特惠(,|$)');

-- ---------------------------------------------------------------------------
-- 2. abilities：插能力行，让路由真的能找到渠道 17
--    enabled 跟随渠道状态（渠道 status=1 时为 true）
-- ---------------------------------------------------------------------------
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT c."group", 'seedance2.5-特惠', c.id,
       c.status = 1, c.priority, c.weight, c.tag
  FROM channels c
 WHERE c.id = 17
ON CONFLICT ("group", model, channel_id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3. models 广场卡片：没有卡片就不上广场（isAllowedPricingModel 只决定「能不能
--    出现在定价接口」，卡片决定「广场有没有这一项」）
-- ---------------------------------------------------------------------------
INSERT INTO models (model_name, description, status, created_time, updated_time)
SELECT 'seedance2.5-特惠',
       'Wan3 Prime 增强画质（seedance2.5-特惠 为同一模型）。按分辨率分档：720P ¥0.20/秒、1080P ¥0.30/秒，时长 2-30 秒；参考素材上限与标准版一致。',
       1,
       EXTRACT(EPOCH FROM now())::bigint,
       EXTRACT(EPOCH FROM now())::bigint
 WHERE NOT EXISTS (
   SELECT 1 FROM models WHERE model_name = 'seedance2.5-特惠' AND deleted_at IS NULL
 );

-- ---------------------------------------------------------------------------
-- 4. ModelPrice：与 wan3.0-video-prime 逐字相同（¥0.20/秒 基准）
--    值取自同库现有 wan3.0-video-prime，避免手抄精度差异
-- ---------------------------------------------------------------------------
UPDATE options o
   SET value = jsonb_set(
         o.value::jsonb,
         ARRAY['seedance2.5-特惠'],
         COALESCE(o.value::jsonb -> 'wan3.0-video-prime', to_jsonb(0.027397260273972602::numeric)),
         true
       )::text
 WHERE o.key = 'ModelPrice'
   AND (o.value::jsonb -> 'wan3.0-video-prime') IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 5. 广场卡片 60（wan3.0-video-prime）文案：1080P ¥0.25 → ¥0.30
--    只替换那一处，其它文案原样保留
-- ---------------------------------------------------------------------------
UPDATE models
   SET description = replace(description, '1080P ¥0.25/秒', '1080P ¥0.30/秒'),
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE id = 60
   AND description LIKE '%1080P ¥0.25/秒%';

COMMIT;

-- ============================================================================
-- 复核（应输出 4 行，count 均 >= 1）
-- ============================================================================
SELECT 'in_channels' AS where_, count(*) FROM channels
 WHERE id = 17 AND (',' || models || ',') LIKE '%,seedance2.5-特惠,%'
UNION ALL
SELECT 'in_abilities', count(*) FROM abilities WHERE model = 'seedance2.5-特惠'
UNION ALL
SELECT 'in_cards', count(*) FROM models WHERE model_name = 'seedance2.5-特惠' AND deleted_at IS NULL
UNION ALL
SELECT 'in_prices', count(*) FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key = 'seedance2.5-特惠';

-- 两个名字的价格必须完全一致（差 0 才算对）
SELECT e.key,
       e.value,
       round((e.value::numeric * 7.3)::numeric, 4) AS cny_per_second
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice'
   AND e.key IN ('seedance2.5-特惠', 'wan3.0-video-prime')
 ORDER BY e.key;

-- ============================================================================
-- 回滚（按需手工执行）
-- ============================================================================
-- BEGIN;
-- UPDATE channels
--    SET models = btrim(regexp_replace(',' || models || ',', ',seedance2\.5-特惠,', ',', 'g'), ',')
--   FROM _bak_tehui_20261029_channels b
--  WHERE channels.id = b.id;
-- DELETE FROM abilities WHERE model = 'seedance2.5-特惠' AND channel_id = 17;
-- DELETE FROM models WHERE model_name = 'seedance2.5-特惠';
-- UPDATE options o
--    SET value = (o.value::jsonb - 'seedance2.5-特惠')::text
--  WHERE o.key = 'ModelPrice';
-- UPDATE models SET description = replace(description, '1080P ¥0.30/秒', '1080P ¥0.25/秒') WHERE id = 60;
-- COMMIT;
