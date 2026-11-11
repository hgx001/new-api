-- ============================================================================
-- 生产：经营统计（成本与收入）新建成本表
-- 日期：2026-11-11
-- 背景：上游充值没有可用账单接口，成本由管理员在管理页手工录入。
--       收入分两套口径，互不替代：
--         · 现金实收 = top_ups.money where status='success'（按 complete_time）
--         · 渠道消耗金额 = quota_data.quota / QuotaPerUnit * USDExchangeRate
--       本脚本只建一张旁路表 channel_costs，不改任何现有表、不碰计费/充值链路。
--       金额以「分」为单位的 bigint 保存；channel_id=0 表示公共成本；
--       status='voided' 表示作废，统计一律只取 status='active'。
--
-- 幂等：CREATE TABLE / CREATE INDEX IF NOT EXISTS，可重复执行。
-- 说明：后端启动时 AutoMigrate 也会建这张表；先手动执行是为了让 schema 变更
--       可审计、可回滚，且避免发版瞬间把 DDL 混进健康检查窗口。
--
-- ============================ 回滚 SQL ============================
--   DROP TABLE IF EXISTS channel_costs;
--   注意：回滚会丢弃全部已录入成本；统计接口在表缺失时会报错，需同步下线路由。
--   现有表（top_ups / quota_data / channels）不做任何变更，回滚不影响资金数据。
-- ============================================================================

\set ON_ERROR_STOP on

BEGIN;

CREATE TABLE IF NOT EXISTS channel_costs (
    id            serial PRIMARY KEY,
    channel_id    integer      NOT NULL DEFAULT 0,
    amount_cents  bigint       NOT NULL DEFAULT 0,
    occurred_at   bigint       NOT NULL DEFAULT 0,
    category      varchar(32)  NOT NULL DEFAULT '',
    reference_no  varchar(64)  NOT NULL DEFAULT '',
    note          text         NOT NULL DEFAULT '',
    status        varchar(16)  NOT NULL DEFAULT '',
    created_by    integer      NOT NULL DEFAULT 0,
    created_at    bigint       NOT NULL DEFAULT 0,
    updated_at    bigint       NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_finance_cost_channel ON channel_costs (channel_id);
CREATE INDEX IF NOT EXISTS idx_finance_cost_time    ON channel_costs (occurred_at);
CREATE INDEX IF NOT EXISTS idx_finance_cost_status  ON channel_costs (status);

COMMIT;

-- ---------------------------------------------------------------------------
-- 复核
-- ---------------------------------------------------------------------------
-- 1) 新表存在
SELECT 'channel_costs' AS object, to_regclass('public.channel_costs') IS NOT NULL AS exists;

-- 2) 列齐全
SELECT column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_name = 'channel_costs'
ORDER BY ordinal_position;

-- 3) 新表应为空（还没有录入任何成本）
SELECT count(*) AS cost_rows FROM channel_costs;

-- 4) 现有资金数据未被本脚本改动（对比发版前后即可）
SELECT (SELECT count(*) FROM top_ups WHERE status = 'success') AS paid_topups,
       (SELECT count(*) FROM quota_data)                        AS quota_data_rows,
       (SELECT count(*) FROM channels)                         AS channels;
