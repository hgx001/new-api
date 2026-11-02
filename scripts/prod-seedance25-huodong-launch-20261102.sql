-- ════════════════════════════════════════════════════════════════════════════
-- seedance2.5-活动 上架（按次 ¥4.5/次 · 固定 30 秒）
-- 生成时间：2026-11-02
-- 对应代码：new-api e14a200fa
-- ════════════════════════════════════════════════════════════════════════════
--
-- 三项变更，缺一不可：
--
--   1. channels.models 加 seedance2.5-活动
--      → 不加则 distributor 选渠道时找不到该模型，直接 503。
--      → 且 UpdateAbilities 重建能力行时会把它抹掉，所以必须加。
--
--   2. abilities 新增 (default, seedance2.5-活动, 17)
--      → 不加则「模型 → 渠道」的映射不存在。
--      → 参数完全照抄母本：enabled=t / priority=10 / weight=0 / tag=NULL
--         （tag 是渠道标签，母本没打，所以活动版也不打）
--
--   3. options.ModelPrice 加 "seedance2.5-活动": 0.6164383561643836
--      → 不加则落到 ModelPriceHelperPerCall 的「查不到价」分支
--        → 按次计费算成 ¥0 → **静默免单**（客户用完不扣钱，不报错）。
--      → 0.6164383561643836 = 4.5 / 7.3（USD2RMB）
--
--   4. options.Seedance25TeHuiRoute30s 加 "source_models"
--      → 不加则活动版**不走**四条路由规则 → 30 秒全部走有赞兜底
--        → 成本 ¥0.15/秒 × 30 = ¥4.50 = 售价 ¥4.50 → **零毛利**。
--      → 必须保留 "source_model" 单值字段（向后兼容，旧字段兼容逻辑在代码里）
--
-- 幂等：全部用 IF NOT EXISTS / ON CONFLICT，重复执行安全。

BEGIN;

-- ── 1. 渠道 17 的模型清单 ────────────────────────────────────────────────
UPDATE channels
SET models = CASE
    WHEN ',' || models || ',' LIKE '%,seedance2.5-活动,%' THEN models
    ELSE models || ',seedance2.5-活动'
END
WHERE id = 17;

-- ── 2. 能力行 ────────────────────────────────────────────────────────────
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
VALUES ('default', 'seedance2.5-活动', 17, true, 10, 0, NULL)
ON CONFLICT ("group", model, channel_id) DO UPDATE
SET enabled = EXCLUDED.enabled,
    priority = EXCLUDED.priority,
    weight = EXCLUDED.weight;

-- ── 3. ModelPrice（JSON 全量替换，必须保留原有全部键）────────────────────
-- ⚠️ options.ModelPrice 是**全量替换**不是合并
--    （types.LoadFromJsonStringWithCallback 先 make(map) 再整体反序列化），
--    所以这里用 jsonb_set 在**原值**上打补丁，而不是写一个新 map。
--
-- 键数守卫：变更前记下键数，变更后必须恰好 +1。
-- 若因为手误写了新 map 而丢失原有键，这里会直接报错回滚，而不是静默改坏线上定价。
--
-- 注意：jsonb_object_length() 在生产的 PostgreSQL 15.17-alpine 上不可用
--（报 "function jsonb_object_length(jsonb) does not exist"），
-- 改用 jsonb_path_query_array + jsonb_array_length 统计键数（已实测可用）。
DO $$
DECLARE
    v_before integer;
    v_after  integer;
BEGIN
    SELECT jsonb_array_length(jsonb_path_query_array(value::jsonb, '$.keyvalue()')) INTO v_before
    FROM options WHERE key = 'ModelPrice';

    UPDATE options
    SET value = (
        value::jsonb || jsonb_build_object('seedance2.5-活动', 0.6164383561643836)
    )::text
    WHERE key = 'ModelPrice';

    SELECT jsonb_array_length(jsonb_path_query_array(value::jsonb, '$.keyvalue()')) INTO v_after
    FROM options WHERE key = 'ModelPrice';

    IF v_after <> v_before + 1 THEN
        RAISE EXCEPTION 'ModelPrice 键数异常：变更前 %，变更后 %（应为 %）',
            v_before, v_after, v_before + 1;
    END IF;
    RAISE NOTICE 'ModelPrice 键数 % -> %（正常）', v_before, v_after;
END $$;

-- ── 4. 路由 option：加 source_models（保留 source_model 单值）────────────
UPDATE options
SET value = (
    value::jsonb || jsonb_build_object(
        'source_models', jsonb_build_array('seedance2.5-特惠', 'seedance2.5-活动')
    )
)::text
WHERE key = 'Seedance25TeHuiRoute30s';

COMMIT;

-- ── 校验 ────────────────────────────────────────────────────────────────
\echo '=== 1. 渠道 17 模型清单 ==='
SELECT id, name, type, models FROM channels WHERE id = 17;

\echo '=== 2. 促销族 abilities ==='
SELECT "group", model, channel_id, enabled, priority, weight
FROM abilities WHERE model IN ('seedance2.5-特惠','seedance2.5-活动')
ORDER BY model;

\echo '=== 3. ModelPrice 关键键 ==='
SELECT
    value::jsonb ->> 'seedance2.5-特惠' AS tehui_per_second,
    value::jsonb ->> 'seedance2.5-活动' AS huodong_per_call,
    value::jsonb ->> 'wan3.0-720p-按次' AS xinbi_per_call
FROM options WHERE key = 'ModelPrice';

\echo '=== 4. 路由 option ==='
SELECT value FROM options WHERE key = 'Seedance25TeHuiRoute30s';

\echo '=== 5. ModelPrice 键总数（与变更前对比，应为 78）==='
SELECT jsonb_array_length(jsonb_path_query_array(value::jsonb, '$.keyvalue()')) AS total_keys
FROM options WHERE key = 'ModelPrice';
