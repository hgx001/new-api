-- 打开「万相官网 → 客美 tj」手动换货开关（2026-11-20）。
--
-- 后台入口：渠道 → 编辑 DashScope-Wan3(12) 或 客美 Kemeitv(32/33) →
-- 高级设置 → 路由与覆盖 →「万相官网 → 客美 tj 换货」（第一张卡）。
-- 接口：POST /api/video-route/policy/wan3-guanwang-tj {"enabled":true}
--
-- 这里直接用 SQL 置位，是因为写的时候没有后台会话；写库后最多 60 秒
-- （SYNC_FREQUENCY）由 SyncOptions 载入内存，不需要重启容器。
-- ⚠️ 双机各有一份 DB，必须两边都执行。

-- 1) 备份当前策略（回滚用）
CREATE TABLE IF NOT EXISTS bak_20261120_wan3_gw_tj AS
SELECT * FROM options WHERE key = 'Seedance25TeHuiRoute30s';

-- 2) 只加一个字段，其余策略原样保留
UPDATE options
   SET value = (value::jsonb || '{"wan3_guanwang_tj_enabled": true}'::jsonb)::text
 WHERE key = 'Seedance25TeHuiRoute30s';

-- 3) 核对
SELECT json_extract_path_text(value::json, 'wan3_guanwang_tj_enabled') AS switch_on,
       json_extract_path_text(value::json, 'enabled')                  AS promo_rules_on
  FROM options
 WHERE key = 'Seedance25TeHuiRoute30s';

-- 回滚：UPDATE options SET value = (value::jsonb || '{"wan3_guanwang_tj_enabled": false}'::jsonb)::text
--        WHERE key = 'Seedance25TeHuiRoute30s';
