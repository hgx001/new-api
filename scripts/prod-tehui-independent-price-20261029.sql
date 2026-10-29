-- 生产：seedance2.5-特惠 恢复独立定价（2026-10-29 第三轮）
--
-- 老板纠正：这个模型不跟 wan3 走，价格独立、保持原来的价。
--   720P ¥0.20/秒、1080P ¥0.30/秒（prime 现在是 ¥0.25 / ¥0.30，两者只差 720P 一档）
--
-- 之前错在哪：把它当成「prime 的对外别名」处理，于是
--   ① controller.youzanAliasTargets 里登记了它 → 同步任务把 prime 的价覆盖过来；
--   ② resolutionRatioForModel 走 isPrimeModel 分支读的是 **prime 的倍率**。
-- 两处都会让「prime 调价，特惠跟着动」，而且全程不报错。
--
-- 代码侧已改为：特惠有自己的倍率表 tehuiResolutionSizeRatio{720P:1.0,1080P:1.5}，
-- 并且不在 youzanAliasTargets 里 —— 同步任务只写上游那两个真实模型，不碰它。
--
-- 本脚本只动 options 里这两处，渠道/能力/卡片/文案都不动。

BEGIN;

CREATE TABLE IF NOT EXISTS _bak_tehui_price_20261029c AS
SELECT key, value, now() AS snap_at
  FROM options
 WHERE key IN ('ModelPrice', 'YouzanWan3Pricing');

-- 1) 特惠基准价 = 0.20 / 7.3（不取整，与同步任务同口径）
UPDATE options
   SET value = jsonb_set(value::jsonb,
                 ARRAY['seedance2.5-特惠'], '0.027397260273972605'::jsonb, false)::text
 WHERE key = 'ModelPrice';

-- 2) 特惠自己的倍率：1080P = 0.30 / 0.20 = 1.5
--    （同步任务下一次跑会把这个键整体清掉，届时由代码常量
--      tehuiResolutionSizeRatio 兜底，值同样是 1.5，行为不变）
UPDATE options
   SET value = jsonb_set(value::jsonb,
                 '{ratios,seedance2.5-特惠,1080P}', '1.5'::jsonb, false)::text
 WHERE key = 'YouzanWan3Pricing';

-- 3) 自检
DO $$
DECLARE
  tehui_usd numeric;
  prime_usd numeric;
  tehui_1080 numeric;
  prime_1080 numeric;
BEGIN
  SELECT (value::jsonb ->> 'seedance2.5-特惠')::numeric,
         (value::jsonb ->> 'wan3.0-video-prime')::numeric
    INTO tehui_usd, prime_usd
    FROM options WHERE key = 'ModelPrice';
  IF round(tehui_usd * 7.3, 4) <> 0.20 THEN
    RAISE EXCEPTION '特惠基准价不是 ¥0.20：%', tehui_usd * 7.3;
  END IF;
  IF round(prime_usd * 7.3, 4) <> 0.25 THEN
    RAISE EXCEPTION 'prime 基准价被改动了：%（应为 ¥0.25）', prime_usd * 7.3;
  END IF;

  SELECT (value::jsonb #>> '{ratios,seedance2.5-特惠,1080P}')::numeric,
         (value::jsonb #>> '{ratios,wan3.0-video-prime,1080P}')::numeric
    INTO tehui_1080, prime_1080
    FROM options WHERE key = 'YouzanWan3Pricing';
  IF tehui_1080 <> 1.5 THEN
    RAISE EXCEPTION '特惠 1080P 倍率不是 1.5：%', tehui_1080;
  END IF;
  IF prime_1080 <> 1.2 THEN
    RAISE EXCEPTION 'prime 1080P 倍率不是 1.2：%', prime_1080;
  END IF;
END $$;

COMMIT;

-- 4) 结果：各档实付价（元/秒）
SELECT 'wan3.0-video-prime' AS model,
       round((m.value::jsonb ->> 'wan3.0-video-prime')::numeric * 7.3, 4) AS p720,
       round((m.value::jsonb ->> 'wan3.0-video-prime')::numeric * 7.3
             * (p.value::jsonb #>> '{ratios,wan3.0-video-prime,1080P}')::numeric, 4) AS p1080
  FROM options m, options p
 WHERE m.key = 'ModelPrice' AND p.key = 'YouzanWan3Pricing'
UNION ALL
SELECT 'seedance2.5-特惠',
       round((m.value::jsonb ->> 'seedance2.5-特惠')::numeric * 7.3, 4),
       round((m.value::jsonb ->> 'seedance2.5-特惠')::numeric * 7.3
             * (p.value::jsonb #>> '{ratios,seedance2.5-特惠,1080P}')::numeric, 4)
  FROM options m, options p
 WHERE m.key = 'ModelPrice' AND p.key = 'YouzanWan3Pricing';

-- 回滚（如需）：
--   UPDATE options o SET value = b.value
--     FROM _bak_tehui_price_20261029c b WHERE o.key = b.key;
--   DROP TABLE _bak_tehui_price_20261029c;
