-- new-api 生产库：漫屋豆包系图片模型双上架（2026-11-07）
--
-- 两个新公开模型（公开名 = 官网显示名，一个字不改）：
--   - Seedream 5.0 Flash → 豆包 www.doubao.com「图像生成」默认模型
--   - Seedream 4.5      → dola www.dola.com/chat/create-image 默认模型
--
-- 定价：两模型均 ¥0.20/张（内部按 USD 等值 0.2 / 7.3 存 ModelPrice，
-- count 通过 OtherRatios{"n": count} 乘到单张价上）。
-- 既有 `Nano Banana Pro` 保持 ¥0.30/张 —— 用户口径「定价两毛一张」针对
-- **新增的两个模型**，不是把整个图片品类改成两毛。
--
-- ⚠️ 必须写 option 而不能只靠代码默认值：
-- types.LoadFromJsonStringWithCallback 是**整表替换**，生产 option 里没有的键
-- 会被代码 model_ratio.go 的默认值冲掉（价格变 0）。见 AGENTS.md
-- 「生产 ModelPrice option 会覆盖代码默认值」。
--
-- 前置：后端已发版到含 Seedream 支持的提交（先发版后跑 SQL，
-- 保证无「可见但不可调」窗口）。
-- 回滚：见文件末尾注释。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_seedream_channels_20261107 AS
  SELECT * FROM channels WHERE id = 20;

CREATE TABLE IF NOT EXISTS _bak_seedream_abilities_20261107 AS
  SELECT * FROM abilities WHERE channel_id = 20;

CREATE TABLE IF NOT EXISTS _bak_seedream_models_20261107 AS
  SELECT * FROM models WHERE model_name IN ('Seedream 5.0 Flash', 'Seedream 4.5');

CREATE TABLE IF NOT EXISTS _bak_seedream_options_20261107 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- 渠道模型表 ----------
-- 追加两个公开名（不覆盖既有清单，避免误删其它在售模型）。
UPDATE channels
   SET models = trim(both ',' from models || ',Seedream 5.0 Flash,Seedream 4.5')
 WHERE id = 20
   AND models NOT LIKE '%Seedream 5.0 Flash%'
   AND models NOT LIKE '%Seedream 4.5%';

-- 若两模型中只有一个已在清单里（幂等重跑），补齐缺的那个。
UPDATE channels
   SET models = trim(both ',' from models || ',Seedream 5.0 Flash')
 WHERE id = 20 AND models NOT LIKE '%Seedream 5.0 Flash%';

UPDATE channels
   SET models = trim(both ',' from models || ',Seedream 4.5')
 WHERE id = 20 AND models NOT LIKE '%Seedream 4.5%';

-- ---------- 能力表：新增两个模型 ----------
-- group / enabled / priority / weight / tag 继承同渠道既有图片模型
-- （`Nano Banana Pro` 走的是同一条 ArcReel 图片链路）。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'Seedream 5.0 Flash', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 20 AND model = 'Nano Banana Pro'
   AND NOT EXISTS (
     SELECT 1 FROM abilities
      WHERE channel_id = 20 AND model = 'Seedream 5.0 Flash'
   )
 LIMIT 1;

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'Seedream 4.5', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 20 AND model = 'Nano Banana Pro'
   AND NOT EXISTS (
     SELECT 1 FROM abilities
      WHERE channel_id = 20 AND model = 'Seedream 4.5'
   )
 LIMIT 1;

-- ---------- 模型广场：新增两张卡 ----------
-- 描述承载全部信息（vendor/icon/tags 留空，与既有漫屋卡片同风格）。
INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time)
SELECT 'Seedream 5.0 Flash',
       '豆包官网图像生成模型，按张计费 ¥0.20/张。比例支持自动/9:16/2:3/3:4/1:1/4:3/3:2/16:9，未指定时保持官网出厂的「自动」档。单次最多 4 张。',
       1, 1, EXTRACT(EPOCH FROM NOW())::bigint, EXTRACT(EPOCH FROM NOW())::bigint
 WHERE NOT EXISTS (
   SELECT 1 FROM models WHERE model_name = 'Seedream 5.0 Flash' AND deleted_at IS NULL
 );

INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time)
SELECT 'Seedream 4.5',
       'dola 官网图像生成模型，按张计费 ¥0.20/张。比例支持自动/9:16/2:3/3:4/1:1/4:3/3:2/16:9，未指定时保持官网出厂的「自动」档。单次最多 4 张。',
       1, 1, EXTRACT(EPOCH FROM NOW())::bigint, EXTRACT(EPOCH FROM NOW())::bigint
 WHERE NOT EXISTS (
   SELECT 1 FROM models WHERE model_name = 'Seedream 4.5' AND deleted_at IS NULL
 );

-- 已存在则刷新描述（幂等）
UPDATE models
   SET description = '豆包官网图像生成模型，按张计费 ¥0.20/张。比例支持自动/9:16/2:3/3:4/1:1/4:3/3:2/16:9，未指定时保持官网出厂的「自动」档。单次最多 4 张。',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'Seedream 5.0 Flash' AND deleted_at IS NULL;

UPDATE models
   SET description = 'dola 官网图像生成模型，按张计费 ¥0.20/张。比例支持自动/9:16/2:3/3:4/1:1/4:3/3:2/16:9，未指定时保持官网出厂的「自动」档。单次最多 4 张。',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'Seedream 4.5' AND deleted_at IS NULL;

-- ---------- 单价（USD 等值；1 元 = 1/7.3 USD）----------
-- ⚠️ jsonb_set 只在键不存在时写入；已存在则用嵌套 jsonb_set 覆盖为同值，
-- 保证幂等重跑不会把价格改回旧值。
UPDATE options
   SET value = jsonb_set(
         jsonb_set(value::jsonb,
                   '{Seedream 5.0 Flash}',
                   to_jsonb(0.2 / 7.3::numeric),
                   true),
         '{Seedream 4.5}',
         to_jsonb(0.2 / 7.3::numeric),
         true)
 WHERE key = 'ModelPrice';

COMMIT;

-- ---------- 复核 ----------
SELECT id, models FROM channels WHERE id = 20;

SELECT model, "group", enabled, priority, weight
  FROM abilities WHERE channel_id = 20 AND model IN ('Seedream 5.0 Flash', 'Seedream 4.5', 'Nano Banana Pro')
 ORDER BY model;

SELECT model_name, status FROM models
 WHERE deleted_at IS NULL AND model_name IN ('Seedream 5.0 Flash', 'Seedream 4.5', 'Nano Banana Pro');

-- ⚠️ Nano Banana Pro 必须是 0.3/7.3，不许被一并降价。
SELECT e.key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice'
   AND e.key IN ('Seedream 5.0 Flash', 'Seedream 4.5', 'Nano Banana Pro');

-- ---------- 回滚 ----------
-- BEGIN;
-- UPDATE channels SET models = (SELECT models FROM _bak_seedream_channels_20261107 WHERE id = 20)
--  WHERE id = 20;
-- DELETE FROM abilities WHERE channel_id = 20
--   AND model IN ('Seedream 5.0 Flash', 'Seedream 4.5');
-- DELETE FROM models WHERE model_name IN ('Seedream 5.0 Flash', 'Seedream 4.5')
--   AND deleted_at IS NULL;
-- UPDATE options SET value = (SELECT value FROM _bak_seedream_options_20261107
--   WHERE key = 'ModelPrice') WHERE key = 'ModelPrice';
-- COMMIT;
