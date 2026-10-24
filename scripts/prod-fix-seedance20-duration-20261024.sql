-- 生产：seedance-2.0（dola）时长锁定 10 秒（2026-10-24 老板定稿），价格不变
--
-- 1) 后端：relay/channel/task/manwu/adaptor.go 的 kindDolaVideo 走
--    resolveFixedDuration(req, model, dolaFixedDuration=10)：
--    缺省 → 下发 10 秒；显式传其它值 → 400 invalid_duration（不静默改档）。
--    ⚠️ 必须先发版再跑本脚本（否则网关还按 5–15 区间放行，卡片先写死会与实际能力不符）。
--
-- 2) 广场卡片文案：5/10/15 秒 → 固定 10 秒（价格文案不动，仍是按次计费）
--
-- 3) 价格：ModelPrice option **不变**（"seedance-2.0": 0.136986301369863 = ¥1.00/次，按次计费与时长无关）
--
-- 幂等：可重复执行。

BEGIN;

-- 1) 广场卡片：时长口径改写
UPDATE models
SET description = 'Seedance 2.0 文/图生视频，固定 10 秒，6 档比例，≤10 张参考图，卡人脸',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'seedance-2.0';

COMMIT;

-- 2) 复核
SELECT model_name, description FROM models WHERE model_name = 'seedance-2.0';
SELECT value::jsonb ->> 'seedance-2.0' AS seedance20_usd_per_call,
       round((value::jsonb ->> 'seedance-2.0')::numeric * 7.3, 4) AS seedance20_cny_per_call
FROM options WHERE key = 'ModelPrice';
