-- new-api 生产库：漫屋豆包官网两个模型改名 + 改价（2026-10-12 用户定稿）
--
-- 改名（旧名彻底退役，调用返回 400 invalid_model / 无渠道 503）：
--   db-seedance-2-0  →  seedance-2-0-fast   （同时由 ¥1/次 改为 ¥0.1/秒，5-15 秒逐秒）
--   db-seedance-2-5  →  seedance-2-5        （同时由 ¥2/次 改为 ¥3/次）
--
-- ⚠️ 与 dola 两个模型只差分隔符，**不要混淆**：
--   seedance-2.0 / seedance-2.5（点号）= dola 官网，本次不动；
--   seedance-2-0-fast / seedance-2-5（连字符）= 豆包官网，本次改名对象。
--   正则一律按「独立逗号项」匹配，避免 seedance-2.5 误伤 seedance-2-5。
--
-- 为什么必须三处同改（三条链路各自的驱动源）：
--   · 路由（线上 MEMORY_CACHE_ENABLED=true）← channels.models 字段本身
--     （model/channel_cache.go InitChannelCache，每 SYNC_FREQUENCY=60s 重建）
--   · 广场 /api/pricing ← abilities(enabled) × isAllowedPricingModel 白名单（已随发版改）
--   · 下游 /v1/models  ← abilities(enabled) × HasModelBillingConfig
--   · 计费单价          ← options.ModelPrice（**会覆盖代码 defaultModelPrice**，
--                          只改 setting/ratio_setting/model_ratio.go 线上不变）
--   若只改 abilities：渠道一旦被后台编辑，model.UpdateAbilities 会按 channels.models
--   删光重建 → 旧名复活、新名丢失。故 channels.models 是源头，必须先改它。
--
-- 回滚（依赖本次备份表）：
--   BEGIN;
--   UPDATE channels SET models = (SELECT models FROM _bak_doubao_rename_channels_20261012) WHERE id = 20;
--   DELETE FROM abilities WHERE channel_id = 20
--     AND model IN ('seedance-2-0-fast','seedance-2-5');
--   INSERT INTO abilities SELECT * FROM _bak_doubao_rename_abilities_20261012;
--   UPDATE models SET model_name = 'db-seedance-2-0', description = 'Seedance 2.0 Fast 视频生成。按次收费 ¥1/次，支持 5/10/15 秒（不支持 30 秒）'
--    WHERE model_name = 'seedance-2-0-fast';
--   UPDATE models SET model_name = 'db-seedance-2-5', description = 'Seedance 2.5 视频生成。按次收费 ¥2/次，支持 5/10/15/30 秒（时长不影响价格）'
--    WHERE model_name = 'seedance-2-5';
--   UPDATE options SET value = (SELECT value FROM _bak_doubao_rename_options_20261012) WHERE key = 'ModelPrice';
--   COMMIT;

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_doubao_rename_channels_20261012 AS
  SELECT * FROM channels WHERE id = 20;

CREATE TABLE IF NOT EXISTS _bak_doubao_rename_abilities_20261012 AS
  SELECT * FROM abilities WHERE channel_id = 20 AND model IN ('db-seedance-2-0','db-seedance-2-5');

CREATE TABLE IF NOT EXISTS _bak_doubao_rename_models_20261012 AS
  SELECT * FROM models WHERE model_name IN ('db-seedance-2-0','db-seedance-2-5');

CREATE TABLE IF NOT EXISTS _bak_doubao_rename_options_20261012 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- ① 渠道模型表：改名（路由源头，必须先于 abilities） ----------
UPDATE channels
   SET models = replace(models, 'db-seedance-2-0', 'seedance-2-0-fast')
 WHERE id = 20 AND models ~ '(^|,)db-seedance-2-0(,|$)';

UPDATE channels
   SET models = replace(models, 'db-seedance-2-5', 'seedance-2-5')
 WHERE id = 20 AND models ~ '(^|,)db-seedance-2-5(,|$)';

-- ---------- ② 能力表：改名（保留 priority / weight / group） ----------
UPDATE abilities SET model = 'seedance-2-0-fast'
 WHERE channel_id = 20 AND model = 'db-seedance-2-0';

UPDATE abilities SET model = 'seedance-2-5'
 WHERE channel_id = 20 AND model = 'db-seedance-2-5';

-- ---------- ③ 模型元数据：改名 + 卡片描述/图标/供应商 ----------
-- description 是**模型广场卡片正文**，必须与新名、新价一致，否则卡片继续写旧名旧价。
UPDATE models
   SET model_name = 'seedance-2-0-fast',
       description = '漫屋豆包官网 Seedance 2.0 Fast 文/图生视频。5–15 秒逐秒可选，按秒计费 ¥0.10/秒；7 档比例（含 auto），≤10 张参考图，无分辨率档位',
       icon = 'Doubao.Color',
       tags = 'video',
       vendor_id = 6
 WHERE model_name = 'db-seedance-2-0' AND deleted_at IS NULL;

UPDATE models
   SET model_name = 'seedance-2-5',
       description = '漫屋豆包官网 Seedance 2.5 文/图生视频。5/10/15/30 秒四档，按次计费 ¥3.00/次；7 档比例（含 auto），≤10 张参考图，无分辨率档位',
       icon = 'Doubao.Color',
       tags = 'video',
       vendor_id = 6
 WHERE model_name = 'db-seedance-2-5' AND deleted_at IS NULL;

-- ---------- ④ 计费单价：删旧键、写新键（USD 等值，USD2RMB=7.3） ----------
-- 0.1/7.3 = 0.0136986301369863（¥0.10/秒）；3/7.3 = 0.410958904109589（¥3.00/次）
-- ⚠️ 全程用 jsonb 运算，**只在最后一步** ::text 回写 options.value：
--    jsonb_set(text, ...) 不存在该函数签名（2026-10-12 dry-run 实证报错）。
UPDATE options
   SET value = jsonb_set(
         jsonb_set(
           (value::jsonb - 'db-seedance-2-0') - 'db-seedance-2-5',
           '{seedance-2-0-fast}', to_jsonb(0.1 / 7.3::numeric)
         ),
         '{seedance-2-5}', to_jsonb(3.0 / 7.3::numeric)
       )::text
 WHERE key = 'ModelPrice';

COMMIT;

-- ---------- 复核 ----------
\echo '--- 渠道 20 models（应含 seedance-2-0-fast / seedance-2-5，无 db-*）---'
SELECT id, name, status, models FROM channels WHERE id = 20;

\echo '--- 渠道 20 能力全貌 ---'
SELECT "group", model, channel_id, enabled, priority, weight FROM abilities WHERE channel_id = 20 ORDER BY model;

\echo '--- 旧名残留（abilities / channels.models / models 应均为 0）---'
SELECT 'abilities' AS src, count(*) FROM abilities WHERE model LIKE 'db-seedance%'
UNION ALL SELECT 'channels', count(*) FROM channels WHERE models LIKE '%db-seedance%'
UNION ALL SELECT 'models', count(*) FROM models WHERE model_name LIKE 'db-seedance%';

\echo '--- 模型广场卡片元数据 ---'
SELECT model_name, description, icon, tags, vendor_id, status FROM models
 WHERE model_name LIKE 'seedance-2%' AND deleted_at IS NULL ORDER BY model_name;

\echo '--- ModelPrice 单价（应为 0.0136986… / 0.4109589…）---'
SELECT value::jsonb -> 'seedance-2-0-fast' AS fast_per_sec,
       value::jsonb -> 'seedance-2-5'      AS per_call
  FROM options WHERE key = 'ModelPrice';
