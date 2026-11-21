-- new-api 生产库：下架 wan3.0-720p-特惠（2026-11-21 老板指令）。
--
-- 背景：该 SKU 在上游 有赞 mw-sd2.5-flash 渠道 503 后持续失败；
-- 它在代码里还被 Wan3TehuiAliasRoute 无条件别名成 seedance2.5-特惠，
-- 所以仅删渠道/能力不足以停售 —— 已同步在 middleware/retired_model.go
-- 的 retiredModelReplacements 里加：
--     "wan3.0-720p-特惠": "seedance2.5-特惠"
-- 拦截器挂在 TokenAuth 之后、别名中间件之前（router/video-router.go），
-- 命中返回 400「模型已下线，请使用 seedance2.5-特惠」。
--
-- 下架前核实（2026-11-21 17:4x）：
--   · 只在渠道 12（有赞 Wan3 官网）的 models 与 abilities 里；
--     渠道 25 的「wan3.0-720p-特惠-内链」是另一个 SKU，不能误伤。
--   · 无在途任务（tasks 里 IN_PROGRESS/QUEUED/NOT_START 命中 0）。
--   · 两个 token 的 model_limits 只写了这一个模型（167 视频生成 / 174 面试视频），
--     下架后它们连替代型号都调不了，故同步把 limits 换成 seedance2.5-特惠
--     （两者 ModelPrice 相同，均为 0.0273972602739726）。
--   · 策略 option 里 ratio_target_model / short_target_model 仍指向它，但
--     对应规则对特惠源模型是关闭状态（source_overrides.*_rule_enabled=false，
--     short_link_enabled=false）；本脚本不动 option，避免把改派目标改成自身。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS bak_20261121_delist_wan3720p_channels AS
  SELECT * FROM channels WHERE id = 12;
CREATE TABLE IF NOT EXISTS bak_20261121_delist_wan3720p_abilities AS
  SELECT * FROM abilities WHERE model = 'wan3.0-720p-特惠';
CREATE TABLE IF NOT EXISTS bak_20261121_delist_wan3720p_models AS
  SELECT * FROM models WHERE model_name = 'wan3.0-720p-特惠';
CREATE TABLE IF NOT EXISTS bak_20261121_delist_wan3720p_options AS
  SELECT * FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');
CREATE TABLE IF NOT EXISTS bak_20261121_delist_wan3720p_tokens AS
  SELECT * FROM tokens WHERE model_limits LIKE '%wan3.0-720p-特惠%';

-- ---------- ① 从渠道 models 列表摘掉（精确名，不碰 -内链）----------
UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'wan3.0-720p-特惠'), ',')
 WHERE models ~ '(^|,)wan3\.0-720p-特惠(,|$)';

-- ---------- ② 删能力行 ----------
DELETE FROM abilities WHERE model = 'wan3.0-720p-特惠';

-- ---------- ③ 删模型广场卡片 ----------
DELETE FROM models WHERE model_name = 'wan3.0-720p-特惠';

-- ---------- ④ 摘价格键 ----------
UPDATE options
   SET value = (value::jsonb - 'wan3.0-720p-特惠')::text
 WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND value::jsonb ? 'wan3.0-720p-特惠';

-- ---------- ⑤ token 模型限制换成替代型号 ----------
UPDATE tokens
   SET model_limits = regexp_replace(
         model_limits,
         '(^|,)wan3\.0-720p-特惠(,|$)',
         '\1seedance2.5-特惠\2',
         'g')
 WHERE model_limits LIKE '%wan3.0-720p-特惠%';

COMMIT;

-- ---------- 复核（事务外只读）----------
\echo '--- ① 渠道 12 应只剩 wan3.0-video-官网 / wan3.0-720p-官网；渠道 25 的 -内链 必须保留 ---'
SELECT id, models FROM channels WHERE id IN (12, 25) ORDER BY id;

\echo '--- ② 能力表必须 0 行 ---'
SELECT model, channel_id, "group", enabled FROM abilities WHERE model = 'wan3.0-720p-特惠';

\echo '--- ③ 卡片必须 0 行 ---'
SELECT id, model_name FROM models WHERE model_name = 'wan3.0-720p-特惠';

\echo '--- ④ 价格键必须 0 行 ---'
SELECT o.key AS opt_key, e.key AS model_key
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND e.key = 'wan3.0-720p-特惠';

\echo '--- ⑤ token limits（167/174 应显示 seedance2.5-特惠）---'
SELECT id, name, model_limits FROM tokens WHERE id IN (167, 174);

-- =====================================================================
-- 回滚
-- =====================================================================
-- BEGIN;
-- UPDATE channels c SET models = b.models FROM bak_20261121_delist_wan3720p_channels b WHERE c.id = b.id;
-- INSERT INTO abilities SELECT * FROM bak_20261121_delist_wan3720p_abilities ON CONFLICT DO NOTHING;
-- INSERT INTO models SELECT * FROM bak_20261121_delist_wan3720p_models ON CONFLICT (id) DO NOTHING;
-- UPDATE options o SET value = b.value FROM bak_20261121_delist_wan3720p_options b WHERE o.key = b.key;
-- UPDATE tokens t SET model_limits = b.model_limits FROM bak_20261121_delist_wan3720p_tokens b WHERE t.id = b.id;
-- COMMIT;
-- 代码侧回滚：revert retired_model.go 的对应 map 行并重新发版（否则仍会被 400 拦截）。
