-- 生产调价：wan3.0-video-官网 → 480P ¥0.20/秒、720P ¥0.45/秒、1080P ¥0.80/秒（2026-10-22 用户拍板）
--
-- ⚠️ 执行顺序：**必须先发版**（后端 commit c2c5f262，把 resolutionSizeRatio 改成 1/2.25/4），
--    再跑本脚本。只跑本脚本不改代码 → 720P/1080P 仍按旧倍率 2/4 收，新基准价 × 2/4 ≠ 目标价，
--    会变成 480P ¥0.20、720P ¥0.40、1080P ¥0.80（720P 少收 ¥0.05/秒）。
--
-- 计费链路（任务模型）：quota = int(ModelPrice × QuotaPerUnit × 分组倍率) × seconds × size
--   - ModelPrice：480P 基准单价（USD 等值）→ 本脚本改
--   - seconds：生成秒数 + 参考视频总秒数（四舍五入）→ 代码，不变
--   - size：分辨率倍率 1 / 2.25 / 4 → 代码，c2c5f262
-- 复核值（QuotaPerUnit=500000，Price=7.3）：480P 10 秒=¥2.00、+10 秒参考=¥4.00；
--   720P 10 秒=¥4.50、+10 秒参考=¥9.00；1080P 10 秒=¥8.00、+10 秒参考=¥16.00。
--
-- 幂等：可重复执行（每次执行前把当前 ModelPrice 备份进 _bak_modelprice_20261022）。

BEGIN;

-- 0) 备份当前 ModelPrice
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261022 (
    id         serial primary key,
    key        text        NOT NULL,
    value      text        NOT NULL,
    backup_at  timestamptz DEFAULT now()
);
INSERT INTO _bak_modelprice_20261022 (key, value)
SELECT key, value FROM options WHERE key = 'ModelPrice';

-- 1) 480P 基准单价：¥0.20/秒 ÷ 7.3 = 0.0273972602739726
UPDATE options
SET value = jsonb_set(value::jsonb, '{wan3.0-video-官网}', '0.0273972602739726'::jsonb)::text
WHERE key = 'ModelPrice';
UPDATE options
SET value = jsonb_set(value::jsonb, '{wan3.0-video}', '0.0273972602739726'::jsonb)::text
WHERE key = 'ModelPrice';

-- 2) 广场卡片描述同步新价与新算例（与前端教程同一口径）
UPDATE models
SET description = 'Wan3.0 视频生成。支持 480P（¥0.20/s）、720P（¥0.45/s）、1080P（¥0.80/s），时长 2-30 秒。⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× 同档单价计费，例如 10 秒参考视频 + 生成 10 秒 480P = ¥4.00（不传参考视频只要 ¥2.00）。'
WHERE model_name = 'wan3.0-video-官网';

COMMIT;

-- 3) 复核
SELECT value::jsonb ->> 'wan3.0-video-官网' AS official_480p_usd_per_second,
       value::jsonb ->> 'wan3.0-video'      AS std_480p_usd_per_second,
       round((value::jsonb ->> 'wan3.0-video-官网')::numeric * 7.3, 4) AS official_480p_cny_per_second
FROM options WHERE key = 'ModelPrice';
SELECT model_name, description FROM models WHERE model_name = 'wan3.0-video-官网';
