-- new-api 生产库：修正 wan3.0-video-prime 卡片描述与真实定价不一致（2026-10-30）
--
-- 问题：卡片正文写「720P ¥0.20/秒」，但**真实售价是 720P ¥0.25/秒**。
--
-- 真实定价的推导（两条路径互相印证）：
--   ① 上游权威价目表（GET https://youzan666.vip/v1/models）：
--        wan3.0-video-prime → resolutionPrices = {720P: "0.15", 1080P: "0.20"}（⚡/秒，1⚡=¥1）
--      我们的加价策略（options.YouzanWan3Pricing.markup）：720P +¥0.10、1080P +¥0.10
--        → 720P 0.15+0.10 = ¥0.25/秒；1080P 0.20+0.10 = ¥0.30/秒
--   ② 站内实际计费（options.ModelPrice 基准 ¥0.25 × size 倍率）：
--        ratios：720P = 1.0、1080P = 1.2
--        → 720P ¥0.25/秒；1080P 0.25 × 1.2 = ¥0.30/秒
--      两条路径完全吻合 → **720P ¥0.25、1080P ¥0.30 是真实售价**。
--
-- ⚠️ 卡片写 ¥0.20 的原因：那是 seedance2.5-特惠 的价格（prime 的对外别名，**独立定价**，
--    不跟随 prime —— 见 controller/youzan_pricing_sync.go 的 youzanAliasTargets 故意为空）。
--    写卡片时误用了别名的价。
--
-- ⚠️ 本脚本**只改 prime 卡片**，绝不能动 seedance2.5-特惠：
--    它是另一个商品（对外别名，独立定价 720P ¥0.20 / 1080P ¥0.30），
--    同货不同名不同价是老板 10-29 明确拍板的。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

CREATE TABLE IF NOT EXISTS _bak_prime_card_desc_20261030 AS
  SELECT * FROM models WHERE model_name IN ('wan3.0-video-prime', 'seedance2.5-特惠');

-- 只改 prime 一张卡的正文。1080P 那行本来就对，只把 720P 的 ¥0.20 改成 ¥0.25。
UPDATE models
   SET description  = 'Wan3 Prime 增强画质。按分辨率分档：720P ¥0.25/秒、1080P ¥0.30/秒，时长 2-30 秒；参考素材上限与标准版一致。',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan3.0-video-prime'
   AND deleted_at IS NULL
   AND description <> 'Wan3 Prime 增强画质。按分辨率分档：720P ¥0.25/秒、1080P ¥0.30/秒，时长 2-30 秒；参考素材上限与标准版一致。';

COMMIT;

-- ---------- 复核 ----------
-- 期望：prime 卡片 = 720P ¥0.25/秒、1080P ¥0.30/秒；
--       seedance2.5-特惠 卡片**保持** 720P ¥0.20/秒、1080P ¥0.30/秒（独立定价，不动）。
SELECT model_name, description FROM models
 WHERE deleted_at IS NULL AND model_name IN ('wan3.0-video-prime', 'seedance2.5-特惠')
 ORDER BY model_name;

-- 期望：prime 基准价 ¥0.25（0.0342465753424658）；特惠基准价 ¥0.20（0.0273972602739726）—— 两者本就不同。
SELECT e.key, e.value, round((e.value::numeric * 7.3)::numeric, 4) AS cny_base
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key IN ('wan3.0-video-prime', 'seedance2.5-特惠')
 ORDER BY e.key;

-- 期望：prime 的 size 倍率 720P=1.0、1080P=1.2 → 实际售价 720P ¥0.25、1080P ¥0.30。
SELECT value FROM options WHERE key = 'YouzanWan3Pricing';


-- =====================================================================
-- 回滚 SQL
-- =====================================================================
-- BEGIN;
-- UPDATE models SET description = (SELECT description FROM _bak_prime_card_desc_20261030
--                             WHERE model_name = 'wan3.0-video-prime' LIMIT 1),
--        updated_time = EXTRACT(EPOCH FROM now())::bigint
--  WHERE model_name = 'wan3.0-video-prime' AND deleted_at IS NULL;
-- COMMIT;
