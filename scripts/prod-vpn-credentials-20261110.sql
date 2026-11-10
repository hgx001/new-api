-- ============================================================================
-- 生产：VPN 每订单独立凭据（subscription 模式）建表与加列
-- 日期：2026-11-10
-- 背景：此前所有买家共用同一条 vless 链接（controller/vpn.go 的 vpnNodeKey），
--       分享出去无法单独特赦。改为每个订单独立 UUID + 订阅地址：
--         · vpn_orders 新增 sub_token（订阅寻址）与 client_ip（下单出口 IP）
--         · 新表 vpn_credentials（一单一凭据）与 vpn_credential_access（访问留痕）
--       历史订单 sub_token 为空 → 继续走原来的明文 node_key 路径，不受影响。
--
-- 幂等：全部 IF NOT EXISTS / ADD COLUMN IF NOT EXISTS，可重复执行。
-- 说明：后端启动时 AutoMigrate 也会建这些对象；先手动执行是为了让 schema 变更
--       可审计、可回滚，且避免发版瞬间把 DDL 混进健康检查窗口。
--
-- ============================ 回滚 SQL ============================
--   DROP TABLE IF EXISTS vpn_credential_access;
--   DROP TABLE IF EXISTS vpn_credentials;
--   DROP INDEX IF EXISTS idx_vpn_orders_sub_token;
--   ALTER TABLE vpn_orders DROP COLUMN IF EXISTS sub_token;
--   ALTER TABLE vpn_orders DROP COLUMN IF EXISTS client_ip;
--   注意：回滚前先确认已停止 deploy/vpn-node-agent（否则节点会在下一轮
--   拉到空清单并把 8443 上的托管用户全部移除——老共享 UUID 不受影响）。
-- ============================================================================

\set ON_ERROR_STOP on

BEGIN;

CREATE TABLE IF NOT EXISTS vpn_credentials (
    id          serial PRIMARY KEY,
    order_id    integer NOT NULL DEFAULT 0,
    user_id     integer NOT NULL DEFAULT 0,
    name        varchar(32) NOT NULL DEFAULT '',
    uuid        varchar(64) NOT NULL DEFAULT '',
    status      varchar(20) NOT NULL DEFAULT '',
    create_time bigint  NOT NULL DEFAULT 0,
    revoke_time bigint  NOT NULL DEFAULT 0,
    frozen_time bigint  NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_vpn_credentials_name
    ON vpn_credentials (name);
CREATE INDEX IF NOT EXISTS idx_vpn_credentials_order_id
    ON vpn_credentials (order_id);
CREATE INDEX IF NOT EXISTS idx_vpn_credentials_user_id
    ON vpn_credentials (user_id);

CREATE TABLE IF NOT EXISTS vpn_credential_access (
    id            serial PRIMARY KEY,
    credential_id integer NOT NULL DEFAULT 0,
    name          varchar(32) NOT NULL DEFAULT '',
    ip            varchar(64) NOT NULL DEFAULT '',
    source        varchar(16) NOT NULL DEFAULT '',
    create_time   bigint  NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_vpn_access_cred_time
    ON vpn_credential_access (credential_id, create_time);
CREATE INDEX IF NOT EXISTS idx_vpn_credential_access_name
    ON vpn_credential_access (name);

ALTER TABLE vpn_orders ADD COLUMN IF NOT EXISTS client_ip varchar(64) NOT NULL DEFAULT '';
ALTER TABLE vpn_orders ADD COLUMN IF NOT EXISTS sub_token varchar(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_vpn_orders_sub_token
    ON vpn_orders (sub_token);

COMMIT;

-- ---------------------------------------------------------------------------
-- 复核：下面每条查询都应返回 expected 列里的值
-- ---------------------------------------------------------------------------
-- 1) 新表存在
SELECT 'vpn_credentials' AS object, to_regclass('public.vpn_credentials') IS NOT NULL AS exists
UNION ALL
SELECT 'vpn_credential_access', to_regclass('public.vpn_credential_access') IS NOT NULL;

-- 2) 新列存在且非空默认
SELECT column_name, data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_name = 'vpn_orders' AND column_name IN ('client_ip', 'sub_token')
ORDER BY column_name;

-- 3) 历史订单不受影响：应全部 sub_token = ''，且 node_key 仍有值
SELECT count(*) AS total,
       count(*) FILTER (WHERE sub_token = '') AS legacy_no_token,
       count(*) FILTER (WHERE coalesce(node_key, '') <> '') AS has_node_key
FROM vpn_orders;

-- 4) 已付款订单里不应出现「有 token 却没凭据」的半成品
SELECT count(*) AS paid_with_token_without_credential
FROM vpn_orders o
WHERE o.status = 'success'
  AND coalesce(o.sub_token, '') <> ''
  AND NOT EXISTS (SELECT 1 FROM vpn_credentials c WHERE c.order_id = o.id);
