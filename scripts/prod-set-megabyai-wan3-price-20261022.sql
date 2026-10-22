-- 生产上线：MegaAI 渠道 wan-3.0（2026-10-22）
--
-- ⚠️ 执行顺序：**必须先发版**（后端 commit fe114ed1，megabyai 适配器 + 视频代理分支），
--    再跑本脚本。反序（SQL 先、后端旧）时 abilities 已有新名，/v1/models 会对外列出
--    wan-3.0，但旧二进制的适配器表里没有它 → 调用即 400 invalid_model。
--
-- 计费链路（任务模型）：quota = int(ModelPrice × QuotaPerUnit × 分组倍率) × size
--   - ModelPrice：480p 基准单价（USD 等值）→ 本脚本写 2.4/7.3
--   - size：分辨率倍率 1 / 1.25 / 1.666667 → 代码（megabyai/constants.go）
--   - 没有任何 seconds 倍率：上游按次计费，时长不参与计价
-- 复核值（QuotaPerUnit=500000，Price=7.3）：480P ¥2.40/次、720P ¥3.00/次、1080P ¥4.00/次。
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

-- 1) wan-3.0 480p 基准单价：¥2.40/次 ÷ 7.3 = 0.3287671232876712
UPDATE options
SET value = jsonb_set(value::jsonb, '{wan-3.0}', '0.3287671232876712'::jsonb)::text
WHERE key = 'ModelPrice';

-- 2) 广场卡片（models 表）：新建一条，描述写清三档价与「按次不随时长加价」
INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time)
SELECT 'wan-3.0',
       'Wan3.0 满参视频生成（第三方 MegaAI 渠道）。按次计费：480P ¥2.40/次、720P ¥3.00/次、1080P ¥4.00/次，时长 5/10/15/30 秒（不随时长加价）。支持 10 张参考图、5 段参考视频、5 条参考音频，全比例，提示词 ≤5000 字符。',
       '', 'video', NULL, '', 1, 1, 0, extract(epoch from now())::bigint, extract(epoch from now())::bigint
WHERE NOT EXISTS (SELECT 1 FROM models WHERE model_name = 'wan-3.0');

COMMIT;

-- 3) 复核
SELECT value::jsonb ->> 'wan-3.0' AS wan30_480p_usd_per_call,
       round((value::jsonb ->> 'wan-3.0')::numeric * 7.3, 4) AS wan30_480p_cny_per_call
FROM options WHERE key = 'ModelPrice';
SELECT model_name, description FROM models WHERE model_name = 'wan-3.0';
