-- new-api 生产库：规则二（9:16）改派到心笔 480P 内链（2026-11-11）
--
-- 背景：老板要求
--   ① 删掉「规则四 提示词 >5000 字符 → 豆包」（代码侧已删除，本脚本不涉及）；
--   ② 规则二 9:16 直接路由到心笔渠道的 480P 模型。
--
-- 心笔渠道 25 上挂了三个模型：
--   wan3.0-720p-按次        （按次固定 30 秒，不适合 5/10/15 秒的 9:16 请求）
--   seedance2.5-活动        （促销族源模型，不能路由到自己）
--   wan3.0-720p-特惠-内链   （按秒透传 2–30 秒，480P 生成 + 交付 720P）★选它
--
-- 所以只改 option 的两个字段：
--   trigger_ratio      = 9:16
--   ratio_target_model = wan3.0-720p-特惠-内链
--
-- ⚠️ 执行顺序：**必须先发版**（commit c67e9cc4f 起才认这个目标模型并用心笔
--    契约改写请求体），再执行本脚本。旧二进制会把请求改派成内链模型、
--    却按 wan3 官网格式改写 body，参考图会被静默丢弃。
--
-- ⚠️ 用 `||` 合并而不是整体覆盖：保留 enabled / source_models /
--    source_overrides / failover_* / pause_* / updated_at 等现有字段与状态。
--
-- ⚠️ 发版后 options 由 SyncOptions 每 60s 从库里重载；执行完等 ≥90s
--    再验内存态（/api/video-route/maintenance/doubao 的 ratio_rule_enabled）。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ① 备份现有 option
CREATE TABLE IF NOT EXISTS _bak_seedanceroute_20261111_options (
  key       text PRIMARY KEY,
  value     text,
  backup_at timestamptz DEFAULT now()
);

INSERT INTO _bak_seedanceroute_20261111_options (key, value)
SELECT key, value FROM options WHERE key = 'Seedance25TeHuiRoute30s'
ON CONFLICT (key) DO UPDATE
  SET value = EXCLUDED.value, backup_at = now();

-- ② 追加/更新比例规则字段
UPDATE options
   SET value = (
         value::jsonb || jsonb_build_object(
           'ratio_target_model', 'wan3.0-720p-特惠-内链',
           'trigger_ratio',      '9:16',
           'updated_at',         EXTRACT(EPOCH FROM now())::bigint
         )
       )::text
 WHERE key = 'Seedance25TeHuiRoute30s';

COMMIT;

-- ③ 校验
\echo '--- 路由策略（应有 trigger_ratio / ratio_target_model）---'
SELECT value::jsonb AS policy FROM options WHERE key = 'Seedance25TeHuiRoute30s';

\echo '--- 目标模型的渠道归属（应为 ch25 心笔，enabled）---'
SELECT a.model, a.channel_id, c.name, c.type, c.status, a.enabled
  FROM abilities a JOIN channels c ON c.id = a.channel_id
 WHERE a.model = 'wan3.0-720p-特惠-内链';

-- =====================================================================
-- 回滚
-- =====================================================================
-- 只去掉比例规则（保留其余全部字段与状态）：
-- BEGIN;
-- UPDATE options
--    SET value = (value::jsonb - 'ratio_target_model' - 'trigger_ratio')::text
--  WHERE key = 'Seedance25TeHuiRoute30s';
-- COMMIT;
--
-- 完全恢复本次改动前：
-- BEGIN;
-- UPDATE options o SET value = b.value
--   FROM _bak_seedanceroute_20261111_options b
--  WHERE o.key = b.key;
-- COMMIT;
