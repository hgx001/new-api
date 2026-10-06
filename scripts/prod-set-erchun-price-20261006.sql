-- 把 wan3.0-480p 的每秒单价写入生产 ModelPrice option。
--
-- 为什么必须写 option：types.LoadFromJsonStringWithCallback 是
--   m.data = make(map[K]V); Unmarshal(jsonStr, &m.data)
-- 即**整表替换**，不是合并。所以生产 option 里没有的键，代码里的
-- model_ratio.go 默认值会被冲掉，价格变 0（等于免费），且广场不展示。
-- 见 AGENTS.md「生产 ModelPrice option 会覆盖代码默认值」。
--
-- 定价依据：对外 ¥0.28/秒 ÷ USD2RMB(7.3) = 0.03835616438356164 USD/秒。
-- 上游 erchun 成本 0.158 credits/秒（用户确认 1 credit = 1 元），毛利约 43.7%。
-- 按秒计费由 adaptor 的 EstimateBilling 只下发 seconds 倍率实现，不叠 size。

BEGIN;

CREATE TABLE IF NOT EXISTS _bak_modelprice_20261006_erchun AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';

UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan3.0-480p}', to_jsonb(0.28 / 7.3))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'wan3.0-480p');

COMMIT;

SELECT 'wan3.0-480p 单价 USD/秒' AS item,
       (value::jsonb ->> 'wan3.0-480p') AS usd_per_second,
       round(((value::jsonb ->> 'wan3.0-480p')::numeric * 7.3), 4) AS cny_per_second
  FROM options WHERE key = 'ModelPrice';
