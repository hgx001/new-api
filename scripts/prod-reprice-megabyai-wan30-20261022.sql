-- 生产：wan-3.0 档位价上调 + seedance2.5-特惠 卡片文案精简（2026-10-22 晚）
--
-- 1) wan-3.0 对外价：480p ¥2.4 → ¥3.0 / 720p ¥3.0 → ¥3.5 / 1080p ¥4.0（不变）
--    基准价（480p）写进 ModelPrice option：3.0/7.3 = 0.4109589041095891
--    720p / 1080p 的档位价由代码里的分辨率倍率（3.5/3.0、4.0/3.0）算出来，
--    option 里只存基准价 —— 所以这里只改一个数。
--    复核：480P 205479 quota(¥3.00) / 720P 239725 quota(¥3.50) / 1080P 273972 quota(¥4.00)
--
-- 2) 广场卡片同步：
--    - wan-3.0 卡片价格文案改成新档位
--    - seedance2.5-特惠 卡片去掉「第三方 MegaAI 渠道，Wan3.0 满参版同源」这句
--
-- ⚠️ 执行顺序：必须先发版（代码里的倍率表与默认值），再跑本脚本。
--    反序时 option 已用新基准价、代码还是旧倍率 → 计费与下发档位对不上。
--
-- 幂等：可重复执行（每次执行前把当前 ModelPrice 备份进 _bak_modelprice_20261022c）。

BEGIN;

-- 0) 备份当前 ModelPrice
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261022c (
    id         serial primary key,
    key        text        NOT NULL,
    value      text        NOT NULL,
    backup_at  timestamptz DEFAULT now()
);
INSERT INTO _bak_modelprice_20261022c (key, value)
SELECT key, value FROM options WHERE key = 'ModelPrice';

-- 1) wan-3.0 480p 基准单价：¥3.00/次 ÷ 7.3 = 0.4109589041095891
UPDATE options
SET value = jsonb_set(value::jsonb, '{wan-3.0}', '0.4109589041095891'::jsonb)::text
WHERE key = 'ModelPrice';

-- 2) wan-3.0 广场卡片：价格文案改成新档位
UPDATE models
SET description = 'Wan3.0 满参视频生成（第三方 MegaAI 渠道）。按次计费：480P ¥3.00/次、720P ¥3.50/次、1080P ¥4.00/次，时长 5/10/15/30 秒（不随时长加价）。支持 10 张参考图、5 段参考视频、5 条参考音频，全比例，提示词 ≤5000 字符。',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'wan-3.0';

-- 3) seedance2.5-特惠 广场卡片：去掉「第三方 MegaAI 渠道，Wan3.0 满参版同源」
UPDATE models
SET description = 'Seedance 2.5 特惠视频生成。固定 ¥5.00/条：**仅 720p 分辨率、固定 30 秒一条**（不分档、不随时长加价）。支持 10 张参考图、5 段参考视频、5 条参考音频，全比例，提示词 ≤5000 字符。',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'seedance2.5-特惠';

COMMIT;

-- 4) 复核
SELECT value::jsonb ->> 'wan-3.0' AS wan30_480p_usd_per_call,
       round((value::jsonb ->> 'wan-3.0')::numeric * 7.3, 4) AS wan30_480p_cny_per_call
FROM options WHERE key = 'ModelPrice';
SELECT model_name, description FROM models WHERE model_name IN ('wan-3.0', 'seedance2.5-特惠') ORDER BY model_name;
