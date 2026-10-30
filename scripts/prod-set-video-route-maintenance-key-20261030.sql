-- 条件路由运维开关：写入共享维护密钥（2026-10-30）
--
-- 是什么：一个 option（VideoRouteMaintenanceKey），作为「暂停 / 恢复『30 秒 → 豆包』
-- 路由规则」这个**单用途**接口的共享密钥。
--
-- 为什么不用管理员令牌：这个接口的调用方是客户端安装包（每个操作员机器上一份）。
-- 把管理员凭据放进安装包，泄露一次等于交出整个后台；而这里真正需要的权限只有
-- 一件事 —— 暂停/恢复一条路由规则。密钥可随时轮换，不涉及任何用户账号。
--
-- 空值（或 option 不存在）= 运维接口关闭，访问返回 404。
--
-- 执行：在服务器上
--   docker exec -i postgres psql -U root -d new-api -v ON_ERROR_STOP=1 -f - < 本文件
--
-- ⚠️ 执行后必须把打印出来的密钥填进客户端（二选一）：
--   ① userData/data/settings.json 里的 "routeMaintenanceKey"；
--   ② 启动客户端时带环境变量 NEW_API_MAINTENANCE_KEY=<密钥>。
-- 两边不一致 → 客户端拿到 401「网关维护密钥不正确」，本地闸门仍然生效（只是不联动）。
--
-- 轮换：重跑本文件（会重新生成），然后更新所有客户端的值。
--   轮换期间旧密钥立即失效，正在用的客户端会退化成「仅本地生效」——
--   这是有意的：宁可少一次联动，也不留一个旧密钥长期有效。

BEGIN;

-- 备份现状（重复执行时保留**第一次**的原始值）
CREATE TABLE IF NOT EXISTS _bak_video_route_maintenance_key_20261030 AS
SELECT * FROM options WHERE key = 'VideoRouteMaintenanceKey';

-- 生成 32 位十六进制随机密钥（128 bit，md5 无需 pgcrypto 扩展）。
-- ON CONFLICT DO NOTHING：已存在就不覆盖 —— 避免误重跑把客户端正在用的密钥换掉。
INSERT INTO options (key, value)
VALUES (
  'VideoRouteMaintenanceKey',
  md5(random()::text || clock_timestamp()::text || 'video-route-maintenance')
)
ON CONFLICT (key) DO NOTHING;

COMMIT;

-- 打印结果（把这个 value 填进客户端）
SELECT key, value AS maintenance_key, length(value) AS key_len
  FROM options
 WHERE key = 'VideoRouteMaintenanceKey';

-- 轮换时用这两句（取消注释后执行）：
--   DELETE FROM options WHERE key = 'VideoRouteMaintenanceKey';
--   然后重跑本文件。
