-- new-api 生产库：取消 seedance2.5-活动 的两条路由（2026-11-11）
--
-- 需求：活动版
--   ① 不再 9:16 → wan3.0-720p-特惠-内链（ratio_rule_enabled=false）；
--   ② 被规则一改派到豆包后，不再二次降级到 wan3.0-班车
--      （failover_after_doubao_enabled=false）。
--
-- 只针对 seedance2.5-活动：母本 seedance2.5-特惠 的 9:16 规则本来已单独关闭，
-- 其「豆包 → 班车」降级链、以及客户直接请求 seedance-2-5 的降级全部保持不变。
--
-- ⚠️ 执行顺序：**必须先发版**（5e2746267 起才认 failover_after_doubao_enabled）。
--    旧二进制会忽略新字段，活动版仍会降级到班车。
-- ⚠️ 用 || 合并 source_overrides 里的活动版对象，保留 target_model 等既有字段。
-- ⚠️ options 由 SyncOptions 每 60s 从库里重载；执行完等 ≥90s 再生效。

BEGIN;

-- ① 备份现有 option
CREATE TABLE IF NOT EXISTS _bak_seedanceroute_20261111_huodong_options (
  key       text PRIMARY KEY,
  value     text,
  backup_at timestamptz DEFAULT now()
);

INSERT INTO _bak_seedanceroute_20261111_huodong_options (key, value)
SELECT key, value FROM options WHERE key = 'Seedance25TeHuiRoute30s'
ON CONFLICT (key) DO UPDATE
  SET value = EXCLUDED.value, backup_at = now();

-- ② 只合并活动版的覆盖对象，其余字段原样保留
UPDATE options
   SET value = (
         jsonb_set(
           value::jsonb,
           '{source_overrides}',
           COALESCE(value::jsonb -> 'source_overrides', '{}'::jsonb)
             || jsonb_build_object(
                  'seedance2.5-活动',
                  COALESCE(value::jsonb #> '{source_overrides,seedance2.5-活动}', '{}'::jsonb)
                    || jsonb_build_object(
                         'ratio_rule_enabled', false,
                         'failover_after_doubao_enabled', false
                       )
                ),
           true
         )
         || jsonb_build_object('updated_at', EXTRACT(EPOCH FROM now())::bigint)
       )::text
 WHERE key = 'Seedance25TeHuiRoute30s';

COMMIT;

-- ③ 校验：活动版覆盖对象应有两个 false，母本覆盖不应被改动
\echo '--- 路由策略 ---'
SELECT value::jsonb AS policy FROM options WHERE key = 'Seedance25TeHuiRoute30s';

-- =====================================================================
-- 回滚（只去掉本次新增的两个开关，保留其余全部字段与状态）
-- =====================================================================
-- BEGIN;
-- UPDATE options
--    SET value = (
--          jsonb_set(
--            value::jsonb,
--            '{source_overrides,seedance2.5-活动}',
--            (value::jsonb #> '{source_overrides,seedance2.5-活动}')
--              - 'ratio_rule_enabled'
--              - 'failover_after_doubao_enabled',
--            true
--          )
--          || jsonb_build_object('updated_at', EXTRACT(EPOCH FROM now())::bigint)
--        )::text
--  WHERE key = 'Seedance25TeHuiRoute30s';
-- COMMIT;
--
-- 完全恢复本次改动前：
-- BEGIN;
-- UPDATE options o SET value = b.value
--   FROM _bak_seedanceroute_20261111_huodong_options b
--  WHERE o.key = b.key;
-- COMMIT;
