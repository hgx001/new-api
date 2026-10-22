-- 生产回退：把误改的 wan3.0-video 基准价还原（2026-10-22 用户要求「只改 wan3.0-video-官网，别的不动」）
--
-- ⚠️ 执行顺序：**必须先发版**（后端 commit 见本次回退提交：分辨率倍率改为**按对外模型名分表**，
--    官网渠道 1/2.25/4，其余模型名 1/2/4），再跑本脚本。
--    先跑本脚本不改代码 → wan3.0-video 会用官网的 2.25 倍率，720P 变成 ¥0.6075/秒（错价）。
--
-- 背景：2026-10-22 调价只授权了官网渠道模型名 wan3.0-video-官网
--   （480P ¥0.20/秒、720P ¥0.45/秒、1080P ¥0.80/秒）。
--   同批操作误把 wan3.0-video 的 ModelPrice 也改成了 0.0273972602739726，
--   而它原值是 0.0369863（480P ¥0.27/秒、720P ¥0.54/秒、1080P ¥1.08/秒）。
--   wan3.0-video-prime（¥0.405/秒）只在代码默认值里被误改，生产 ModelPrice 本就是
--   0.038356164383561646（¥0.28/秒），未受影响，本脚本不动它。
--
-- 说明：wan3.0-video 在生产 abilities 表里**没有绑定任何渠道**（调不通），
--   所以本次回退不改变任何在跑服务的计费，只是把口径还原到调价前。
--
-- 幂等：可重复执行（每次执行前把当前 ModelPrice 备份进 _bak_modelprice_20261022b）。

BEGIN;

-- 0) 备份当前 ModelPrice
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261022b (
    id         serial primary key,
    key        text        NOT NULL,
    value      text        NOT NULL,
    backup_at  timestamptz DEFAULT now()
);
INSERT INTO _bak_modelprice_20261022b (key, value)
SELECT key, value FROM options WHERE key = 'ModelPrice';

-- 1) 还原标准版基准价：¥0.27/秒 ÷ 7.3 = 0.0369863（截断值，与调价前一致）
UPDATE options
SET value = jsonb_set(value::jsonb, '{wan3.0-video}', '0.0369863'::jsonb)::text
WHERE key = 'ModelPrice';

COMMIT;

-- 2) 复核
SELECT 'wan3.0-video (标准版，应为 0.0369863 → ¥0.27/秒)' AS item,
       value::jsonb ->> 'wan3.0-video' AS model_price_usd,
       round((value::jsonb ->> 'wan3.0-video')::numeric * 7.3, 4) AS cny_per_second
FROM options WHERE key = 'ModelPrice'
UNION ALL
SELECT 'wan3.0-video-官网 (应为 0.0273972602739726 → ¥0.20/秒)',
       value::jsonb ->> 'wan3.0-video-官网',
       round((value::jsonb ->> 'wan3.0-video-官网')::numeric * 7.3, 4)
FROM options WHERE key = 'ModelPrice'
UNION ALL
SELECT 'wan3.0-video-prime (未动)',
       value::jsonb ->> 'wan3.0-video-prime',
       round((value::jsonb ->> 'wan3.0-video-prime')::numeric * 7.3, 4)
FROM options WHERE key = 'ModelPrice';

-- 3) 其余 wan 模型的价必须保持不变
SELECT k, value::jsonb ->> k AS model_price_usd
FROM options, LATERAL jsonb_each_text(value::jsonb) j(k, v)
WHERE key = 'ModelPrice' AND k LIKE 'wan%'
ORDER BY k;
