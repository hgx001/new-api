-- 生产：管理员令牌（token id=11，name=auto，user=admin）IP 白名单追加运营商网段
-- 日期：2026-10-25
-- 背景：用户本地出口 IP 变成 111.85.26.19（运营商动态 IP），不在白名单 →
--       中间件直接 403「您的 IP 不在令牌允许访问的列表中」，模型请求根本没进模型。
--       用户定稿：放行 111.85.0.0/16 整段（该运营商段内换 IP 不用再改）。
-- 原有条目全部保留（172.18.0.1 / 119.29.253.97 / 106.55.13.250 /
-- 220.197.185.57 / 220.197.181.57 / 220.197.181.34 / 43.167.248.149）。
--
-- 生效：token 走 Redis 缓存（key = token:<HMAC>，TTL = SYNC_FREQUENCY = 60s），
--       裸 SQL 改库不会立刻刷新缓存 → 改完等 ≥60s 生效（或清 token:* 缓存）。
--
-- ============================ 回滚 SQL ============================
--   UPDATE tokens SET allow_ips = (SELECT allow_ips FROM _bak_token11_allow_ips_20261025)
--     WHERE id = 11;   -- 之后等 ≥60s 或清 token:* 缓存
-- ============================================================================

\set ON_ERROR_STOP on

BEGIN;

DROP TABLE IF EXISTS _bak_token11_allow_ips_20261025;
CREATE TABLE _bak_token11_allow_ips_20261025 AS
SELECT id, name, user_id, COALESCE(allow_ips, '') AS allow_ips, key, now() AS backup_at
FROM tokens WHERE id = 11;

UPDATE tokens
SET allow_ips = COALESCE(NULLIF(allow_ips, ''), '') || E'\n111.85.0.0/16'
WHERE id = 11
  AND allow_ips NOT LIKE '%111.85.0.0/16%';

COMMIT;

\echo '--- 结果 ---'
SELECT id, name, user_id, allow_ips FROM tokens WHERE id = 11;
\echo '--- 备份（原值）---'
SELECT id, '[' || allow_ips || ']' AS old_allow_ips FROM _bak_token11_allow_ips_20261025;
