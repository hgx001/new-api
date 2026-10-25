-- new-api 生产库：wan-3.0-无限 分分辨率定价（480p ¥1.50/次、720p ¥2.00/次）
-- 依据：老板 2026-10-25 定稿。
--
-- ⚠️ 为什么必须拆成两个模型名（不是只改一个价格）：
--   new-api 只能按**模型名**计价 —— options.ModelPrice 按名索引，
--   relay/relay_task.go:181 的 ModelPriceHelperPerCall 读 info.OriginModelName，
--   而分辨率原本是**请求体参数**（ArcReel 侧 picsart 只认 480P/720P，缺省 720P）。
--   单名字承载不了两个价格，所以新增两个公开名，靠 adaptor 的
--   modelSpec.ForcedResolution 把分辨率锁死在名字里：
--     显式传别的档 → 400（不静默改档，与固定 30 秒同口径）
--     缺省         → 按名字补（否则调 -480p 名会落到默认 720P）
--   不锁的话，「model=wan-3.0-无限-480p + resolution=720P」能花 1.5 拿 2 块的片。
--
-- ⚠️ 前置条件（发布顺序不可颠倒）：
--   relay/channel/task/manwu/adaptor.go 里的 ModelWan3Unlimited480P / ModelWan3Unlimited720P
--   两个 spec 必须**先发版上线**。反序执行会出现「模型在广场可见、但一调就 400」
--   （SpecFor 不认识新名字）的半可用窗口。
--
-- 三个公开名同走 picsart 上游（发往 ArcReel 的一律是官网档位 id wan-3.0-video），
-- 翻译在 adaptor 的 spec.UpstreamModel 里做，**不需要配 model_mapping**。
--
-- 单档名 wan-3.0-无限 保留并改价为 ¥2.00/次（= 720p 档价）：ArcReel 在请求未指定
-- resolution 时默认给 720P，老调用不破、也不会出现「¥3 与 ¥1.5/¥2 并存」的乱价。
-- 若后续要摘掉它，见文末回滚段 ④ 的单行写法。
--
-- 全程单事务 + 备份表，出错即回滚。

BEGIN;

-- ---------- 备份 ----------
-- 回滚基线：执行前状态整表留底，不依赖任何推断。
CREATE TABLE IF NOT EXISTS _bak_wan3_tiered_channels_20261025 AS
  SELECT * FROM channels WHERE id = 20;

CREATE TABLE IF NOT EXISTS _bak_wan3_tiered_abilities_20261025 AS
  SELECT * FROM abilities WHERE channel_id = 20;

CREATE TABLE IF NOT EXISTS _bak_wan3_tiered_models_20261025 AS
  SELECT * FROM models WHERE model_name IN ('wan-3.0-无限-480p', 'wan-3.0-无限-720p');

CREATE TABLE IF NOT EXISTS _bak_wan3_tiered_options_20261025 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- ① channels.models：追加两个模型名 ----------
-- 路由源头。只改 abilities 的话，渠道一旦被后台编辑，model.UpdateAbilities 会按
-- channels.models 删光重建 abilities → 新模型消失。**必须先改这里。**
-- 追加而非覆写：避免把执行前别人新增的模型名冲掉；正则守卫保证重复执行不拼双份。
UPDATE channels
   SET models = models || ',wan-3.0-无限-480p,wan-3.0-无限-720p'
 WHERE id = 20
   AND models !~ '(^|,)wan-3\.0-无限-480p(,|$)';

-- ---------- ② abilities：各新增一行能力 ----------
-- 模板行取同商品的单档名 wan-3.0-无限（同渠道、同 group、同启停），NOT EXISTS
-- 守卫保证重复执行不产生重复行。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'wan-3.0-无限-480p', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 20 AND model = 'wan-3.0-无限'
   AND NOT EXISTS (SELECT 1 FROM abilities
                    WHERE channel_id = 20 AND model = 'wan-3.0-无限-480p');

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'wan-3.0-无限-720p', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 20 AND model = 'wan-3.0-无限'
   AND NOT EXISTS (SELECT 1 FROM abilities
                    WHERE channel_id = 20 AND model = 'wan-3.0-无限-720p');

-- ---------- ③ models：模型广场卡片 ----------
-- icon/tags/vendor 与单档名 wan-3.0-无限 一致（icon=NULL、tags=video、vendor=NULL），
-- 广场靠 tags 归类。description 即广场正文，必须写清价格与分辨率档位。
INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, created_time, updated_time, name_rule)
SELECT * FROM (VALUES
  ('wan-3.0-无限-480p',
   'Wan 3.0 视频生成（Picsart 免费无限档 · 480P 档）。固定 30 秒一条、480P 输出，不随时长加价（传 10 秒等其它时长直接报错拒绝，不会静默改成 30 秒）。按次计费 ¥1.50/次。支持 6 档比例（含自适应），≤10 张参考图，不收参考视频。⚠️ 官网免费档串行出片，单条通常 15–25 分钟，急单请选其它模型',
   NULL::varchar, 'video', NULL::bigint, '', 1::bigint, 1::bigint,
   EXTRACT(EPOCH FROM now())::bigint, EXTRACT(EPOCH FROM now())::bigint, 0::bigint),
  ('wan-3.0-无限-720p',
   'Wan 3.0 视频生成（Picsart 免费无限档 · 720P 档）。固定 30 秒一条、720P 输出，不随时长加价（传 10 秒等其它时长直接报错拒绝，不会静默改成 30 秒）。按次计费 ¥2.00/次。支持 6 档比例（含自适应），≤10 张参考图，不收参考视频。⚠️ 官网免费档串行出片，单条通常 15–25 分钟，急单请选其它模型',
   NULL::varchar, 'video', NULL::bigint, '', 1::bigint, 1::bigint,
   EXTRACT(EPOCH FROM now())::bigint, EXTRACT(EPOCH FROM now())::bigint, 0::bigint)
) AS v(model_name, description, icon, tags, vendor_id, endpoints,
      status, sync_official, created_time, updated_time, name_rule)
WHERE NOT EXISTS (SELECT 1 FROM models WHERE model_name = v.model_name);

-- ---------- ④ options.ModelPrice ----------
-- 单位 USD 等值；换算 1 元 = 1/7.3 USD，与 wan2.7-图生（0.3/7.3）同口径。
--   ¥1.50/次 → 1.5/7.3 = 0.2054794520547945
--   ¥2.00/次 → 2.0/7.3 = 0.2739726027397260
-- Adaptor 侧 EstimateBilling 对 picsart 视频返回 nil（按次模型），
-- relay/relay_task.go 不会再把单价乘一遍秒数。
--
-- ④-a 新增两个分档价（NOT EXISTS 守卫：重复执行不覆盖已调过的价）
UPDATE options
   SET value = jsonb_set(
         jsonb_set(value::jsonb, '{wan-3.0-无限-480p}', to_jsonb(1.5 / 7.3::numeric)),
                    '{wan-3.0-无限-720p}', to_jsonb(2.0 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'wan-3.0-无限-480p' AND value::jsonb ? 'wan-3.0-无限-720p');

-- ④-b 单档名改价 ¥3.00 → ¥2.00（= 720p 档价，与 ArcReel 缺省 720P 对齐）
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan-3.0-无限}', to_jsonb(2.0 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND (value::jsonb ->> 'wan-3.0-无限')::numeric <> (2.0 / 7.3)::numeric;

-- ---------- ⑤ models：单档名卡片正文同步改价 ----------
-- ④-b 把 wan-3.0-无限 的价格改成 ¥2.00，但卡片 description（广场正文）里还写着
-- 「固定 ¥3.00/条」——价格与文案不一致会被客户当成乱价。此处同步。
-- 守卫用 LIKE '%¥3.00%'：只改还没改过的，重复执行不覆盖人工润色后的文案。
UPDATE models
   SET description = 'Wan 3.0 视频生成（Picsart 免费无限档）。固定 30 秒一条；未指定分辨率时按 720P 交付，按次计费 ¥2.00/次。需要 480P 请改用 wan-3.0-无限-480p（¥1.50/次）。支持 6 档比例（含自适应），≤10 张参考图，不收参考视频。⚠️ 官网免费档串行出片，单条通常 15–25 分钟，急单请选其它模型',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan-3.0-无限'
   AND description LIKE '%¥3.00%';

COMMIT;
SELECT id, models FROM channels WHERE id = 20;

SELECT model, "group", enabled, priority, weight, tag
  FROM abilities WHERE channel_id = 20 ORDER BY model;

SELECT model_name, icon, tags, vendor_id, status, sync_official,
       left(description, 50) AS description_head
  FROM models
 WHERE model_name LIKE 'wan-3.0-无限%' AND deleted_at IS NULL;

-- 价格必须落在 1.5 / 2.0 / 2.0 元（cny 列是还原出来给人看的）
SELECT e.key, e.value, round((e.value::numeric * 7.3)::numeric, 4) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key LIKE 'wan-3.0-无限%'
 ORDER BY e.key;


-- =====================================================================
-- 回滚 SQL（仅在上架后发现错误时使用；反向执行，逐项注释说明）
-- =====================================================================
--
-- BEGIN;
-- -- ④-b 单档名价格还原 ¥3.00（备份表 _bak_wan3_tiered_options_20261025 可整表还原）
-- UPDATE options
--    SET value = jsonb_set(value::jsonb, '{wan-3.0-无限}', to_jsonb(3.0 / 7.3::numeric))::text
--  WHERE key = 'ModelPrice';
-- -- ④-a 摘除两个分档价
-- UPDATE options
--    SET value = (value::jsonb - 'wan-3.0-无限-480p' - 'wan-3.0-无限-720p')::text
--  WHERE key = 'ModelPrice';
-- -- ③ 摘除广场卡片
-- DELETE FROM models WHERE model_name IN ('wan-3.0-无限-480p', 'wan-3.0-无限-720p');
-- -- ② 摘除能力行
-- DELETE FROM abilities WHERE channel_id = 20 AND model IN ('wan-3.0-无限-480p', 'wan-3.0-无限-720p');
-- -- ① 摘除路由模型名（保留其余模型名，只删这两个）
-- UPDATE channels
--    SET models = array_to_string(
--          array_remove(string_to_array(models, ','), 'wan-3.0-无限-480p'), ',')
--  WHERE id = 20;
-- UPDATE channels
--    SET models = array_to_string(
--          array_remove(string_to_array(models, ','), 'wan-3.0-无限-720p'), ',')
--  WHERE id = 20;
-- COMMIT;
--
-- 若需整体回退到执行前状态：
--   channels  → UPDATE channels SET models = (SELECT models FROM _bak_wan3_tiered_channels_20261025) WHERE id = 20;
--   abilities → DELETE FROM abilities WHERE channel_id = 20;
--                INSERT INTO abilities SELECT * FROM _bak_wan3_tiered_abilities_20261025;
--   options   → UPDATE options SET value = (SELECT value FROM _bak_wan3_tiered_options_20261025) WHERE key = 'ModelPrice';
