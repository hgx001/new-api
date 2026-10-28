-- new-api 生产库：彻底下架 wan2.7-r2v（2026-10-28 老板确认）
--
-- 背景：上游有赞 2026-10-27 切换到 V1 协议（/v1/tasks + Idempotency-Key + /v1/assets），
--        /v1/models 目录里只剩 wan3.0-video、wan3.0-video-prime，wan2.7-r2v 已被下架。
--        new-api 侧此前保留 `wan2.7-r2v` 在渠道 17 的 models 里，路由仍能选中它，
--        但适配器 upstreamModelFor 会直接返回 400「上游已不再提供」——
--        客户看到的是「能路由、必失败」，比「模型不存在」更难排查。
--        本脚本把它彻底摘掉，让调用直接拿到 503「无可用渠道」。
--
-- ⚠️ 为什么不只删 abilities（三条链路各自的驱动源）：
--   · 广场 /api/pricing  ← abilities(enabled=true) × isAllowedPricingModel 白名单
--   · 下游 /v1/models    ← abilities(enabled=true) × HasModelBillingConfig
--   · 路由（内存缓存开）  ← channels.models 字段本身
--     （model/channel_cache.go InitChannelCache 每 SYNC_FREQUENCY=60s 重建）
--   model/ability.go 的 FixAbility()（管理后台「修复能力」按钮 + 启动兜底）
--   会 TRUNCATE abilities 再按 channels.models 重建 —— 只删 abilities 会被复活。
--
-- 执行前核实（2026-10-28 16:1x 本次查询）：
--   · r2v 历史累计 52 单（user_id=31 占 44 单，2 占 4 单，36/28/32 各 1~2 单）
--   · **最近一次调用 2026-10-26 05:57（成功），10-27 之后 0 次调用**
--   · 无在途任务（tasks 里 status 不属于 SUCCESS/FAILURE 的 r2v 任务 0 条）
--   · tokens.model_limits 无任何 token 引用 r2v / wan3.0-smart
--   · r2v 只挂在渠道 17 一个渠道上；摘掉后渠道仍有 wan3.0-video /
--     wan3.0-video-prime / wan3.0-smart 三个模型，渠道本身保留启用
--   · 同渠道的 wan3.0-smart 别名**保留**（10-27 后仍有 10 次调用，且它等价于
--     wan3.0-video，代码内 upstreamModelFor 有映射）
--
-- 代码侧**不动**（沿用 2026-10-25 下架脚本的同一口径）：
--   relay/channel/task/youzanwan3/constants.go   r2vModel 常量
--   relay/channel/task/youzanwan3/adaptor.go     upstreamModelFor 的 400 兜底
--   model/pricing.go                             isAllowedPricingModel 拦截
--   这些是 adaptor 的内部常量与防御分支，不是可下架的商品名；没有 ability
--   就没有路由，白名单/倍率不会被走到。真要删干净得连 adaptor 一起重构。
--
-- 客户可见文档已同步（前端 tutorials/content-models.ts 移除 r2v 条目）。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_delist_r2v_20261028_channels AS
  SELECT * FROM channels WHERE id = 17;
CREATE TABLE IF NOT EXISTS _bak_delist_r2v_20261028_abilities AS
  SELECT * FROM abilities WHERE model = 'wan2.7-r2v';
CREATE TABLE IF NOT EXISTS _bak_delist_r2v_20261028_models AS
  SELECT * FROM models WHERE model_name = 'wan2.7-r2v';
CREATE TABLE IF NOT EXISTS _bak_delist_r2v_20261028_options AS
  SELECT * FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');

-- ---------- ① 从渠道模型列表里摘掉（路由源头）----------
-- 用正则摘除整个逗号分隔项，避免硬编码顺序漂移；只匹配独立项
-- （(^|,) ... (,|$)），不会误伤其它模型名。
UPDATE channels
   SET models = trim(both ',' from regexp_replace(models, '(^|,)wan2\.7-r2v(,|$)', '\1', 'g'))
 WHERE models ~ '(^|,)wan2\.7-r2v(,|$)';

-- ---------- ② 删能力行（广场 + /v1/models 立即生效；当前已是 0 行，幂等）----------
DELETE FROM abilities WHERE model = 'wan2.7-r2v';

-- ---------- ③ 删广场卡片（备份表里有原文，可原样恢复）----------
-- models 表这行 status=1，若留着会在管理后台显示成「在售」的死卡片。
DELETE FROM models WHERE model_name = 'wan2.7-r2v';

-- ---------- ④ 摘价格键 ----------
-- ModelPrice 是 r2v 的计费来源（quota_type=1 按秒）；摘掉后
-- HasModelBillingConfig("wan2.7-r2v") 返回 false，/v1/models 也不会带出它。
-- ModelRatio / CompletionRatio 目前不含该键，一并防御性摘除。
UPDATE options
   SET value = (value::jsonb - 'wan2.7-r2v')::text
 WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND (value::jsonb ? 'wan2.7-r2v');

COMMIT;

-- ---------- 复核（事务外只读，脚本跑完必须逐项核对）----------
\echo '--- ① 渠道 17（应只剩 wan3.0-video / wan3.0-video-prime / wan3.0-smart）---'
SELECT id, name, status, models FROM channels WHERE id = 17;

\echo '--- ② 能力表：r2v 必须 0 行；其余三个必须仍在且 enabled ---'
SELECT "group", model, channel_id, enabled FROM abilities
 WHERE channel_id = 17 ORDER BY model;

\echo '--- ③ 广场卡片：r2v 必须 0 行 ---'
SELECT id, model_name, status FROM models WHERE model_name = 'wan2.7-r2v';

\echo '--- ④ 价格键：r2v 必须 0 行 ---'
SELECT o.key AS opt_key, e.key AS model_key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND e.key = 'wan2.7-r2v';

\echo '--- ⑤ 空 model 名的垃圾能力行：必须 0 行 ---'
SELECT count(*) AS empty_model_abilities FROM abilities WHERE COALESCE(model, '') = '';

\echo '--- ⑥ 在售两个模型的 ModelPrice（应保持不变）---'
SELECT value::jsonb -> 'wan3.0-video' AS video,
       value::jsonb -> 'wan3.0-video-prime' AS prime
  FROM options WHERE key = 'ModelPrice';

\echo '--- ⑦ 渠道 17 是否还有其它渠道挂着 r2v（应 0 行）---'
SELECT id, name, models FROM channels WHERE models LIKE '%wan2.7-r2v%';


-- =====================================================================
-- 回滚（整体恢复到本脚本执行前）
-- =====================================================================
--
-- BEGIN;
-- UPDATE channels c
--    SET models = b.models, status = b.status
--   FROM _bak_delist_r2v_20261028_channels b
--  WHERE c.id = b.id;
-- INSERT INTO abilities SELECT * FROM _bak_delist_r2v_20261028_abilities
--   ON CONFLICT DO NOTHING;
-- INSERT INTO models SELECT * FROM _bak_delist_r2v_20261028_models
--   ON CONFLICT (id) DO NOTHING;
-- -- 价格键：整份还原会覆盖期间其它调价，优先用 jsonb_set 逐键补
-- UPDATE options
--    SET value = jsonb_set(value::jsonb, '{wan2.7-r2v}', to_jsonb(0.1 / 7.3::numeric))::text
--  WHERE key = 'ModelPrice';
-- COMMIT;
--
-- ⚠️ 前端教程文案是另一次发版（commit）改的，回滚数据不会把文档带回来，
--    要恢复文档得 revert 那个 commit 并重新发前端。
