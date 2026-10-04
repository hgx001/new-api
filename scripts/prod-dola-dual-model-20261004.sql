-- new-api 生产库：漫屋 dola 双模型（seedance-2.0 / seedance-2.5）上线
-- 旧公开名 dola-seedance-2.5 一并退役（adaptor 已不再识别该名）
-- 全程单事务 + 备份表，出错即回滚。

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_dola_dual_channels_20261004 AS
  SELECT * FROM channels WHERE id = 20;

CREATE TABLE IF NOT EXISTS _bak_dola_dual_abilities_20261004 AS
  SELECT * FROM abilities WHERE channel_id = 20;

CREATE TABLE IF NOT EXISTS _bak_dola_dual_models_20261004 AS
  SELECT * FROM models WHERE model_name IN ('dola-seedance-2.5');

CREATE TABLE IF NOT EXISTS _bak_dola_dual_options_20261004 AS
  SELECT * FROM options WHERE key = 'ModelPrice';

-- ---------- 渠道模型表 ----------
UPDATE channels
   SET models = 'seedance-2.0,seedance-2.5,gemini-web-video,Nano Banana Pro,jimeng-video-reverse'
 WHERE id = 20;

-- ---------- 能力表：改名 + 新增 2.5 ----------
UPDATE abilities
   SET model = 'seedance-2.0'
 WHERE channel_id = 20 AND model = 'dola-seedance-2.5';

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'seedance-2.5', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE channel_id = 20 AND model = 'seedance-2.0';

-- ---------- 模型广场：改名 + 新增 2.5 ----------
UPDATE models
   SET model_name = 'seedance-2.0',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'dola-seedance-2.5' AND deleted_at IS NULL;

INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, created_time, updated_time, name_rule)
SELECT 'seedance-2.5', description, icon, tags, vendor_id, endpoints,
       status, sync_official, created_time, EXTRACT(EPOCH FROM now())::bigint, name_rule
  FROM models
 WHERE model_name = 'seedance-2.0' AND deleted_at IS NULL;

-- ---------- 单价（USD 等值；1 元 = 1/7.3 USD）----------
UPDATE options
   SET value = jsonb_set(
         jsonb_set(value::jsonb, '{seedance-2.0}', to_jsonb(1.5 / 7.3::numeric)),
                    '{seedance-2.5}', to_jsonb(1.0 / 7.3::numeric))
     - 'dola-seedance-2.5'
 WHERE key = 'ModelPrice';

COMMIT;

-- ---------- 复核 ----------
SELECT id, models FROM channels WHERE id = 20;

SELECT model, "group", enabled, priority, weight
  FROM abilities WHERE channel_id = 20 ORDER BY model;

SELECT model_name, status FROM models
 WHERE deleted_at IS NULL AND model_name IN ('seedance-2.0', 'seedance-2.5', 'dola-seedance-2.5');

SELECT e.key, e.value
  FROM options o, LATERAL jsonb_each_text(o.value::jsonb) e
 WHERE o.key = 'ModelPrice' AND e.key IN ('seedance-2.0', 'seedance-2.5', 'dola-seedance-2.5');
