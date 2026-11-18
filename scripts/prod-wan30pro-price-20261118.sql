-- ---------------------------------------------------------------------------
-- wan-3.0-pro 调价（2026-11-18）
--
-- 原价（同日上架时）：720P ¥4.50/条、1080P ¥5.50/条
-- 新价（老板 2026-11-18 晚）：720P ¥5.00/条、1080P ¥7.00/条
--
-- 只改两处，其余口径不动：
--   1) options.ModelPrice 的 wan-3.0-pro = 5/7.3（720P 基准档，USD 等值）
--      1080P 的 7/5 倍率在 adaptor（wan30ProResolutionSizeRatio），属代码，
--      随同一个提交发版 —— 只改这里不改代码，1080P 会按 5.5/4.5 算成 ¥6.11。
--   2) models 卡片描述里的价格文案（与教程、渠道契约文档统一为 ¥5.00 / ¥7.00）
--
-- 幂等：可重复执行。备份表带 IF NOT EXISTS，UPDATE 结果收敛到同一值。
-- ---------------------------------------------------------------------------

BEGIN;

-- 1) 备份（只留调价前的第一份快照，重复执行不会覆盖） ----------------------
CREATE TABLE IF NOT EXISTS bak_20261118_wan30pro_price_options AS
SELECT * FROM options WHERE key = 'ModelPrice';

CREATE TABLE IF NOT EXISTS bak_20261118_wan30pro_price_models AS
SELECT * FROM models WHERE model_name = 'wan-3.0-pro';

-- 2) ModelPrice：720P 基准价 5/7.3 = 0.684931506849315 ---------------------
--    ⚠️ 这里只放「按条」的 720P 基准价：不写 per-token、不写按秒口径。
--    1080P 由 EstimateBilling 返回的 size 倍率（7/5）实时算出，不进这张表。
--    精度：必须写 0.684931506849315（float64 的最短往返表示）；
--    多写一位（...151）会落到相邻的另一个 double，与代码里的 5/7.3 不相等。
UPDATE options
SET value = (value::jsonb || jsonb_build_object('wan-3.0-pro', 0.684931506849315))::text
WHERE key = 'ModelPrice';

-- 3) 模型卡片描述（价格文案与代码、教程、渠道契约文档统一） ------------------
UPDATE models
SET description = 'Wan3.0 Pro 高清视频生成（独立上游模型，非 Wan3 标准版别名）。' ||
                  '按条计费：720P ¥5.00/条、1080P ¥7.00/条，价格与生成时长无关。' ||
                  '时长 4-30 秒整数秒可选（不传默认 5 秒）。' ||
                  '画幅仅支持 16:9 / 9:16，输出尺寸 1280x720 / 720x1280 / 1920x1080 / 1080x1920。' ||
                  '参考素材支持图片（最多 10）、视频（最多 5）、音频（最多 5），合计最多 20 项，' ||
                  '均须公网 http(s) URL；平台校验参考图短边 ≥720px、参考视频合计 ≤15 秒、' ||
                  '参考音频合计 ≤15 秒、有参考视频时生成时长 ≤15 秒。' ||
                  '计费可预期，适合批量出片。',
    updated_time = extract(epoch FROM now())::bigint
WHERE model_name = 'wan-3.0-pro'
  AND deleted_at IS NULL;

COMMIT;

-- 4) 验收 -------------------------------------------------------------------
-- 4.1 基准价（USD）与折算人民币（应为 0.684931 / ¥5.0000）
SELECT (value::jsonb ->> 'wan-3.0-pro')::numeric                       AS price_usd,
       round(((value::jsonb ->> 'wan-3.0-pro')::numeric * 7.3), 4)     AS price_cny_720p
FROM options WHERE key = 'ModelPrice';

-- 4.2 1080P 折算（5.0 × 7/5 = ¥7.0000）—— 倍率在代码里，这里只演算，便于对账
SELECT round(((value::jsonb ->> 'wan-3.0-pro')::numeric * 7.3 * 7.0 / 5.0), 4)
           AS price_cny_1080p
FROM options WHERE key = 'ModelPrice';

-- 4.3 卡片描述里的价格文案（应含 ¥5.00 与 ¥7.00，且不含 4.50 / 5.50）
SELECT id, model_name, status,
       position('720P ¥5.00/条' IN description) > 0 AS has_720p_new,
       position('1080P ¥7.00/条' IN description) > 0 AS has_1080p_new,
       position('4.50' IN description) = 0 AND position('5.50' IN description) = 0
           AS no_old_price,
       length(description) AS desc_len
FROM models WHERE model_name = 'wan-3.0-pro';

-- 4.4 渠道与路由未受影响
SELECT id, models FROM channels WHERE id = 17;
SELECT "group", model, channel_id, enabled FROM abilities WHERE model = 'wan-3.0-pro';
