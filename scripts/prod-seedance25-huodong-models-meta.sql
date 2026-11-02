-- ════════════════════════════════════════════════════════════════════════════
-- seedance2.5-活动 广场元数据补齐（models 表）
-- 生成时间：2026-11-02
-- 背景：上一次变更（abilities/channels/ModelPrice/route）已成功，
--        但 models 表里没有 seedance2.5-活动 的记录，
--        导致 /api/pricing 返回里该模型 description 为空 —— 广场上是一段空白说明。
--
-- 为什么母本要特别处理：
--   母本 seedance2.5-特惠 是**按秒**（720P ¥0.20/秒、1080P ¥0.30/秒、2-30 秒变时长），
--   活动版是**按次 ¥4.5/次 · 固定 30 秒**。
--   两者能力相同但计费口径不同，说明文字必须分别写，不能直接复制母本的文案
--   （复制过去会让客户以为活动版也是按秒计费 → 报价纠纷）。
--
-- 幂等：ON CONFLICT (model_name) DO UPDATE，重复执行安全。
-- ════════════════════════════════════════════════════════════════════════════

BEGIN;

-- 幂等：先查后插/改。
-- ⚠️ 不能用 ON CONFLICT (model_name)：
--    唯一索引是 uk_model_name_delete_at (model_name, deleted_at) 复合索引，
--    且 deleted_at 允许为 NULL —— Postgres 的 NULL 在唯一索引里不参与冲突判定，
--    写 ON CONFLICT (model_name) 会报
--    "there is no unique or exclusion constraint matching the ON CONFLICT specification"。
DO $$
DECLARE
    v_id      bigint;
    v_now     bigint := EXTRACT(EPOCH FROM NOW())::bigint;
    v_desc    text := '高清视频生成（促销档）。按次计费 ¥4.50/次，固定 30 秒、固定 720P 输出，不随时长与分辨率变化。支持文生视频、图生视频与多模态参考，参考素材：图 ≤10 张、视频 ≤5 个、音频 ≤5 条，三类合计 ≤20 项。特点：价格固定、计费可预期，适合批量出片。⚠️ 带参考视频时按「生成 30 秒 + 参考视频总秒数（四舍五入）」合并计算时长。';
BEGIN
    SELECT id INTO v_id FROM models
    WHERE model_name = 'seedance2.5-活动' AND deleted_at IS NULL
    LIMIT 1;

    IF v_id IS NULL THEN
        INSERT INTO models
            (model_name, description, icon, tags, vendor_id, endpoints,
             status, sync_official, created_time, updated_time, name_rule, deleted_at)
        VALUES
            ('seedance2.5-活动', v_desc, '', '', NULL, '',
             1, 0, v_now, v_now, 0, NULL);
        RAISE NOTICE '已插入 seedance2.5-活动 元数据';
    ELSE
        UPDATE models
        SET description   = v_desc,
            status        = 1,
            sync_official = 0,
            updated_time  = v_now
        WHERE id = v_id;
        RAISE NOTICE '已更新 seedance2.5-活动 元数据 (id=%)', v_id;
    END IF;
END $$;

COMMIT;

-- ── 校验 ────────────────────────────────────────────────────────────────
\echo '=== 促销族 models 元数据 ==='
SELECT id, model_name, status, sync_official,
       CASE WHEN description IS NULL OR description = '' THEN '缺失!' ELSE 'OK' END AS desc_status,
       length(description) AS desc_len
FROM models
WHERE model_name IN ('seedance2.5-特惠', 'seedance2.5-活动', 'wan3.0-720p-按次')
ORDER BY model_name;

\echo '=== 确认母本文案未被改动（应仍是按秒口径，含 ¥0.20/秒）==='
SELECT CASE
           WHEN description LIKE '%¥0.20/秒%' THEN 'OK：母本仍是按秒口径'
           ELSE '异常：母本文案被改动了！'
       END AS tehui_desc_check
FROM models WHERE model_name = 'seedance2.5-特惠';

\echo '=== 确认活动版文案是按次口径（含 ¥4.50/次 与 固定 30 秒）==='
SELECT CASE
           WHEN description LIKE '%¥4.50/次%' AND description LIKE '%固定 30 秒%'
             THEN 'OK：活动版是按次固定 30 秒口径'
           ELSE '异常：活动版文案口径不对！'
       END AS huodong_desc_check
FROM models WHERE model_name = 'seedance2.5-活动';
