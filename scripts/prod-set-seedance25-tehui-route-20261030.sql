-- new-api 生产库：条件路由开关（2026-10-30）
--
-- 是什么：一份 option（Seedance25TeHuiRoute30s），描述「什么条件下把请求改派到另一个模型」。
--
-- 当前的策略：
--   源模型 seedance2.5-特惠（渠道 17 有赞，按秒 ¥0.20）
--     + 时长 = 30 秒
--     + 只带参考图（带参考视频/音频 → 不路由）
--     + 没有明确指定分辨率
--   → 改派到 seedance-2-5（渠道 20 漫屋，按次 ¥3.00）
--
-- 为什么要这条路由：30 秒按秒计费是 ¥0.20 × 30 = ¥6.00，
-- 而目标链路按次只要 ¥3.00 —— 同样 30 秒，省 50%。
--
-- ⚠️ **默认关闭（enabled=false）**：先观察再开。
--   关着的时候行为与改造前**逐字节一致**（中间件直接放行，不碰请求体）。
--
-- ⚠️ **执行顺序：必须先发版（后端含条件路由中间件），再执行本脚本。**
--    顺序反了不会出错 —— option 写进去没人读而已（`updateOptionMap` 的 case 分支
--    是发版后才有的）；但那就白写了。
--
-- ⚠️ **不要回填 origin_model_name**：计费链查的是 info.OriginModelName
--    （relay/helper/price.go:170），回填成源模型名会按 ¥6.00 计费，
--    路由的省钱效果直接归零。任务属性记「实际执行的模型」是自洽的。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- =====================================================================
-- ① 备份现有 option（如果之前写过）
-- =====================================================================
CREATE TABLE IF NOT EXISTS _bak_seedanceroute_20261030_options (
  key         text PRIMARY KEY,
  value       text,
  backup_at   timestamptz DEFAULT now()
);

INSERT INTO _bak_seedanceroute_20261030_options (key, value)
SELECT key, value FROM options WHERE key = 'Seedance25TeHuiRoute30s'
ON CONFLICT (key) DO UPDATE
  SET value = EXCLUDED.value, backup_at = now();

-- =====================================================================
-- ② 写入路由策略（默认关闭）
-- =====================================================================
INSERT INTO options (key, value)
VALUES (
  'Seedance25TeHuiRoute30s',
  jsonb_build_object(
    'enabled',         false,           -- ← 总开关。观察期后再改成 true
    'source_model',    'seedance2.5-特惠',
    'target_model',    'seedance-2-5',
    'trigger_seconds', 30,
    'updated_at',      EXTRACT(EPOCH FROM now())::bigint
  )::text
)
ON CONFLICT (key) DO UPDATE
  SET value = EXCLUDED.value;

COMMIT;

-- =====================================================================
-- ③ 校验
-- =====================================================================
\echo '--- ① 路由开关状态（enabled 应为 false，观察期后再开）---'
SELECT value::jsonb AS policy FROM options WHERE key = 'Seedance25TeHuiRoute30s';

\echo '--- ② 两个模型的渠道与能力（源=17 有赞，目标=20 漫屋）---'
SELECT id, name, type, status FROM channels WHERE id IN (17, 20);

\echo '--- ③ 目标模型的能力行（应 enabled=t）---'
SELECT "group", model, channel_id, enabled FROM abilities
 WHERE model IN ('seedance2.5-特惠', 'seedance-2-5') ORDER BY model;

\echo '--- ④ 价格（源应 ≈0.0273972602739726 = ¥0.20/秒；目标应 ≈0.410958904109589 = ¥3.00/次）---'
SELECT e.key, round((e.value::numeric * 7.3)::numeric, 6) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key IN ('seedance2.5-特惠', 'seedance-2-5');

-- =====================================================================
-- 回滚 SQL
-- =====================================================================
--
-- BEGIN;
-- -- 关闭路由（推荐）：行为立刻回到改造前，不删配置。
-- UPDATE options
--    SET value = jsonb_set(value::jsonb, '{enabled}', 'false'::jsonb)::text
--  WHERE key = 'Seedance25TeHuiRoute30s';
-- COMMIT;
--
-- 开启路由（观察期结束后）：
-- BEGIN;
-- UPDATE options
--    SET value = jsonb_set(
--            jsonb_set(value::jsonb, '{enabled}', 'true'::jsonb),
--            '{updated_at}', to_jsonb(EXTRACT(EPOCH FROM now())::bigint))::text
--  WHERE key = 'Seedance25TeHuiRoute30s';
-- COMMIT;
--
-- 彻底删除：
--   DELETE FROM options WHERE key = 'Seedance25TeHuiRoute30s';
