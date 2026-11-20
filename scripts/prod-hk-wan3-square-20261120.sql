-- ---------------------------------------------------------------------------
-- hk-wan3 上广场（2026-11-20）
--
-- 与 2026-11-19 的上架脚本区别：
--   1. 双机执行（老机 api.heibaidao.cn / 新机 api2.heibaidao.cn 各自独立 DB）；
--      新机此前**完全没有** hk-wan3（channels.models / abilities / 卡片都没有），
--      所以这里把三处都做成幂等 upsert，两边跑同一份。
--   2. 描述改为「参考视频不额外收费」并补上成片带音轨的说明。
--   3. 价格不变：480P ¥0.18/秒（基准档）/ 720P ¥0.25 / 1080P ¥0.48，
--      ModelPrice = 0.18 / 7.3 = 0.024657534246575342。
--
-- 幂等：可重复执行。
-- ---------------------------------------------------------------------------

BEGIN;

-- 1) 渠道 17 的模型列表
UPDATE channels
SET models = CASE
        WHEN models IS NULL OR btrim(models) = '' THEN 'hk-wan3'
        WHEN models LIKE '%hk-wan3%' THEN models
        ELSE models || ',hk-wan3'
    END
WHERE id = 17;

-- 2) 路由（abilities）：default 组 + 优先级 10
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

-- 4) YouzanWan3Pricing：markup / fixed_prices / ratios
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

-- 5) 广场卡片：不存在则建，存在则更新描述与上架状态
INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time)
SELECT
    'hk-wan3',
    'Wan3.0 视频生成（WAN3 满血满参）。按秒计费：480P ¥0.18/秒、720P ¥0.25/秒、1080P ¥0.48/秒（基准档 480P），参考视频不额外收费。时长 4-30 整数秒逐秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto（不传默认 16:9）。支持纯文生、首帧 / 首尾帧（尾帧需首帧，且与普通参考素材互斥）和多素材参考：参考图最多 10、参考视频最多 5、参考音频最多 5，合计最多 20 项，均须公网 http(s) URL。成片默认带音轨，参考音频用于给声音定调；带参考视频时生成时长与参考视频合计均不超过 15 秒。',
    1, 1,
    EXTRACT(EPOCH FROM now())::bigint,
    EXTRACT(EPOCH FROM now())::bigint
WHERE NOT EXISTS (
    SELECT 1 FROM models WHERE model_name = 'hk-wan3' AND deleted_at IS NULL
);

UPDATE models
SET description = 'Wan3.0 视频生成（WAN3 满血满参）。按秒计费：480P ¥0.18/秒、720P ¥0.25/秒、1080P ¥0.48/秒（基准档 480P），参考视频不额外收费。时长 4-30 整数秒逐秒可选（不传默认 5 秒）。画幅支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto（不传默认 16:9）。支持纯文生、首帧 / 首尾帧（尾帧需首帧，且与普通参考素材互斥）和多素材参考：参考图最多 10、参考视频最多 5、参考音频最多 5，合计最多 20 项，均须公网 http(s) URL。成片默认带音轨，参考音频用于给声音定调；带参考视频时生成时长与参考视频合计均不超过 15 秒。',
    status = 1,
    updated_time = EXTRACT(EPOCH FROM now())::bigint
WHERE model_name = 'hk-wan3' AND deleted_at IS NULL;

COMMIT;
