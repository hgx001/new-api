-- new-api 生产库：公开模型名 wan3.0-720p-官网 → wan3.0-720p-特惠 + 调价 ¥0.25 → ¥0.20（2026-10-30 老板拍板）
--
-- 是什么：
--   1. 改名：「官网」→「特惠」。与 seedance2.5-特惠 / wan-3.0-特惠 同一套后缀风格 ——
--      「特惠」直白表达"比官网原生 720P 便宜"，比「官网」更能让客户一眼看懂档位关系。
--   2. 调价：¥0.25/秒 → ¥0.20/秒（单一价位，不随分辨率变化）。
--
-- ⚠️ 执行顺序：**必须先发版**（后端 commit 含新名的路由 / 两跳判定 / 脱敏名单，
--    前端 commit 含教程与模型广场副本），再跑本脚本。
--    只跑 SQL 不发版 → 新名会被路由到通用 wan3 逻辑：
--      · size 倍率落进 1/2/4 表，用户传 720P 时价格算成 ¥0.40/秒（翻倍）；
--      · 不走成片提升 → 交付 480P 原片（用户付 720P 的钱拿 480P 的片）。
--    反过来先跑 SQL 再发版 → 新名 404/503（渠道里还没有这个模型）。
--
-- ⚠️ 改价依据与毛利提示（老板已确认走让价路线）：
--   成本口径（沿用 2026-10-29 上架脚本的核算）：
--     上游 480P 生成   ¥0.20/秒
--     成片提升（COS+MPS）¥0.0067/秒
--     合计成本         ¥0.2067/秒
--   新售价 ¥0.20/秒 → 毛利率约 **−3%**（让价引流档，刻意贴着成本线）。
--   ⚠️ 但官网渠道走的是**阿里云百炼官方 API**，公开原价 480P ¥0.30/秒
--      （2026-08-24~09-23 的 7 折 ¥0.21/秒 是限时活动，已过期）。
--      若采购端没有折扣、实际按 ¥0.30/秒 结算，这档实际是**亏本卖**。
--      → 建议尽快核实百炼账户的实际结算单价（账单/控制台）。
--
-- 四件事必须同时做（缺一不可）：
--   ① channels.models + model_mapping 改名（渠道 12）
--   ② abilities 行（新增新名；旧名保留可调用）
--   ③ models 广场卡片（改名 + 正文改 ¥0.20）
--   ④ options.ModelPrice（新名加键 + 旧名改同价）
--
-- ⚠️ 旧名 wan3.0-720p-官网 **过渡期保留可调用**（与 wan-3.0-无限 → wan-3.0-特惠 同一套做法）：
--   · 能力行保留、channels.models 条目保留、价目键保留（且与新名**同价**）；
--   · 但广场卡片**只挂新名**（旧名不进广场），代码白名单也只放行新名。
--   理由：改名前后同货不同价 = 差别定价；旧名查不到价 = 静默免单（更糟）。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_wan3720p_tehui_models_20261030 AS
  SELECT * FROM models WHERE model_name IN ('wan3.0-720p-官网', 'wan3.0-720p-特惠');
CREATE TABLE IF NOT EXISTS _bak_wan3720p_tehui_abilities_20261030 AS
  SELECT * FROM abilities WHERE channel_id = 12 AND model IN ('wan3.0-720p-官网', 'wan3.0-720p-特惠');
CREATE TABLE IF NOT EXISTS _bak_wan3720p_tehui_channels_20261030 AS
  SELECT * FROM channels WHERE id = 12;
CREATE TABLE IF NOT EXISTS _bak_wan3720p_tehui_options_20261030 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- ① 广场卡片：改名 + 正文改口径 ----------
-- 卡片正文**不能出现「超分 / 增强 / 腾讯云」等内部实现词**（2026-10-29 老板明确要求）：
-- 露出实现细节等于把成本结构和真实货源写在脸上，客户会拿
-- 「这不是原生 720P」来质疑定价。
UPDATE models
   SET model_name   = 'wan3.0-720p-特惠',
       description  = 'Wan3.0 视频生成，固定输出 720P。支持文生视频、图生视频与多模态参考（图 / 视频 / 音频），时长 2-30 秒。计费按秒 ¥0.20，单一价位，不随分辨率变化。⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.20 计费，例如 10 秒参考视频 + 生成 10 秒 = ¥4.00（不传参考视频只要 ¥2.00）。',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan3.0-720p-官网'
   AND deleted_at IS NULL
   AND NOT EXISTS (
         SELECT 1 FROM models m2
          WHERE m2.model_name = 'wan3.0-720p-特惠' AND m2.deleted_at IS NULL
       );

-- 若卡片已是新名（重复执行），只补正文里的旧口径字样。
UPDATE models
   SET description  = 'Wan3.0 视频生成，固定输出 720P。支持文生视频、图生视频与多模态参考（图 / 视频 / 音频），时长 2-30 秒。计费按秒 ¥0.20，单一价位，不随分辨率变化。⚠️ 带参考视频时按「生成秒数 + 参考视频总秒数（四舍五入）」× ¥0.20 计费，例如 10 秒参考视频 + 生成 10 秒 = ¥4.00（不传参考视频只要 ¥2.00）。',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan3.0-720p-特惠'
   AND deleted_at IS NULL
   AND (description LIKE '%¥0.25%' OR description LIKE '%wan3.0-720p-官网%');

-- ---------- ② 价目键 ----------
-- 单位 USD 等值；1 元 = 1/7.3 USD（与 wan3.0-video-官网 同口径）。
-- ¥0.20/秒 → 0.20/7.3 = 0.0273972602739726
-- 计费链路（任务模型）：quota = int(ModelPrice × QuotaPerUnit) × seconds × size
--   - seconds：生成秒数 + 参考视频总秒数（四舍五入）→ 代码
--   - size：**恒为 1.0**（本档只有 720P 一个输出档位，价格是复合价）→ 代码
-- 复核值（QuotaPerUnit=500000，Price=7.3）：10 秒 = ¥2.00；+10 秒参考 = ¥4.00。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan3.0-720p-特惠}', to_jsonb(0.2 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND COALESCE((value::jsonb ->> 'wan3.0-720p-特惠')::numeric, -1) <> (0.2 / 7.3)::numeric;

-- 旧名**同价**（过渡期两个名字下单必须是一个价，否则等于按名字差别定价）。
-- ⚠️ 这一行绝不能删：删了旧名会走 ModelPriceHelperPerCall 的「查不到价」分支，
--    任务按次计费算成 ¥0 —— **静默免单**，比报错更难发现。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan3.0-720p-官网}', to_jsonb(0.2 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND (value::jsonb ? 'wan3.0-720p-官网')
   AND COALESCE((value::jsonb ->> 'wan3.0-720p-官网')::numeric, -1) <> (0.2 / 7.3)::numeric;

-- ---------- ③ abilities：新名加一行（旧名那行保留，可调用）----------
-- 从旧名那行整行复制，priority/weight/tag 一并继承，避免两行权重不一致。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'wan3.0-720p-特惠', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 12
   AND model = 'wan3.0-720p-官网'
   AND NOT EXISTS (
         SELECT 1 FROM abilities a2
          WHERE a2.channel_id = 12 AND a2.model = 'wan3.0-720p-特惠'
       );

-- ---------- ④ channels.models：追加新名（旧名条目保留）----------
-- 用 array_append 而不是整体覆写，避免冲掉执行前别人新增的模型名。
UPDATE channels
   SET models = array_to_string(
         array_append(string_to_array(models, ','), 'wan3.0-720p-特惠'), ',')
 WHERE id = 12
   AND NOT (',' || models || ',') LIKE '%,wan3.0-720p-特惠,%';

-- ---------- ⑤ channels.model_mapping：给新名补映射 ----------
-- 上游只认标准版 wan3.0-video，新名与旧名都要映射过去。
-- ⚠️ 代码里 upstreamModelFor 已有兜底（不依赖这个配置），
--    但渠道映射仍要补 —— 万一以后有人删了代码里的兜底，这里是第二道保险。
UPDATE channels
   SET model_mapping = jsonb_set(
         model_mapping::jsonb, '{wan3.0-720p-特惠}', '"wan3.0-video"'::jsonb, true)::text
 WHERE id = 12
   AND COALESCE(model_mapping, '') <> ''
   AND NOT (model_mapping::jsonb ? 'wan3.0-720p-特惠');

COMMIT;

-- ---------- 复核（事务外只读，脚本跑完必须逐项核对）----------

-- 期望：一张卡 wan3.0-720p-特惠（正文 ¥0.20，不含「超分/增强/腾讯」）。
SELECT id, model_name, status, left(description, 90) AS descr_head
  FROM models WHERE deleted_at IS NULL AND model_name LIKE 'wan3.0-720p-%' ORDER BY model_name;

-- 期望：两行能力（新名 + 过渡期旧名），都 enabled = t，channel_id = 12。
SELECT model, channel_id, "group", enabled, priority, weight
  FROM abilities WHERE model LIKE 'wan3.0-720p-%' ORDER BY model;

-- 期望：channel 12 的 models 同时含新旧两名；model_mapping 两个名字都映射到 wan3.0-video。
SELECT id, name, models, model_mapping FROM channels WHERE id = 12;

-- 期望：两个键都是 ¥0.20（0.0273972602739726）。
SELECT e.key, e.value, round((e.value::numeric * 7.3)::numeric, 4) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key LIKE 'wan3.0-720p-%' ORDER BY e.key;

-- 期望：新名有卡片（model_name = wan3.0-720p-特惠），旧名不再有卡片。
SELECT count(*) AS should_be_1 FROM models
 WHERE model_name = 'wan3.0-720p-特惠' AND deleted_at IS NULL;


-- =====================================================================
-- 旧名清理条件（三个都满足才删）
-- =====================================================================
-- 1. 调用方已全量切到新名（客户端代码 / 教程 / 下游 ArcReel）；
-- 2. 近 30 天 logs 表按 model_name 统计，旧名调用数为 0：
--      SELECT model_name, count(*) FROM logs
--       WHERE model_name = 'wan3.0-720p-官网'
--         AND created_at > EXTRACT(EPOCH FROM now())::bigint - 30*86400
--       GROUP BY model_name;
-- 3. 广场卡片已只挂新名（本次脚本已做）。
-- 清理内容：删 abilities 旧行 + channels.models 摘掉旧名 + channels.model_mapping 删旧键
--   + ModelPrice 摘掉旧键 + 代码里删掉 Official720PModelNameLegacy
--   + model/pricing.go 白名单保持只含新名（已如此）。


-- =====================================================================
-- 回滚 SQL（整体回到本脚本执行前状态）
-- =====================================================================
--
-- BEGIN;
-- -- ① 卡片名与正文
-- UPDATE models SET model_name = 'wan3.0-720p-官网',
--        description = (SELECT description FROM _bak_wan3720p_tehui_models_20261030
--                         WHERE model_name = 'wan3.0-720p-官网' LIMIT 1),
--        updated_time = EXTRACT(EPOCH FROM now())::bigint
--  WHERE model_name = 'wan3.0-720p-特惠' AND deleted_at IS NULL;
-- -- ② 价目键：新名删掉；旧名恢复 ¥0.25
-- UPDATE options SET value = (value::jsonb - 'wan3.0-720p-特惠')::text
--  WHERE key = 'ModelPrice' AND (value::jsonb ? 'wan3.0-720p-特惠');
-- UPDATE options SET value = jsonb_set(value::jsonb, '{wan3.0-720p-官网}', to_jsonb(0.25 / 7.3::numeric))::text
--  WHERE key = 'ModelPrice' AND (value::jsonb ? 'wan3.0-720p-官网');
-- -- ③ 能力行
-- DELETE FROM abilities WHERE channel_id = 12 AND model = 'wan3.0-720p-特惠';
-- -- ④ channels
-- UPDATE channels SET models = array_to_string(
--          array_remove(string_to_array(models, ','), 'wan3.0-720p-特惠'), ',')
--  WHERE id = 12 AND (',' || models || ',') LIKE '%,wan3.0-720p-特惠,%';
-- UPDATE channels SET model_mapping = (model_mapping::jsonb - 'wan3.0-720p-特惠')::text
--  WHERE id = 12 AND (model_mapping::jsonb ? 'wan3.0-720p-特惠');
-- COMMIT;
--
-- ⚠️ 回滚 SQL 必须与代码回滚同步：新版代码已把两跳判定切到新名+旧名双认、
--    广场白名单只放行新名。只回滚 DB 不回滚代码 → 新名 404、旧名不上广场。
