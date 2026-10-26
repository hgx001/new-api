-- 生产：恢复上架 wan2.7-r2v（有赞 r2v，2026-10-26 老板指令）
--
-- 背景：2026-10-25 下架的三个模型里，wan2.7-r2v 今天恢复上架
--       （同批的 wan3.0-1080p-慢速 / wan3.0-480p 仍保持下架）。
--       上游有赞（https://youzan666.vip）模型列表里 wan2.7-r2v 仍在 → 上游能力还在。
--
-- 恢复三处（缺一即「看得见但调不通」或「调得通但没价格」）：
--   ① ModelPrice option：恢复按秒单价 0.013698630136986302（= ¥0.10/秒 ÷ 7.3）
--      —— 原值取自下架时的备份表 _bak_delist_wan3_20261025_options，不手抄。
--   ② 广场卡片：原样恢复下架前的卡片行（同样从备份表 _bak_delist_wan3_20261025_models 取，
--      id=31 未被占用）。
--   ③ channels.models：把 wan2.7-r2v 挂回渠道 17「有赞 Wan3」——这一步**走管理 API**
--      （PUT /api/channel/），它会重建 abilities 并刷新路由缓存；
--      裸 SQL 改 channels.models 不会立刻生效，能力行还得手写。
--
-- 代码侧无需改动：adaptor（relay/channel/task/youzanwan3/）仍完整支持 r2v，
-- model/pricing.go 的广场白名单与 setting/ratio_setting/model_ratio.go 的
-- 默认单价（0.1/7.3）都还在 → 不需要发版。
--
-- ⚠️ 幂等：可重复执行（ModelPrice / 卡片都做了存在性判断）。
--
-- ============================ 回滚 SQL ============================
--   UPDATE options SET value = (value::jsonb - 'wan2.7-r2v')::text
--     WHERE key = 'ModelPrice';
--   DELETE FROM models WHERE model_name = 'wan2.7-r2v';
--   -- channels.models 的回滚走管理 API 把 wan2.7-r2v 摘掉。
-- ============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份当前 ModelPrice（便于回滚）----------
CREATE TABLE IF NOT EXISTS _bak_restore_wan27_r2v_20261026_options AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- ① 恢复按秒单价（值取自下架备份表，不手抄）----------
UPDATE options o
   SET value = jsonb_set(
         o.value::jsonb,
         '{wan2.7-r2v}',
         to_jsonb((SELECT value::jsonb ->> 'wan2.7-r2v'
                     FROM _bak_delist_wan3_20261025_options
                    WHERE key = 'ModelPrice')::numeric),
         true)::text
 WHERE o.key = 'ModelPrice'
   AND NOT (o.value::jsonb ? 'wan2.7-r2v');

-- ---------- ② 恢复广场卡片（原样取下架前备份行）----------
INSERT INTO models (id, model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, created_time, updated_time, name_rule, deleted_at)
SELECT b.id, b.model_name, b.description, b.icon, b.tags, b.vendor_id, b.endpoints,
       b.status, b.sync_official, b.created_time, b.updated_time, b.name_rule, b.deleted_at
  FROM _bak_delist_wan3_20261025_models b
 WHERE b.model_name = 'wan2.7-r2v'
   AND NOT EXISTS (SELECT 1 FROM models m WHERE m.model_name = 'wan2.7-r2v' AND m.deleted_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM models m WHERE m.id = b.id);

COMMIT;

-- ---------- 复核（事务外只读）----------
\echo '--- ① ModelPrice：wan2.7-r2v 单价（应为 0.013698630136986302 = ¥0.10/秒）---'
SELECT value::jsonb ->> 'wan2.7-r2v' AS r2v_usd_per_second,
       round((value::jsonb ->> 'wan2.7-r2v')::numeric * 7.3, 4) AS r2v_cny_per_second
  FROM options WHERE key = 'ModelPrice';

\echo '--- ② 广场卡片：应恢复 1 行 ---'
SELECT id, model_name, status, description FROM models WHERE model_name = 'wan2.7-r2v';

\echo '--- ③ 能力行：此时应为 0 行（渠道 17 的 models 走管理 API 改，会自动重建）---'
SELECT model, channel_id, "group", enabled FROM abilities WHERE model = 'wan2.7-r2v';
