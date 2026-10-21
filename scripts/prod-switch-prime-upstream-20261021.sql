-- new-api 生产库：wan3.0-video-prime-1080p 换上游（2026-10-21 用户定稿）
--
-- 背景：原上游「有赞 Wan3」（channel 17）的 wan3满血30秒 已被上游撤下 ——
--   2026-10-16 起上游对该渠道返回「模型 …不存在或未启用」（该文案不在本站代码里），
--   上游 /v1/models 实时目录里已无 prime。对外模型名与 ¥8.00/次 的定价**不变**，
--   只把实际上游换成三元巡狩（channel 23）的 WAN 3.0 实时工作流。
--
-- 目标上游（2026-10-21 读 GET /v1/workflows 实测）：
--   工作流名「WAN 3.0（ 720P/1080P · 30s）」（全角括号+空格+间隔号，逐字匹配）
--   ¥4/次、实时（offline=false）、720p/1080p 竖横、5–30 秒、图 ≤10 / 音 ≤5
--
-- ⚠️ 两条能力上限必须同步改文案（开放 API 无对应入参，不是「少支持一点」而是「传了就 400」）：
--   · 参考视频：prime 原宣传 ≤5 个 → **不支持**（上游 catalog 的 params 无 video 字段）
--   · 首尾帧：prime 原宣传各 1 张 → **不支持**（上游无 first_frame / last_frame 字段）
--   · 参考图：≤8 → ≤9；参考音频：≤5 → ≤3（取合并协议文档口径，比 capabilities 更严）
--
-- 为什么不选同平台的「wanwan 1080P（拼车）」（= wan3.0-1080p-慢速 现用工作流）：
--   拼车档是 offline 次日发车（12–14 点 / 20 点发车），把 ¥8 的实时满血档变成次日档，
--   而 ¥2 的慢速档已经在卖同一批片子 → 用户花 4 倍钱等一天，必然投诉退款。
--   慢速需求由 wan3.0-1080p-慢速 覆盖，prime 保住「实时 30 秒 1080P」的定位。
--
-- 三处同改（三条链路各自的驱动源，缺一不可）：
--   · 路由     ← channels.models（source of truth）+ abilities
--   · 广场卡片 ← models.description
--   · 计费单价 ← options.ModelPrice（**本次不变**：¥8.00/次 = 8/7.3 =
--                1.095890410958904，仍由 options 覆盖代码默认值）
--
-- ⚠️ 顺序：必须**先发版**（deploy/deploy-backend.sh <sha>，让 sanyuan 适配器认识
--   prime 并按 WAN 3.0 工作流建单），再跑本脚本。反序会出现「模型可见但一调就 400
--   invalid_model」：abilities 已有 prime，旧二进制的适配器却不认这个模型名。
--
-- 回滚（依赖本次备份表）：
--   BEGIN;
--   UPDATE channels SET models = (SELECT models FROM _bak_prime_switch_channels_20261021 WHERE id = 17) WHERE id = 17;
--   UPDATE channels SET models = (SELECT models FROM _bak_prime_switch_channels_20261021 WHERE id = 23) WHERE id = 23;
--   DELETE FROM abilities WHERE channel_id = 23 AND model = 'wan3.0-video-prime-1080p';
--   INSERT INTO abilities SELECT * FROM _bak_prime_switch_abilities_20261021;
--   UPDATE models SET description = (SELECT description FROM _bak_prime_switch_models_20261021
--     WHERE model_name = 'wan3.0-video-prime-1080p')
--    WHERE model_name = 'wan3.0-video-prime-1080p';
--   COMMIT;

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_prime_switch_channels_20261021 AS
  SELECT * FROM channels WHERE id IN (17, 23);

CREATE TABLE IF NOT EXISTS _bak_prime_switch_abilities_20261021 AS
  SELECT * FROM abilities WHERE (channel_id = 17 AND model = 'wan3.0-video-prime-1080p')
                           OR (channel_id = 23 AND model IN ('wan3.0-1080p-慢速', 'wan3.0-video-prime-1080p'));

CREATE TABLE IF NOT EXISTS _bak_prime_switch_models_20261021 AS
  SELECT * FROM models WHERE model_name = 'wan3.0-video-prime-1080p';

-- ---------- ① 渠道模型表：路由源头，必须先于 abilities ----------
-- 渠道 17（有赞 Wan3）：摘掉 prime，只留 smart / r2v。
UPDATE channels
   SET models = array_to_string(
         ARRAY(SELECT trim(item) FROM unnest(string_to_array(models, ',')) AS item
                WHERE trim(item) <> '' AND trim(item) <> 'wan3.0-video-prime-1080p'),
         ',')
 WHERE id = 17;

-- 渠道 23（三元巡狩）：加上 prime，与慢速档同渠道同 key。
UPDATE channels
   SET models = models || ',wan3.0-video-prime-1080p'
 WHERE id = 23 AND models NOT LIKE '%wan3.0-video-prime-1080p%';

-- ---------- ② 能力表：删旧、加新（group / priority / weight / tag 沿用慢速档那一行） ----------
-- 只删不加会 503 model_not_found；只加不删则两渠道同时可用，路由可能仍命中已撤模型的 17。
DELETE FROM abilities
 WHERE channel_id = 17 AND model = 'wan3.0-video-prime-1080p';

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'wan3.0-video-prime-1080p', 23, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 23 AND model = 'wan3.0-1080p-慢速'
ON CONFLICT DO NOTHING;

-- ---------- ③ 模型广场卡片描述：与新上游的真实能力对齐 ----------
UPDATE models
   SET description = '满血 30 秒档，固定 30 秒 1080P，¥8.00/次，实时出片。支持参考图 ≤9 张、参考音频 ≤3 条；不支持参考视频与首尾帧（传了直接报错，不消耗费用）。'
 WHERE model_name = 'wan3.0-video-prime-1080p' AND deleted_at IS NULL;

-- ---------- ④ 计费单价：本次不变（¥8.00/次 = 8/7.3），只做复核不写库 ----------
-- options.ModelPrice 会覆盖代码 defaultModelPrice；若被误改成别的值，下面这条会暴露。

COMMIT;

-- ---------- 复核 ----------
\echo '--- 渠道 17 / 23 的 models（17 应无 prime，23 应有 prime）---'
SELECT id, name, type, status, models FROM channels WHERE id IN (17, 23) ORDER BY id;

\echo '--- prime 的能力表（应只剩 channel 23，且各 group 与慢速档一致）---'
SELECT "group", model, channel_id, enabled, priority, weight
  FROM abilities
 WHERE model IN ('wan3.0-video-prime-1080p', 'wan3.0-1080p-慢速')
 ORDER BY model, "group";

\echo '--- 慢速档的 group/priority/weight 基准（prime 必须逐项一致）---'
SELECT "group", channel_id, enabled, priority, weight, tag
  FROM abilities WHERE channel_id = 23 AND model = 'wan3.0-1080p-慢速' ORDER BY "group";

\echo '--- 旧上游残留检查：channel 17 不应再有任何 prime 能力（应为 0 行）---'
SELECT count(*) AS stale_rows FROM abilities
 WHERE channel_id = 17 AND model = 'wan3.0-video-prime-1080p';

\echo '--- 模型广场卡片描述 ---'
SELECT model_name, status, description FROM models
 WHERE model_name = 'wan3.0-video-prime-1080p' AND deleted_at IS NULL;

\echo '--- ModelPrice 单价（应为 1.095890410958904 = ¥8.00/次，本次不应被改动）---'
SELECT value::jsonb -> 'wan3.0-video-prime-1080p' AS prime_per_call
  FROM options WHERE key = 'ModelPrice';
