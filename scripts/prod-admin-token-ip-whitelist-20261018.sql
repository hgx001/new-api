-- ============================================================================
-- 生产：管理员 API key（token id=11，name=auto，user=admin）加 IP 白名单
-- 日期：2026-10-18
-- 需求：老板要求「使用管理员这个 key 的 IP 只能是本机服务器」。
--
-- 白名单取值依据（实测得出，不是猜的）：
--   ① 172.18.0.1    —— docker 网桥网关。容器内看到的「本机直连」来源就是它
--                      （宿主机 127.0.0.1:3000 -> 容器，被 docker SNAT 成网关 IP）。
--                      实测：服务器上 curl http://127.0.0.1:3000 → 需要这一条。
--   ② 119.29.253.97 —— 服务器公网 IP。域名 api.heibaidao.cn 解析到它，
--                      服务器自己访问域名会回环（hairpin），nginx 记到的 remote_addr
--                      就是这个公网 IP。实测：服务器上 curl https://api.heibaidao.cn → 需要这一条。
--
--   不放 127.0.0.1：容器侧永远看不到它（被 SNAT 掉），放了也匹配不到。
--
-- 生效方式：token 走 Redis 缓存（key = token:<HMAC>，TTL = SYNC_FREQUENCY = 60s）。
--   改库后要么等 60s，要么清 token:* 缓存。脚本末尾给出清缓存命令（可选）。
--
-- ============================ 回滚 SQL ============================
--   UPDATE tokens SET allow_ips = (SELECT allow_ips FROM _bak_token11_allow_ips_20261018)
--     WHERE id = 11;
--   -- 然后清缓存：
--   --   docker exec redis sh -c "redis-cli -a <PASS> --no-auth-warning \
--   --     --scan --pattern 'token:*' | xargs -r redis-cli -a <PASS> --no-auth-warning del"
-- ============================================================================

\set ON_ERROR_STOP on

BEGIN;

DROP TABLE IF EXISTS _bak_token11_allow_ips_20261018;
CREATE TABLE _bak_token11_allow_ips_20261018 AS
SELECT id, name, user_id, COALESCE(allow_ips, '') AS allow_ips, key
FROM tokens WHERE id = 11;

UPDATE tokens
SET allow_ips = E'172.18.0.1\n119.29.253.97'
WHERE id = 11;

COMMIT;

\echo '--- 结果 ---'
SELECT id, name, user_id, allow_ips FROM tokens WHERE id = 11;
\echo '--- 备份 ---'
SELECT id, name, '[' || allow_ips || ']' AS old_allow_ips FROM _bak_token11_allow_ips_20261018;
