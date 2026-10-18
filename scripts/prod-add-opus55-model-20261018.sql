-- new-api 生产库：上架 opus-5.5（对外售卖名，实际调用 gpt-6-luna）
-- 2026-10-18 老板指令：广场加卡片 opus-5.5，实际转发 gpt-6-luna，
-- 计费 = gpt-6-luna 的 5 倍；卡片备注写「0.1倍率」，纯展示，不影响计费。
--
-- 计费口径（已与老板确认，勿再改）：
--   · modelRatio      = 0.32106（= gpt-6-luna 0.064212 × 5）
--   · completionRatio = 8（与 luna 同口径，保证输入/输出每一类 token 都是 5 倍）
--   · cacheRatio      = 1（luna 无 cache 倍率条目，默认 1，保持一致）
--   · 广场卡片上自动显示的价格 = 0.32106 × 2 × 7.3 ≈ ¥4.69/1M 输入，
--     与备注文案「0.1倍率」并存是已知状态（备注即展示，非计费依据）。
--
-- 前置（顺序不可颠倒，见 AGENTS.md「模型改名/调价的生产步骤」）：
--   ① 先发版：deploy/deploy-backend.sh <sha>
--      —— 本次代码改动：model/pricing.go 白名单放行 opus-5.5（否则广场不可见）、
--         setting/ratio_setting/model_ratio.go 默认倍率 0.32106 + completionRatio 8。
--         先发版可避免「SQL 先上 → 模型可调用但 output 按 completionRatio=1 少收 8 倍」的窗口。
--   ② 再跑本 SQL（单事务 + 备份表，出错即回滚）。
--
-- 生效时延：channels.models 路由缓存 ≤60s（SYNC_FREQUENCY）重建；
--           /v1/models 立即；广场 /api/pricing TTL 1min；
--           options 每 60s 从 DB 同步一次（整表替换内存 map）。
--           ⚠️ 跑完 SQL 后务必等 ≥70s 再做对外验证/放量：这 60 秒内 opus-5.5
--           已有能力但内存里还没有 0.32106，命中 GetModelRatio 兜底 37.5（若
--           SelfUseModeEnabled=true 会按 37.5 倍扣费；关闭时则报「模型价格未配置」）。
--           线上 MEMORY_CACHE_ENABLED=true，路由以 channels.models 为准；
--           若部署未开内存缓存，跑完本 SQL 需重启后端或经管理 API 保存一次
--           luna 渠道，让 abilities 缓存刷新（见 AGENTS.md）。
--
-- 目标渠道：自动定位所有「models 列表含 gpt-6-luna」的启用渠道（官方/二手均覆盖），
--           不写死 id。luna 有几条渠道，opus-5.5 就同样跟几条，路由行为与 luna 一致。

-- ---------- 预检（只读，先看会动哪些渠道）----------
SELECT id, name, type, status, models,
       coalesce(model_mapping, '(空)') AS model_mapping
  FROM channels
 WHERE status = 1
   AND models ~ '(^|,)gpt-6-luna(,|$)'
 ORDER BY id;

BEGIN;

-- ---------- 备份 ----------
-- channels：只备份将被修改的 luna 渠道（回滚整行还原）
CREATE TABLE IF NOT EXISTS _bak_opus55_channels_20261018 AS
  SELECT * FROM channels
   WHERE status = 1 AND models ~ '(^|,)gpt-6-luna(,|$)';

-- abilities：luna 能力行 = 复制模板；opus-5.5 行此时应为空（回滚基线）
CREATE TABLE IF NOT EXISTS _bak_opus55_abilities_20261018 AS
  SELECT * FROM abilities
   WHERE model IN ('gpt-6-luna', 'opus-5.5');

-- models：luna 卡片 = 复制模板；opus-5.5 卡片此时应为空（回滚 = DELETE）
CREATE TABLE IF NOT EXISTS _bak_opus55_models_20261018 AS
  SELECT * FROM models WHERE model_name IN ('gpt-6-luna', 'opus-5.5');

-- options：ModelRatio / CompletionRatio 整行备份（回滚整行还原）
CREATE TABLE IF NOT EXISTS _bak_opus55_options_20261018 AS
  SELECT * FROM options WHERE key IN ('ModelRatio', 'CompletionRatio');

-- ---------- ① channels.models：追加 opus-5.5 ----------
-- 路由源头。只改 abilities 的话，渠道一旦被后台编辑，model.UpdateAbilities
-- 会按 channels.models 删光重建 abilities → 新模型消失。**必须先改这里。**
-- 追加而非覆写：不冲掉执行前别人新增的模型名。
-- 正则守卫保证重复执行不拼出重名（幂等）；opus-5\.5 里的点已转义。
UPDATE channels
   SET models = models || ',opus-5.5'
 WHERE status = 1
   AND models ~ '(^|,)gpt-6-luna(,|$)'
   AND models !~ '(^|,)opus-5\.5(,|$)';

-- ---------- ② channels.model_mapping：合并 opus-5.5 → gpt-6-luna ----------
-- 用户侧看到 opus-5.5，请求体重写成 gpt-6-luna 发给上游（ModelMappedHelper）。
-- jsonb || 合并：保留已有映射键（gpt-5.6-* 等同渠道别名不动），
-- 重复执行时同键覆盖同值，幂等。计费按映射前的原名 opus-5.5 计价，
-- 所以倍率必须配在 opus-5.5 名下（见 ④）。
UPDATE channels
   SET model_mapping = (
         COALESCE(NULLIF(TRIM(coalesce(model_mapping, '')), ''), '{}')::jsonb
         || '{"opus-5.5": "gpt-6-luna"}'::jsonb
       )::text
 WHERE status = 1
   AND models ~ '(^|,)gpt-6-luna(,|$)'
   AND coalesce(model_mapping, '') !~ '"opus-5\.5"';

-- ---------- ③ abilities：按 luna 的能力行复制一行 ----------
-- group/enabled/priority/weight/tag 全部原样继承 luna（分组、权重、负载行为一致）。
-- NOT EXISTS 守卫保证重复执行不产生重复行。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT a."group", 'opus-5.5', a.channel_id, a.enabled, a.priority, a.weight, a.tag
  FROM abilities a
 WHERE a.model = 'gpt-6-luna'
   AND a.channel_id IN (
         SELECT c.id FROM channels c
          WHERE c.status = 1 AND c.models ~ '(^|,)gpt-6-luna(,|$)')
   AND NOT EXISTS (
         SELECT 1 FROM abilities x
          WHERE x.channel_id = a.channel_id
            AND x."group" = a."group"
            AND x.model = 'opus-5.5');

-- ---------- ④ models：模型广场卡片 ----------
-- description = 广场正文 = 老板要的「备注」。按老板要求展示「0.1倍率」；
-- 这是纯展示文案，实际计费倍率是 0.32106（见 ⑤ 与文件头）。
-- icon/tags/vendor_id/endpoints/status/name_rule 全部复制 luna 卡片，
-- 与 gpt-6 家族视觉保持一致；sync_official=1。
INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, created_time, updated_time, name_rule)
SELECT 'opus-5.5',
       'opus-5.5 旗舰模型，0.1倍率。超长上下文，推理与代码能力全面升级，响应迅捷稳定。',
       m.icon, m.tags, m.vendor_id, m.endpoints,
       m.status, 1,
       EXTRACT(EPOCH FROM now())::bigint, EXTRACT(EPOCH FROM now())::bigint, m.name_rule
  FROM models m
 WHERE m.model_name = 'gpt-6-luna' AND m.deleted_at IS NULL
   AND NOT EXISTS (SELECT 1 FROM models WHERE model_name = 'opus-5.5' AND deleted_at IS NULL);

-- ---------- ⑤ options.ModelRatio / CompletionRatio ----------
-- ⚠️ LoadFromJsonStringWithCallback 是**整表替换**：生产 option 里没有的键，
--    代码里的默认值会被冲掉（未命中的模型回落 37.5！）。所以代码默认值之外，
--    必须把 opus-5.5 也插进生产 option，两处同改。
-- ModelRatio：行必然存在（生产 260+ 键），jsonb_set 合并，只增不改动其他键。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{opus-5.5}', to_jsonb(0.32106))::text
 WHERE key = 'ModelRatio'
   AND NOT (value::jsonb ? 'opus-5.5');

-- CompletionRatio：**仅当该键已存在时才合并**。
-- 若生产没有 CompletionRatio 行，说明在用代码默认值（发版后已含 opus-5.5=8），
-- 此时绝不能新建只含一个键的行——整表替换会把其他模型的 completionRatio 全冲掉。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{opus-5.5}', to_jsonb(8))::text
 WHERE key = 'CompletionRatio'
   AND NOT (value::jsonb ? 'opus-5.5');

COMMIT;

-- ---------- 复核（事务外只读，跑完必须逐项核对）----------
\echo '--- luna 渠道（应含 opus-5.5 与映射）---'
SELECT id, name, models, model_mapping
  FROM channels
 WHERE status = 1 AND models ~ '(^|,)opus-5\.5(,|$)'
 ORDER BY id;

\echo '--- abilities（每条 luna 渠道都应多出一行 opus-5.5）---'
SELECT channel_id, model, "group", enabled, priority, weight, tag
  FROM abilities
 WHERE model IN ('gpt-6-luna', 'opus-5.5')
 ORDER BY channel_id, model;

\echo '--- 广场卡片（description 应含「0.1倍率」）---'
SELECT model_name, icon, tags, vendor_id, status, sync_official,
       left(description, 60) AS description_head
  FROM models
 WHERE model_name IN ('gpt-6-luna', 'opus-5.5') AND deleted_at IS NULL;

\echo '--- 倍率（ModelRatio 应为 0.32106 = 5 × 0.064212）---'
SELECT e.key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelRatio' AND e.key IN ('gpt-6-luna', 'opus-5.5');

\echo '--- CompletionRatio（应为 8；无输出说明生产在用代码默认值，属预期）---'
SELECT e.key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'CompletionRatio' AND e.key IN ('gpt-6-luna', 'opus-5.5');

-- =====================================================================
-- 回滚 SQL（仅在上架后发现错误时使用；反向执行）
-- =====================================================================
--
-- BEGIN;
-- -- ⑤ 摘除倍率键（或整行还原备份表）
-- UPDATE options SET value = (value::jsonb - 'opus-5.5')::text WHERE key = 'ModelRatio';
-- UPDATE options SET value = (value::jsonb - 'opus-5.5')::text WHERE key = 'CompletionRatio';
-- -- ④ 摘除广场卡片（备份表 _bak_opus55_models_20261018 中此名为空，直接删）
-- DELETE FROM models WHERE model_name = 'opus-5.5';
-- -- ③ 摘除能力行
-- DELETE FROM abilities WHERE model = 'opus-5.5';
-- -- ②/① 还原渠道 models 与 model_mapping（整行还原备份表，最稳）
-- UPDATE channels c
--    SET models = b.models, model_mapping = b.model_mapping
--   FROM _bak_opus55_channels_20261018 b
--  WHERE c.id = b.id;
-- COMMIT;
--
-- 若备份表被误删，可用最小摘除代替 ②/①：
--   UPDATE channels
--      SET models = array_to_string(array_remove(string_to_array(models, ','), 'opus-5.5'), ',')
--    WHERE models ~ '(^|,)opus-5\.5(,|$)';
--   UPDATE channels
--      SET model_mapping = (model_mapping::jsonb - 'opus-5.5')::text
--    WHERE coalesce(model_mapping, '') !~ '^$' AND model_mapping::jsonb ? 'opus-5.5';
