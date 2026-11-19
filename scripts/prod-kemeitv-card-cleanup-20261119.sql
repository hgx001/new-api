-- ---------------------------------------------------------------------------
-- 客美 Kemeitv 两个万相特价变体的**模型卡片文案清理**（2026-11-19）
--
-- 背景：用户要求卡片描述里**不要出现价格与渠道信息**——
-- 去掉「（客美独立上游模型，非 Wan3 标准版别名）」这类会暴露货源、
-- 又对客户没有决策价值的措辞。描述只留**客户用得上的信息**：
-- 档位价、时长、画幅、参考素材上限与格式约束、场景互斥。
--
-- ⚠️ 为什么不写「非 Wan3 标准版别名」：
-- 客户看到的是商品本身，不需要知道本站内部货源怎么分。
-- 渠道名（客美/Kemeitv）属于上游身份，按站内惯例一律不外露。
--
-- 幂等：可重复执行（用 replace + 存在性判断）。
-- ---------------------------------------------------------------------------

BEGIN;

-- 1) 去掉 tj 描述里的「（客美独立上游模型，非 Wan3 标准版别名）」 ----------
UPDATE models
SET description = replace(
        description,
        '（客美独立上游模型，非 Wan3 标准版别名）',
        ''
    ),
    updated_time = extract(epoch FROM now())::bigint
WHERE model_name = 'wan3.0-video-tj'
  AND deleted_at IS NULL
  AND description LIKE '%（客美独立上游模型，非 Wan3 标准版别名）%';

-- 2) 去掉 prime-tj 描述里的「（客美独立上游模型，非 Wan3 prime 版别名）」 -----
UPDATE models
SET description = replace(
        description,
        '（客美独立上游模型，非 Wan3 prime 版别名）',
        ''
    ),
    updated_time = extract(epoch FROM now())::bigint
WHERE model_name = 'wan3.0-video-prime-tj'
  AND deleted_at IS NULL
  AND description LIKE '%（客美独立上游模型，非 Wan3 prime 版别名）%';

COMMIT;

-- 3) 验收：确认描述里已无「客美」字样，且档位价仍在 ----------------------
SELECT
    model_name,
    status,
    (description LIKE '%客美%')      AS still_has_channel,
    (description LIKE '%Kemeitv%')   AS still_has_brand,
    (description LIKE '%¥0.138/秒%'
       OR description LIKE '%¥0.24/秒%') AS has_base_price,
    description
FROM models
WHERE model_name IN ('wan3.0-video-tj', 'wan3.0-video-prime-tj')
  AND deleted_at IS NULL
ORDER BY model_name;
