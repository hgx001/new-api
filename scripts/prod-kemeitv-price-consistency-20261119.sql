-- ---------------------------------------------------------------------------
-- 客美 Kemeitv 两个万相特价变体的**价格 / 描述一致性校验**（2026-11-19）
--
-- 背景：这两个模型 2026-11-19 改过一次价（tj ¥0.138/秒、prime-tj ¥0.24/秒），
-- 改动只落在 options.ModelPrice 与 models.description 两处，代码里的
-- setting/ratio_setting/model_ratio.go 兜底值一度漏改（还写着 ¥0.27 / ¥0.405）。
--
-- ⚠️ 那个漏改不是"无害的注释问题"：
--   - model/option.go 的 InitOptionMap 用 defaultModelPrice **初始化**
--     options.ModelPrice —— 代码默认值会直接写进 DB；
--   - relay/helper/price.go 的 ModelPriceHelperPerCall 在 DB 查不到时
--     回落到 GetDefaultModelPriceMap()。
-- 所以兜底值错了 = DB 被重置/迁移后价格静默回退到旧价。
-- 代码侧已同步修正，本脚本负责**盯住生产侧**别再漂移。
--
-- 本脚本**只读**（纯 SELECT），不改数据：跑一遍就能看出
-- 「ModelPrice 推导出的档位价」与「卡片文案里写的档位价」是否还对得上。
--
-- 幂等：可重复执行。
-- ---------------------------------------------------------------------------

-- 1) ModelPrice 现值（内部按 USD 等值存储，展示层 ×7.3 换回 RMB） -----------
SELECT
    value::jsonb -> 'wan3.0-video-tj'       AS tj_price,
    value::jsonb -> 'wan3.0-video-prime-tj' AS prime_tj_price
FROM options WHERE key = 'ModelPrice';

-- 2) 由 ModelPrice 推导的**实际扣费档位价** ----------------------------------
--    tj 的 720P 是人工特价 ¥0.25/秒（倍率 0.25/0.138），不是基准价 ×2；
--    其余档位走标准倍率 1 : 2 : 4。
SELECT
    'wan3.0-video-tj' AS model,
    round(((value::jsonb ->> 'wan3.0-video-tj')::numeric * 7.3)::numeric, 4)                AS p480,
    round(((value::jsonb ->> 'wan3.0-video-tj')::numeric * 7.3 * (0.25 / 0.138))::numeric, 4) AS p720,
    round(((value::jsonb ->> 'wan3.0-video-tj')::numeric * 7.3 * 4)::numeric, 4)              AS p1080
FROM options WHERE key = 'ModelPrice';

SELECT
    'wan3.0-video-prime-tj' AS model,
    round(((value::jsonb ->> 'wan3.0-video-prime-tj')::numeric * 7.3)::numeric, 4)     AS p480,
    round(((value::jsonb ->> 'wan3.0-video-prime-tj')::numeric * 7.3 * 2)::numeric, 4) AS p720,
    round(((value::jsonb ->> 'wan3.0-video-prime-tj')::numeric * 7.3 * 4)::numeric, 4) AS p1080
FROM options WHERE key = 'ModelPrice';

-- 3) 卡片文案里写的档位价 ---------------------------------------------------
--    期望值（与第 2 段一致）：
--      wan3.0-video-tj       480P ¥0.138/秒、720P ¥0.25/秒、1080P ¥0.552/秒
--      wan3.0-video-prime-tj 480P ¥0.24/秒 、720P ¥0.48/秒 、1080P ¥0.96/秒
SELECT
    model_name,
    status,
    description
FROM models
WHERE model_name IN ('wan3.0-video-tj', 'wan3.0-video-prime-tj')
  AND deleted_at IS NULL;

-- 4) 一致性断言：文案里必须同时出现该模型的三个档位价 -------------------
--    期望 6 行（2 个模型 × 3 个档位）。任何一行 missing = 文案与价格漂移了。
--
-- ⚠️ 必须按 a.model_name = m.model_name 关联：少了这一句就是**笛卡尔积**，
--    会拿 A 的文案去比对 B 的价格串，输出一堆假 missing。
SELECT
    m.model_name,
    a.tier,
    CASE
        WHEN m.description LIKE '%' || a.price || '%' THEN 'ok'
        ELSE 'missing'
    END AS check_result
FROM models m
JOIN (
    VALUES
        ('wan3.0-video-tj',       '480P',  '¥0.138/秒'),
        ('wan3.0-video-tj',       '720P',  '¥0.25/秒'),
        ('wan3.0-video-tj',       '1080P', '¥0.552/秒'),
        ('wan3.0-video-prime-tj', '480P',  '¥0.24/秒'),
        ('wan3.0-video-prime-tj', '720P',  '¥0.48/秒'),
        ('wan3.0-video-prime-tj', '1080P', '¥0.96/秒')
) AS a(model_name, tier, price) ON a.model_name = m.model_name
WHERE m.deleted_at IS NULL
ORDER BY a.model_name, a.tier;

-- 5) 渠道与能力行（模型必须真的能被路由到） -------------------------------
SELECT id, name, type, models FROM channels WHERE type = 71;

SELECT a."group", a.model, a.channel_id, a.enabled
FROM abilities a
JOIN channels c ON c.id = a.channel_id
WHERE c.type = 71
ORDER BY a.model, a."group";
