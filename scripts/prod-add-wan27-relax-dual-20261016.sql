-- new-api 生产库：wan2.7 免灵感值双模型（图生 / 编辑）上架
-- 依据：ArcReel/docs/plans/2026-10-16-wan27-relax-newapi-model.md §A3
-- 前置：adaptor.go（ModelWan27I2V / ModelWan27Edit + 路由表 + 门控）必须已发版上线，
--       否则出现「模型可见但一调就 400」的半可用窗口。发布顺序见方案 §6，不可颠倒。
-- 全程单事务 + 备份表，出错即回滚。
--
-- 两个模型共用 platformId=tongyiwan，靠素材类型分流 DOM 路线：
--   wan2.7-图生：inputs 带 reference_image（可选，纯文生亦可）
--   wan2.7-编辑：inputs 带 reference_video（必需且恰 1 个）+ generationMode='edit'
-- Worker 侧 RemoteTaskAdapter 对 tongyiwan 已硬编码 generateWithCredits+requireFreeMode，
-- 因此这条通路不消耗官网灵感值（relax 慢队列）。

BEGIN;

-- ---------- 备份 ----------
-- models 备份此刻应为空表：这正是回滚的基线（回滚 = DELETE 新增两行）。
CREATE TABLE IF NOT EXISTS _bak_wan27_dual_channels_20261016 AS
  SELECT * FROM channels WHERE id = 20;

CREATE TABLE IF NOT EXISTS _bak_wan27_dual_abilities_20261016 AS
  SELECT * FROM abilities WHERE channel_id = 20;

CREATE TABLE IF NOT EXISTS _bak_wan27_dual_models_20261016 AS
  SELECT * FROM models WHERE model_name IN ('wan2.7-图生', 'wan2.7-编辑');

CREATE TABLE IF NOT EXISTS _bak_wan27_dual_options_20261016 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- ① channels.models：追加两个模型名 ----------
-- 路由源头。只改 abilities 的话，渠道一旦被后台编辑，model.UpdateAbilities 会按
-- channels.models 删光重建 abilities → 新模型消失。**必须先改这里。**
-- 用追加而非整体覆写：避免把执行前别人新增的模型名冲掉。
-- 守卫条件保证重复执行不会拼接出两份模型名（幂等）。
UPDATE channels
   SET models = models || ',wan2.7-图生,wan2.7-编辑'
 WHERE id = 20
   AND models !~ '(^|,)wan2\.7-图生(,|$)';

-- ---------- ② abilities：各新增一行能力 ----------
-- 同渠道既有行（group=default / enabled=t / priority=0 / weight=0 / tag=NULL）原样复制。
-- 模板行取 seedance-2-5（当前主力），NOT EXISTS 守卫保证重复执行不产生重复行。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'wan2.7-图生', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 20 AND model = 'seedance-2-5'
   AND NOT EXISTS (SELECT 1 FROM abilities
                    WHERE channel_id = 20 AND model = 'wan2.7-图生');

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'wan2.7-编辑', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 20 AND model = 'seedance-2-5'
   AND NOT EXISTS (SELECT 1 FROM abilities
                    WHERE channel_id = 20 AND model = 'wan2.7-编辑');

-- ---------- ③ models：模型广场卡片（description = 广场正文 = 老板要的「备注」）----------
-- vendor_id 置 NULL：vendors 表无阿里/通义条目（id 1-7 分别是 Moonshot/智谱/DeepSeek/
-- OpenAI/讯飞/字节/Google），同渠道的慢速档 wan3.0-1080p-慢速 也是 NULL。
-- icon='Aliyun' 与 aliyun:wan-3.0 卡片一致，广场靠它显示厂商图标。
-- 文案必须含「通常 3 小时左右出一条」——老板明确要求的交付预期提示。
INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, created_time, updated_time, name_rule)
SELECT * FROM (VALUES
  ('wan2.7-图生',
   '通义万相 Wan2.7 图生视频（免灵感值慢速版）。纯文本或 ≤2 张参考图生成视频，5–15 秒逐秒可选，720P/1080P。按次计费 ¥0.30/次。⚠️ 走官网免灵感值慢队列，速度慢，通常 3 小时左右出一条，急单请选其它模型',
   'Aliyun', 'video', NULL::bigint, '', 1::bigint, 1::bigint,
   EXTRACT(EPOCH FROM now())::bigint, EXTRACT(EPOCH FROM now())::bigint, 0::bigint),
  ('wan2.7-编辑',
   '通义万相 Wan2.7 视频编辑（免灵感值慢速版）。上传主视频（≤10 秒 / ≤50MB）按提示词编辑，≤10 张参考图。按次计费 ¥0.30/次。⚠️ 走官网免灵感值慢队列，速度慢，通常 3 小时左右出一条，急单请选其它模型',
   'Aliyun', 'video', NULL::bigint, '', 1::bigint, 1::bigint,
   EXTRACT(EPOCH FROM now())::bigint, EXTRACT(EPOCH FROM now())::bigint, 0::bigint)
) AS v(model_name, description, icon, tags, vendor_id, endpoints,
      status, sync_official, created_time, updated_time, name_rule)
WHERE NOT EXISTS (SELECT 1 FROM models WHERE model_name = v.model_name);

-- ---------- ④ options.ModelPrice：两个模型各写单价 ----------
-- 单位 USD 等值；换算 1 元 = 1/7.3 USD，与 Nano Banana Pro（0.3/7.3）同口径。
-- ¥0.30/次 → 0.3/7.3 = 0.0410958904109589
-- Adaptor 侧 EstimateBilling 对按次模型返回 nil（adaptor.go:593-608），
-- relay/relay_task.go:181 走 ModelPriceHelperPerCall 读这里，不会再被秒数乘一遍。
UPDATE options
   SET value = jsonb_set(
         jsonb_set(value::jsonb, '{wan2.7-图生}', to_jsonb(0.3 / 7.3::numeric)),
                    '{wan2.7-编辑}', to_jsonb(0.3 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'wan2.7-图生' AND value::jsonb ? 'wan2.7-编辑');

COMMIT;

-- ---------- 复核（事务外只读，脚本跑完必须逐项核对）----------
SELECT id, models FROM channels WHERE id = 20;

SELECT model, "group", enabled, priority, weight, tag
  FROM abilities WHERE channel_id = 20 ORDER BY model;

SELECT model_name, icon, tags, vendor_id, status, sync_official,
       left(description, 60) AS description_head
  FROM models
 WHERE model_name IN ('wan2.7-图生', 'wan2.7-编辑') AND deleted_at IS NULL;

SELECT e.key, e.value, round((e.value::numeric * 7.3)::numeric, 4) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key IN ('wan2.7-图生', 'wan2.7-编辑');


-- =====================================================================
-- 回滚 SQL（仅在上架后发现错误时使用；反向执行，逐项注释说明）
-- =====================================================================
--
-- BEGIN;
-- -- ④ 摘除单价（备份表 _bak_wan27_dual_options_20261016 可整表还原）
-- UPDATE options
--    SET value = (value::jsonb - 'wan2.7-图生' - 'wan2.7-编辑')::text
--  WHERE key = 'ModelPrice';
-- -- ③ 摘除广场卡片
-- DELETE FROM models WHERE model_name IN ('wan2.7-图生', 'wan2.7-编辑');
-- -- ② 摘除能力行
-- DELETE FROM abilities WHERE channel_id = 20 AND model IN ('wan2.7-图生', 'wan2.7-编辑');
-- -- ① 摘除路由模型名（保留其余模型名，只删这两个）
-- UPDATE channels
--    SET models = array_to_string(
--          array_remove(string_to_array(models, ','), 'wan2.7-图生'), ',')
--  WHERE id = 20;
-- UPDATE channels
--    SET models = array_to_string(
--          array_remove(string_to_array(models, ','), 'wan2.7-编辑'), ',')
--  WHERE id = 20;
-- COMMIT;
--
-- 若需整体回退到执行前状态：
--   channels  → UPDATE channels SET models = (SELECT models FROM _bak_wan27_dual_channels_20261016) WHERE id = 20;
--   abilities → DELETE FROM abilities WHERE channel_id = 20;
--                INSERT INTO abilities SELECT * FROM _bak_wan27_dual_abilities_20261016;
--   options   → UPDATE options SET value = (SELECT value FROM _bak_wan27_dual_options_20261016) WHERE key = 'ModelPrice';
