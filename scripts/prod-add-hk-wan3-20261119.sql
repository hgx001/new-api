-- ---------------------------------------------------------------------------
-- 上架 hk-wan3（有赞渠道 17 / Youzan Wan3，2026-11-19）
--
-- 上游：api.youzan666.vip 的 hk-wan3（WAN3 官渠，满血满参，可参考视频）
--   时长 4–30 秒、480p/720p/1080p、画幅 16:9/9:16/1:1/4:3/3:4/auto
--   素材：media 或 images/videos/audios，10 图 / 5 视频 / 5 音频、合计 20
--
-- 定价（老板 2026-11-19 口径）：
--   上游成本 480P 0.11 / 720P 0.21 / 1080P 0.41 ⚡每秒
--   对外售价 480P ¥0.18（基准档）/ 720P ¥0.25（钉死）/ 1080P ¥0.48
--   → ModelPrice（USD/秒，基准档 480P）= 0.18 / 7.3 = 0.024657534246575342
--   → 倍率 480P 1.0 / 720P 1.3888888888888888 / 1080P 2.6666666666666665
--
-- 幂等：可重复执行（模型名/渠道模型列表/abilities/卡片都有存在性判断，
--       价格用 jsonb || 覆盖同名键，不会重复插入）。
-- ---------------------------------------------------------------------------

BEGIN;

-- 1) 渠道 17 的模型列表（channels.models 不会自动同步 abilities，见第 2 步）
UPDATE channels
SET models = CASE
        WHEN models IS NULL OR btrim(models) = '' THEN 'hk-wan3'
        WHEN models LIKE '%hk-wan3%' THEN models
        ELSE models || ',hk-wan3'
    END
WHERE id = 17;

-- 2) 路由（abilities）：default 组 + 渠道优先级 10
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight)
SELECT 'default', 'hk-wan3', 17, true, 10, 0
WHERE NOT EXISTS (
    SELECT 1 FROM abilities
    WHERE "group" = 'default' AND model = 'hk-wan3' AND channel_id = 17
);

-- 3) ModelPrice：基准档 480P ¥0.18/秒
UPDATE options
SET value = (value::jsonb || '{"hk-wan3": 0.024657534246575342}'::jsonb)::text
WHERE key = 'ModelPrice';

-- 4) YouzanWan3Pricing：markup / fixed_prices / ratios 三处都要加
--    （fixed_prices 键是**对外公开名**，与 ModelPrice / EstimateBilling 一致）
UPDATE options
SET value = jsonb_set(
        jsonb_set(
            jsonb_set(
                value::jsonb,
                '{markup}',
                COALESCE(value::jsonb -> 'markup', '{}'::jsonb)
                    || '{"hk-wan3": {"480P": 0.07, "720P": 0.04, "1080P": 0.07}}'::jsonb
            ),
            '{fixed_prices}',
            COALESCE(value::jsonb -> 'fixed_prices', '{}'::jsonb)
                || '{"hk-wan3": {"480P": 0.18, "720P": 0.25, "1080P": 0.48}}'::jsonb
        ),
        '{ratios}',
        COALESCE(value::jsonb -> 'ratios', '{}'::jsonb)
            || '{"hk-wan3": {"480P": 1.0, "720P": 1.3888888888888888, "1080P": 2.6666666666666665}}'::jsonb
    )::text
WHERE key = 'YouzanWan3Pricing';

-- 5) 广场卡片（status=1 上架）
INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time)
SELECT
    'hk-wan3',
    'Wan3.0 视频生成（有赞 hk-wan3 官渠，满血满参）。按秒计费：480P ¥0.18/秒、720P ¥0.25/秒、1080P ¥0.48/秒（基准档 480P）。时长 4-30 整数秒逐秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto（不传默认 16:9）。支持纯文生、首帧 / 首尾帧（尾帧需首帧，且与普通参考素材互斥）和多素材参考：参考图最多 10、参考视频最多 5、参考音频最多 5，合计最多 20 项，均须公网 http(s) URL。带参考视频时按「生成秒数 + 参考视频秒数」计费，且生成时长 ≤15 秒、参考视频合计 ≤15 秒。',
    1, 1,
    EXTRACT(EPOCH FROM now())::bigint,
    EXTRACT(EPOCH FROM now())::bigint
WHERE NOT EXISTS (
    SELECT 1 FROM models WHERE model_name = 'hk-wan3' AND deleted_at IS NULL
);

COMMIT;
