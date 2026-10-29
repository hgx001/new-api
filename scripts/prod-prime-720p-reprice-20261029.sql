-- 生产：wan3.0-video-prime 720P ¥0.20 → ¥0.25（2026-10-29 第二轮）
--
-- 为什么不能只改 ModelPrice：
--   售价 = 上游成本 + 该档 markup，由 controller.SyncYouzanWan3Pricing 每小时
--   按 YouzanWan3Pricing 里的 markup 重算一遍写回。只改 ModelPrice 会在下一次
--   同步（≤1 小时）被 markup 算回 ¥0.20 —— 而且 25% 的变动在 30% 阈值内，
--   护栏根本不会拦，属于「悄悄改回去」。
--
-- 改法：prime 两档 markup 都设 ¥0.10
--   720P  0.15 + 0.10 = ¥0.25   （原 ¥0.20）
--   1080P 0.20 + 0.10 = ¥0.30   （不变）
--   基准档仍是最便宜的 720P → ModelPrice = 0.25 / 7.3 = 0.03424657534246575
--   1080P 倍率 = 0.30 / 0.25 = 1.2（原 1.5）
--
-- 别名 seedance2.5-特惠 与 prime 是同一份算力，必须逐字同价：
--   只改一边 = 换个名字调用价格不同，而两边都不报错。
--
-- 部署后 5 分钟后台会跑一次同步，日志应为 applied=true changes=0 ——
-- 那才是「这份配置和上游成本自洽」的证明。

BEGIN;

-- 1) 备份
CREATE TABLE IF NOT EXISTS _bak_youzan_pricing_20261029b AS
SELECT key, value, now() AS snap_at
  FROM options
 WHERE key IN ('YouzanWan3Pricing', 'ModelPrice');

-- 2) 策略：prime 720P markup 0.05 → 0.10；prime 与别名的 1080P 倍率 1.5 → 1.2
UPDATE options
   SET value = jsonb_set(
                 jsonb_set(
                   jsonb_set(
                     jsonb_set(value::jsonb,
                       '{markup,wan3.0-video-prime,720P}', '0.1'::jsonb, false),
                     '{ratios,wan3.0-video-prime,1080P}', '1.2'::jsonb, false),
                   '{ratios,seedance2.5-特惠,1080P}', '1.2'::jsonb, false),
                 '{ratios,wan3.0-video-prime,720P}', '1.0'::jsonb, false
               )::text
 WHERE key = 'YouzanWan3Pricing';

-- 3) 基准价：prime 与别名都写 0.25/7.3（不取整，与同步任务同口径）
UPDATE options
   SET value = jsonb_set(
                 jsonb_set(value::jsonb,
                   ARRAY['wan3.0-video-prime'], '0.03424657534246575'::jsonb, false),
                 ARRAY['seedance2.5-特惠'], '0.03424657534246575'::jsonb, false
               )::text
 WHERE key = 'ModelPrice';

-- 4) 自检：把售价按 markup 重算一遍，与期望逐档对齐；任何一项不符就整事务回滚。
DO $$
DECLARE
  policy     jsonb;
  markup_720 numeric;
  markup_1080 numeric;
  ratio_1080 numeric;
  alias_1080 numeric;
  base_usd   numeric;
  alias_usd  numeric;
  p720       numeric;
  p1080      numeric;
BEGIN
  SELECT value::jsonb INTO policy FROM options WHERE key = 'YouzanWan3Pricing';
  markup_720  := (policy #>> '{markup,wan3.0-video-prime,720P}')::numeric;
  markup_1080 := (policy #>> '{markup,wan3.0-video-prime,1080P}')::numeric;
  ratio_1080  := (policy #>> '{ratios,wan3.0-video-prime,1080P}')::numeric;
  alias_1080  := (policy #>> '{ratios,seedance2.5-特惠,1080P}')::numeric;

  -- 上游成本（2026-10-29 实测 /v1/models）：720P 0.15 ⚡/秒、1080P 0.20 ⚡/秒
  p720  := round(0.15 + markup_720, 3);
  p1080 := round(0.20 + markup_1080, 3);
  IF p720 <> 0.25 OR p1080 <> 0.30 THEN
    RAISE EXCEPTION '售价重算不符：720P=%, 1080P=%（期望 0.25 / 0.30）', p720, p1080;
  END IF;
  IF ratio_1080 <> 1.2 OR alias_1080 <> 1.2 THEN
    RAISE EXCEPTION '1080P 倍率不符：prime=%, alias=%（期望都是 1.2）', ratio_1080, alias_1080;
  END IF;

  SELECT (value::jsonb ->> 'wan3.0-video-prime')::numeric,
         (value::jsonb ->> 'seedance2.5-特惠')::numeric
    INTO base_usd, alias_usd
    FROM options WHERE key = 'ModelPrice';
  IF base_usd <> 0.03424657534246575 THEN
    RAISE EXCEPTION 'prime ModelPrice 不符：%', base_usd;
  END IF;
  IF base_usd <> alias_usd THEN
    RAISE EXCEPTION '别名与 prime 不同价：prime=%, alias=%', base_usd, alias_usd;
  END IF;
END $$;

COMMIT;

-- 5) 结果：各档实付价（元/秒）
SELECT 'wan3.0-video-prime' AS model,
       round((m.value::jsonb ->> 'wan3.0-video-prime')::numeric * 7.3, 4) AS base_cny,
       round((m.value::jsonb ->> 'wan3.0-video-prime')::numeric * 7.3
             * (p.value::jsonb #>> '{ratios,wan3.0-video-prime,720P}')::numeric, 4) AS p720,
       round((m.value::jsonb ->> 'wan3.0-video-prime')::numeric * 7.3
             * (p.value::jsonb #>> '{ratios,wan3.0-video-prime,1080P}')::numeric, 4) AS p1080
  FROM options m, options p
 WHERE m.key = 'ModelPrice' AND p.key = 'YouzanWan3Pricing'
UNION ALL
SELECT 'seedance2.5-特惠',
       round((m.value::jsonb ->> 'seedance2.5-特惠')::numeric * 7.3, 4),
       round((m.value::jsonb ->> 'seedance2.5-特惠')::numeric * 7.3
             * (p.value::jsonb #>> '{ratios,seedance2.5-特惠,720P}')::numeric, 4),
       round((m.value::jsonb ->> 'seedance2.5-特惠')::numeric * 7.3
             * (p.value::jsonb #>> '{ratios,seedance2.5-特惠,1080P}')::numeric, 4)
  FROM options m, options p
 WHERE m.key = 'ModelPrice' AND p.key = 'YouzanWan3Pricing';

-- 回滚（如需）：
--   UPDATE options o SET value = b.value
--     FROM _bak_youzan_pricing_20261029b b WHERE o.key = b.key;
--   DROP TABLE _bak_youzan_pricing_20261029b;
