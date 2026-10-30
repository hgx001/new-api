-- new-api 生产库：给条件路由加「9:16 → wan3.0-720p-特惠」（2026-10-30 第二轮）
--
-- 背景：老板问「豆包那边没有可用账号，任务会切回来吗」，答案是**不会**
--   （改派后 model 已换成目标名，重试只会同模型重选渠道；seedance-2-5 只有渠道 20）。
--   于是补了两件事：
--     ① 目标渠道可用性预检 —— 没有可用渠道就不路由，请求自然落回源链路；
--     ② 9:16 走 wan3.0-720p-特惠 —— 多一条稳定的官方链路。
--
-- 本脚本只做 ① 的配套数据变更：给已存在的 option **追加**两个字段。
--   ratio_target_model = wan3.0-720p-特惠
--   trigger_ratio      = 9:16
--
-- 定价口径（已与老板核对）：
--   豆包 seedance-2-5            30 秒 ¥3.00（按次）   ← 30 秒优先，仍走这个
--   wan3.0-720p-特惠             按秒 ¥0.20           ← 9:16 且非 30 秒走这个
--   wan3.0-video-官网 720P       按秒 ¥0.45（30 秒 ¥13.50，比上者贵一倍多，未采用）
--
-- ⚠️ **保留 enabled 现值**：老板 2026-10-30 已确认保持开启。
--    所以这里用 `||` 合并而不是整体覆盖 —— 整体覆盖会把开关状态重置成脚本里写的值。
--
-- ⚠️ **执行顺序：必须先发版（aef11c48f 起才认这两个字段），再执行本脚本。**
--    提前写不会报错，只是旧代码读不到（common.Unmarshal 忽略未知字段）。
--
-- ⚠️ 发版后**必须等 ≥60 秒**（model.SyncOptions 周期）再验证，否则内存里还是旧配置。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- =====================================================================
-- ① 备份现有 option
-- =====================================================================
CREATE TABLE IF NOT EXISTS _bak_seedanceroute_20261030b_options (
  key       text PRIMARY KEY,
  value     text,
  backup_at timestamptz DEFAULT now()
);

INSERT INTO _bak_seedanceroute_20261030b_options (key, value)
SELECT key, value FROM options WHERE key = 'Seedance25TeHuiRoute30s'
ON CONFLICT (key) DO UPDATE
  SET value = EXCLUDED.value, backup_at = now();

-- =====================================================================
-- ② 追加比例规则字段（保留原有 enabled / source_model / target_model / trigger_seconds）
-- =====================================================================
UPDATE options
   SET value = (
         value::jsonb || jsonb_build_object(
           'ratio_target_model', 'wan3.0-720p-特惠',
           'trigger_ratio',      '9:16',
           'updated_at',         EXTRACT(EPOCH FROM now())::bigint
         )
       )::text
 WHERE key = 'Seedance25TeHuiRoute30s';

COMMIT;

-- =====================================================================
-- ③ 校验
-- =====================================================================
\echo '--- ① 路由策略（应有 6 个字段，enabled 保持原值）---'
SELECT value::jsonb AS policy FROM options WHERE key = 'Seedance25TeHuiRoute30s';

\echo '--- ② 三个模型的渠道归属（源=17 有赞；豆包=20 漫屋；wan3 两跳=12 DashScope）---'
SELECT a.model, a.channel_id, c.name AS channel_name, c.type, c.status, a.enabled
  FROM abilities a JOIN channels c ON c.id = a.channel_id
 WHERE a.model IN ('seedance2.5-特惠', 'seedance-2-5', 'wan3.0-720p-特惠')
 ORDER BY a.model;

\echo '--- ③ 价格（源/wan3 两跳应 ≈0.0273972602739726 = ¥0.20/秒；豆包应 ≈0.410958904109589 = ¥3.00/次）---'
SELECT e.key, round((e.value::numeric * 7.3)::numeric, 6) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice'
   AND e.key IN ('seedance2.5-特惠', 'seedance-2-5', 'wan3.0-720p-特惠');

-- =====================================================================
-- 回滚 SQL
-- =====================================================================
--
-- 只去掉比例规则（保留 30 秒规则与开关状态）：
-- BEGIN;
-- UPDATE options
--    SET value = (value::jsonb - 'ratio_target_model' - 'trigger_ratio')::text
--  WHERE key = 'Seedance25TeHuiRoute30s';
-- COMMIT;
--
-- 完全恢复本次改动前的配置：
-- BEGIN;
-- UPDATE options o
--    SET value = b.value
--   FROM _bak_seedanceroute_20261030b_options b
--  WHERE o.key = b.key;
-- COMMIT;
