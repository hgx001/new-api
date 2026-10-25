-- new-api 生产库：公开模型名 wan-3.0-无限 → wan-3.0-特惠（2026-10-25 老板定稿）
--
-- 改名理由：「无限」指**账号侧的无限合成权益**（不扣积分、不限条数），
--   但对客户是按次计费，叫「无限」容易被理解成「免费无限调用」，咨询成本高。
--   改用「特惠」，与 MegaAI 渠道的 seedance2.5-特惠 同一套后缀风格。
--
-- ⚠️ 与 MegaAI 渠道的 `wan-3.0`（channel 24，¥3.00/次起）是**两个不同商品**
--    （不同渠道/不同上游）。本脚本里所有 LIKE 都写成 `wan-3.0-%` 或精确等值，
--    绝不能用 `wan-3.0` 前缀替换 —— 那会把 channel 24 的商品一起改坏。
--
-- 影响面（4 处 + 1 处过渡期保留）：
--   ① models          id=55 广场卡片  → 改名 + 正文改口径
--   ② options.ModelPrice      价目键  → 新增 wan-3.0-特惠 = ¥1.50（按次，不分分辨率）
--   ③ abilities      channel 20      → 新增 wan-3.0-特惠 能力行
--   ④ channels.models channel 20     → 追加 wan-3.0-特惠（array_append，不整体覆写）
--   ⑤ 【过渡期保留】旧名 wan-3.0-无限 的 abilities 行 / channels.models 条目 /
--      ModelPrice 键**都先不删**。原因见文件末尾「为什么留旧名」。
--
-- 执行前提：代码侧（manwu adaptor 的 modelSpecs + model/pricing.go 白名单）已发版。
--   若只跑 SQL 不发版，新名会 400（adaptor 认不出）、旧名会从广场消失（白名单已切新名）。
--   正确顺序：**先发 new-api 镜像，再跑本脚本**。
--
-- 全程单事务 + 备份表，幂等可重复执行。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_wan3_tehui_models_20261025 AS
  SELECT * FROM models WHERE model_name LIKE 'wan-3.0-%';
CREATE TABLE IF NOT EXISTS _bak_wan3_tehui_abilities_20261025 AS
  SELECT * FROM abilities WHERE channel_id = 20 AND model LIKE 'wan-3.0-%';
CREATE TABLE IF NOT EXISTS _bak_wan3_tehui_channels_20261025 AS
  SELECT * FROM channels WHERE id = 20;
CREATE TABLE IF NOT EXISTS _bak_wan3_tehui_options_20261025 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- ① 广场卡片改名 + 正文改口径 ----------
-- 只在旧名还在、且新名不存在时改，重复执行无副作用。
UPDATE models
   SET model_name   = 'wan-3.0-特惠',
       description  = 'Wan 3.0 视频生成（Picsart 特惠档）。固定 30 秒一条：¥1.50/次（480P / 720P 同价）。支持 6 档比例（含自适应），≤10 张参考图，不收参考视频。⚠️ 官网串行出片，单条通常 15–25 分钟，急单请选其它模型',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan-3.0-无限'
   AND deleted_at IS NULL
   AND NOT EXISTS (
         SELECT 1 FROM models m2
          WHERE m2.model_name = 'wan-3.0-特惠' AND m2.deleted_at IS NULL
       );

-- 若卡片已是新名（重复执行），只补正文里的旧口径字样。
UPDATE models
   SET description  = 'Wan 3.0 视频生成（Picsart 特惠档）。固定 30 秒一条：¥1.50/次（480P / 720P 同价）。支持 6 档比例（含自适应），≤10 张参考图，不收参考视频。⚠️ 官网串行出片，单条通常 15–25 分钟，急单请选其它模型',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan-3.0-特惠'
   AND deleted_at IS NULL
   AND (description LIKE '%免费无限档%'
        OR description LIKE '%官网免费档%'
        OR description LIKE '%480P ¥1.50%');

-- ---------- ② 价目键：新增 wan-3.0-特惠 = ¥1.50（按次，不分分辨率）----------
-- 单位 USD 等值；1 元 = 1/7.3 USD（与 wan2.7-图生 的 0.3/7.3 同口径）。
-- ¥1.50 → 1.5/7.3 = 0.2054794520547945
--
-- 老板 2026-10-25 定稿：按最低的 480P 定价 ¥1.50，卡片徽标也只显示 ¥1.50。
-- 原先「720P 基准 ¥2.00 + 480P 走 size=0.75」的分档方案作废（代码侧
-- picsart480PSizeRatio 已删除）—— 那会让徽标显示 ¥2，而客户最低能买到 ¥1.50。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan-3.0-特惠}', to_jsonb(1.5 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND COALESCE((value::jsonb ->> 'wan-3.0-特惠')::numeric, -1) <> (1.5 / 7.3)::numeric;

-- 旧名同价（过渡期两个名字下单必须是一个价，否则等于按名字差别定价）。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan-3.0-无限}', to_jsonb(1.5 / 7.3::numeric))::text
 WHERE key = 'ModelPrice'
   AND (value::jsonb ? 'wan-3.0-无限')
   AND COALESCE((value::jsonb ->> 'wan-3.0-无限')::numeric, -1) <> (1.5 / 7.3)::numeric;

-- ---------- ③ abilities：给新名加能力行（channel 20 / group default）----------
-- 从旧名那行整行复制，priority/weight/tag 一并继承，避免两行权重不一致。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'wan-3.0-特惠', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 20
   AND model = 'wan-3.0-无限'
   AND NOT EXISTS (
         SELECT 1 FROM abilities a2
          WHERE a2.channel_id = 20 AND a2.model = 'wan-3.0-特惠'
       );

-- ---------- ④ channels.models：追加新名 ----------
-- 用 array_append 而不是整体覆写，避免冲掉执行前别人新增的模型名。
UPDATE channels
   SET models = array_to_string(
         array_append(string_to_array(models, ','), 'wan-3.0-特惠'), ',')
 WHERE id = 20
   AND NOT (',' || models || ',') LIKE '%,wan-3.0-特惠,%';

COMMIT;

-- ---------- 复核（事务外只读，脚本跑完必须逐项核对）----------
-- 期望：两张卡（wan-3.0 = MegaAI ¥3.00 起、wan-3.0-特惠 = Picsart ¥1.50 按次），
--       正文都不含「无限」字样。这里用 `wan-3.0%`（不带横线）才能同时看到 MegaAI 那张。
SELECT id, model_name, status, left(description, 60) AS descr_head
  FROM models WHERE model_name LIKE 'wan-3.0%' AND deleted_at IS NULL ORDER BY model_name;

-- 期望：channel 20 有两行（新名 + 过渡期旧名），都 enabled = t。
SELECT model, channel_id, "group", enabled FROM abilities
 WHERE model LIKE 'wan-3.0-%' ORDER BY model, channel_id;

-- 期望：models 字段同时含新旧两名，且 MegaAI 的 seedance2.5-特惠 等未被破坏。
SELECT id, name, models, model_mapping FROM channels WHERE id IN (20, 24);

-- 期望：wan-3.0-特惠 = ¥1.50；wan-3.0-无限 = ¥1.50（过渡期同价）；wan-3.0 = ¥3.00（不受影响）。
SELECT e.key, e.value, round((e.value::numeric * 7.3)::numeric, 4) AS cny
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key LIKE 'wan-3.0%' ORDER BY e.key;


-- =====================================================================
-- 为什么留旧名（⑤）
-- =====================================================================
-- 调用链路上还有两个组件按旧名发请求，且**不能与 new-api 同一次发版**：
--   · Worker 应用（cron_video_brach）：shared/constants/heibaidaoModelCapabilities.ts
--     的模型键 = 公开名；改完要重新打包 exe + 用户重启才生效。
--   · ArcReel：server/routers/remote_generation.py 的 PICSART_PUBLIC_MODEL
--     同时是 billing_model_id，要跟 sqlite custom_provider_model 行一起改。
-- 这段时间里若旧名既无能力行又无价目键，客户端会拿到
--   「无可用渠道」或按次计费算成 ¥0（静默免单）—— 后者更难发现。
-- 所以旧名保留：能力行 + channels.models 条目 + 价目键，全部与新名同价。
--
-- 清理条件（三个都满足才删）：
--   1. Worker 新版 exe 已全量铺开（≥7 天，无旧版本回滚）；
--   2. ArcReel PICSART_PUBLIC_MODEL 已改为 wan-3.0-特惠 且 sqlite 行已同步；
--   3. 近 30 天 logs 表按 model_name 统计，wan-3.0-无限 调用数为 0：
--      SELECT model_name, count(*) FROM logs
--       WHERE model_name = 'wan-3.0-无限' AND created_at > EXTRACT(EPOCH FROM now())::bigint - 30*86400
--       GROUP BY model_name;
--   清理内容：删 abilities 旧行 + channels.models 摘掉旧名 + ModelPrice 摘掉旧键
--   + 代码里删掉 ModelWan3TeHuiLegacy / modelSpecs 条目 / defaultModelPrice 旧键
--   + model/pricing.go 白名单保持只含新名（已如此）。


-- =====================================================================
-- 回滚 SQL（整体回到本脚本执行前状态）
-- =====================================================================
--
-- BEGIN;
-- -- ① 卡片名与正文
-- UPDATE models SET model_name = 'wan-3.0-无限',
--        description = (SELECT description FROM _bak_wan3_tehui_models_20261025 WHERE id = 55),
--        updated_time = EXTRACT(EPOCH FROM now())::bigint
--  WHERE model_name = 'wan-3.0-特惠' AND deleted_at IS NULL;
-- -- ② 价目键
-- UPDATE options SET value = (value::jsonb - 'wan-3.0-特惠')::text
--  WHERE key = 'ModelPrice' AND (value::jsonb ? 'wan-3.0-特惠');
-- -- ③ 能力行
-- DELETE FROM abilities WHERE channel_id = 20 AND model = 'wan-3.0-特惠';
-- -- ④ channels.models
-- UPDATE channels SET models = array_to_string(
--          array_remove(string_to_array(models, ','), 'wan-3.0-特惠'), ',')
--  WHERE id = 20 AND (',' || models || ',') LIKE '%,wan-3.0-特惠,%';
-- COMMIT;
--
-- ⚠️ 回滚 SQL 必须与代码回滚同步：新版代码已把 modelSpecs 和广场白名单切到新名，
--    只回滚 DB 会让新名 400、旧名不上广场。
