-- 生产：seedance2.5-特惠 对外价下调 ¥5.00/条 → ¥3.00/条（2026-10-23）
--
-- 1) ModelPrice option：固定按次单价 5.00/7.3 = 0.6849315068493151
--    改为 3.00/7.3 = 0.41095890410958904
--    ⚠️ 该模型不分分辨率档、不乘时长（EstimateBilling 返回 nil），
--    所以 option 里只有这一个数，改完即生效（无「基准价 + 倍率」两处要对齐）。
--    复核：205479 quota（QuotaPerUnit=500000，USD2RMB=7.3）= ¥3.00/条
--
-- 2) 广场卡片文案同步：¥5.00/条 → ¥3.00/条
--
-- ⚠️ 执行顺序：先发版（代码 defaultModelPrice 同步为 3.0/7.3），再跑本脚本。
--    生产 option 覆盖代码默认值，只改代码线上不变。
--
-- 幂等：可重复执行（每次执行前把当前 ModelPrice 备份进 _bak_modelprice_20261023）。

BEGIN;

-- 0) 备份当前 ModelPrice
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261023 (
    id         serial primary key,
    key        text        NOT NULL,
    value      text        NOT NULL,
    backup_at  timestamptz DEFAULT now()
);
INSERT INTO _bak_modelprice_20261023 (key, value)
SELECT key, value FROM options WHERE key = 'ModelPrice';

-- 1) 固定按次单价：¥3.00/条 ÷ 7.3 = 0.41095890410958904
UPDATE options
SET value = jsonb_set(value::jsonb, '{seedance2.5-特惠}', '0.41095890410958904'::jsonb)::text
WHERE key = 'ModelPrice';

-- 2) 广场卡片：价格文案改成新价
UPDATE models
SET description = 'Seedance 2.5 特惠视频生成。固定 ¥3.00/条：**仅 720p 分辨率、固定 30 秒一条**（不分档、不随时长加价）。支持 10 张参考图、5 段参考视频、5 条参考音频，全比例，提示词 ≤5000 字符。',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'seedance2.5-特惠';

COMMIT;

-- 3) 复核
SELECT value::jsonb ->> 'seedance2.5-特惠' AS tehui_usd_per_call,
       round((value::jsonb ->> 'seedance2.5-特惠')::numeric * 7.3, 4) AS tehui_cny_per_call
FROM options WHERE key = 'ModelPrice';
SELECT model_name, description FROM models WHERE model_name = 'seedance2.5-特惠';
