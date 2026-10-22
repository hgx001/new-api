-- 生产上线：MegaAI 渠道别名模型 seedance2.5-特惠（2026-10-22）
--
-- ⚠️ 执行顺序：**必须先发版**（后端 commit 含 megabyai 适配器的别名支持 + 广场白名单），
--    再跑本脚本。反序（SQL 先、后端旧）时 abilities 已有新名，/v1/models 会对外列出
--    seedance2.5-特惠，但旧二进制的适配器集合里没有它 → 调用即 400 invalid_model。
--
-- 计费链路（任务模型）：quota = int(ModelPrice × QuotaPerUnit × 分组倍率)
--   - ModelPrice：固定按次单价（USD 等值）→ 本脚本写 5.0/7.3
--   - 没有任何 size / seconds 倍率：megabyai 适配器对该模型 EstimateBilling 返回 nil
-- 复核值（QuotaPerUnit=500000，Price=7.3）：¥5.00/条，任意分辨率、任意时长同价。
--
-- 上游仍然是 wan-3.0（渠道 24 的 model_mapping 由管理 API 单独配置，不在本脚本内）。
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

-- 1) 固定按次单价：¥5.00/条 ÷ 7.3 = 0.6849315068493151
UPDATE options
SET value = jsonb_set(value::jsonb, '{seedance2.5-特惠}', '0.6849315068493151'::jsonb)::text
WHERE key = 'ModelPrice';

-- 2) 广场卡片（models 表）：描述写清「固定 ¥5.00/条、不随时长加价」
INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints, status, sync_official, name_rule, created_time, updated_time)
SELECT 'seedance2.5-特惠',
       'Seedance 2.5 特惠视频生成（第三方 MegaAI 渠道，Wan3.0 满参版同源）。固定 ¥5.00/条：不分分辨率档、不随时长加价，时长 5/10/15/30 秒同价。支持 10 张参考图、5 段参考视频、5 条参考音频，全比例，提示词 ≤5000 字符。',
       '', 'video', NULL, '', 1, 1, 0, extract(epoch from now())::bigint, extract(epoch from now())::bigint
WHERE NOT EXISTS (SELECT 1 FROM models WHERE model_name = 'seedance2.5-特惠');

COMMIT;

-- 3) 复核
SELECT value::jsonb ->> 'seedance2.5-特惠' AS tehui_usd_per_call,
       round((value::jsonb ->> 'seedance2.5-特惠')::numeric * 7.3, 4) AS tehui_cny_per_call
FROM options WHERE key = 'ModelPrice';
SELECT model_name, description FROM models WHERE model_name = 'seedance2.5-特惠';
