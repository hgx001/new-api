-- new-api 生产库：wan-3.0-无限 按分辨率计价（480P ¥1.50/次、720P ¥2.00/次）
-- 依据：老板 2026-10-25 定稿。
--
-- ✅ 为什么用「单模型名 + size 倍率」而不是拆公开名：
--   ModelPrice 按模型名索引，一个名字只能配一个数，看起来两档价只能拆名。
--   但按次任务的最终扣费 = **预扣额**（service/task_polling.go:767 对
--   PerCallBilling 跳过差额结算），而预扣额会被 adaptor 的 EstimateBilling
--   返回的 OtherRatios 乘一次（relay/relay_task.go:206-208）。所以：
--     ModelPrice['wan-3.0-无限'] = ¥2.00（720P 基准价）
--     480P → EstimateBilling 返回 {"size": 0.75} → ¥1.50
--     720P → 返回 nil（不乘）              → ¥2.00
--   与现网 wan3.0-smart / wan3.0-video-官网 的 size 倍率同口径。
--
--   这样「按 480P 请求就按 480P 计价」天然一致，不存在「调 480p 名却传
--   720P 参」的计价漏洞，也不需要白名单 / 卡片 / abilities 多出两套条目。
--   代码侧见 manwu/adaptor.go 的 picsart480PSizeRatio。
--
-- 本脚本同时**回滚** 2026-10-25 早些时候误用的「拆两个公开名」方案
-- （wan-3.0-无限-480p / -720p 已上过生产，此处清干净）。
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_wan3_respricing_channels_20261025 AS
  SELECT * FROM channels WHERE id = 20;
CREATE TABLE IF NOT EXISTS _bak_wan3_respricing_abilities_20261025 AS
  SELECT * FROM abilities WHERE channel_id = 20;
CREATE TABLE IF NOT EXISTS _bak_wan3_respricing_models_20261025 AS
  SELECT * FROM models WHERE model_name LIKE 'wan-3.0-无限%';
CREATE TABLE IF NOT EXISTS _bak_wan3_respricing_options_20261025 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- ① 回滚拆分名：channels.models 摘掉两个多余名字 ----------
-- 用 array_remove 逐项删，避免整体覆写冲掉执行前别人新增的模型名。
UPDATE channels
   SET models = array_to_string(
         array_remove(
           array_remove(string_to_array(models, ','), 'wan-3.0-无限-480p'),
           'wan-3.0-无限-720p'), ',')
 WHERE id = 20
   AND models ~ '(^|,)wan-3\.0-无限-(480p|720p)(,|$)';

-- ---------- ② 回滚拆分名：abilities 删掉对应能力行 ----------
DELETE FROM abilities
 WHERE channel_id = 20
   AND model IN ('wan-3.0-无限-480p', 'wan-3.0-无限-720p');

-- ---------- ③ 回滚拆分名：models 删掉对应广场卡片 ----------
-- 硬删：这两张卡是今天新建的，从未被单独售卖（唯一一笔调用是测试单）。
DELETE FROM models
 WHERE model_name IN ('wan-3.0-无限-480p', 'wan-3.0-无限-720p');

-- ---------- ④ 回滚拆分名：ModelPrice 摘掉两个多余键 ----------
UPDATE options
   SET value = (value::jsonb - 'wan-3.0-无限-480p' - 'wan-3.0-无限-720p')::text
 WHERE key = 'ModelPrice'
   AND (value::jsonb ? 'wan-3.0-无限-480p' OR value::jsonb ? 'wan-3.0-无限-720p');

-- ---------- ⑤ 单模型名配 720P 基准价 ¥2.00 ----------
-- 单位 USD 等值；1 元 = 1/7.3 USD（与 wan2.7-图生 的 0.3/7.3 同口径）。
-- ¥2.00 → 2.0/7.3 = 0.2739726027397260
-- 480P 的 ¥1.50 由代码里的 size=0.75 倍率折出来，**不写在这里**。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan-3.0-无限}', to_jsonb(2.0 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND COALESCE((value::jsonb ->> 'wan-3.0-无限')::numeric, -1) <> (2.0 / 7.3)::numeric;

-- ---------- ⑥ 卡片正文：写清两档价 ----------
-- 广场正文是客户唯一能看到价目表的地方，必须与计费口径一致。
UPDATE models
   SET description = 'Wan 3.0 视频生成（Picsart 免费无限档）。固定 30 秒一条：480P ¥1.50/次、720P ¥2.00/次（按请求的分辨率自动计价，未指定时按 720P）。支持 6 档比例（含自适应），≤10 张参考图，不收参考视频。⚠️ 官网免费档串行出片，单条通常 15–25 分钟，急单请选其它模型',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan-3.0-无限'
   AND deleted_at IS NULL
   AND description NOT LIKE '%480P ¥1.50/次%';

COMMIT;

-- ---------- 复核（事务外只读，脚本跑完必须逐项核对）----------
SELECT id, models FROM channels WHERE id = 20;

SELECT model, "group", enabled FROM abilities WHERE channel_id = 20 ORDER BY model;

SELECT model_name, status, left(description, 70) AS description_head
  FROM models WHERE model_name LIKE 'wan-3.0-无限%' AND deleted_at IS NULL;

-- 期望只剩一行：wan-3.0-无限 = ¥2.00（480P 的 1.5 由 size=0.75 折出）
SELECT e.key, e.value, round((e.value::numeric * 7.3)::numeric, 4) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key LIKE 'wan-3.0-无限%';


-- =====================================================================
-- 回滚 SQL（回到 2026-10-25 调价前的状态：单名 ¥3.00、不分分辨率）
-- =====================================================================
--
-- BEGIN;
-- UPDATE options
--    SET value = jsonb_set(value::jsonb, '{wan-3.0-无限}', to_jsonb(3.0 / 7.3::numeric))::text
--  WHERE key = 'ModelPrice';
-- UPDATE models
--    SET description = 'Wan 3.0 视频生成（Picsart 免费无限档）。固定 ¥3.00/条：**仅 480P 或 720P、固定 30 秒一条**（不随时长加价；传 10 秒等其它时长直接报错拒绝，不会静默改成 30 秒）。支持 6 档比例（含自适应），≤10 张参考图，不收参考视频。⚠️ 官网免费档串行出片，单条通常 15–25 分钟，急单请选其它模型'
--  WHERE model_name = 'wan-3.0-无限';
-- COMMIT;
--
-- ⚠️ 回滚价格必须同时把代码里的 picsart480PSizeRatio 改回 1.0 并发版，
--    否则 480P 仍会被打 75 折（¥3.00 → ¥2.25）。
--
-- 若需整体回退到本脚本执行前状态：
--   channels  → UPDATE channels SET models = (SELECT models FROM _bak_wan3_respricing_channels_20261025) WHERE id = 20;
--   abilities → DELETE FROM abilities WHERE channel_id = 20;
--                INSERT INTO abilities SELECT * FROM _bak_wan3_respricing_abilities_20261025;
--   models    → DELETE FROM models WHERE model_name LIKE 'wan-3.0-无限%';
--                INSERT INTO models SELECT * FROM _bak_wan3_respricing_models_20261025;
--   options   → UPDATE options SET value = (SELECT value FROM _bak_wan3_respricing_options_20261025) WHERE key = 'ModelPrice';
