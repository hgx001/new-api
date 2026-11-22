-- =============================================================================
-- ElevenLabs 档位改名：el-seedance-2.5flash → el-seedance-2.5（2026-11-22）
--
-- 用户口径：「把 flash 去掉，不要带这个 id」。**旧 id 不保留别名**。
--
-- 改动面（四处必须同步，漏一处就是「广场可见但调用 invalid_model」或
-- 「有渠道但广场不显示」）：
--   1. channels.models   ← 路由与 /v1/models 都读它
--   2. abilities         ← 模型广场与路由能力表
--   3. models            ← 广场卡片（描述/标签/供应商）
--   4. options.ModelPrice ← 售价（键名就是模型名）
--
-- ⚠️ 执行顺序：**先发版（代码认识新名）→ 再改 ArcReel 白名单 → 再跑本脚本**。
--    反序会出现「库里有新名、后端还不认」的窗口。
-- ⚠️ 双机各跑一次（api.heibaidao.cn / api2.heibaidao.cn），幂等可重复。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS bak_20261122_elrename_channels AS SELECT * FROM channels;
CREATE TABLE IF NOT EXISTS bak_20261122_elrename_abilities AS SELECT * FROM abilities;
CREATE TABLE IF NOT EXISTS bak_20261122_elrename_models AS SELECT * FROM models;
CREATE TABLE IF NOT EXISTS bak_20261122_elrename_options AS SELECT * FROM options;

-- ---------- 1. channels.models：字符串级改名（保留原顺序与分隔符）----------
UPDATE channels
   SET models = replace(models, 'el-seedance-2.5flash', 'el-seedance-2.5')
 WHERE models LIKE '%el-seedance-2.5flash%';

-- ---------- 2. abilities：先删旧行再插新行（同一渠道/分组）----------
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT "group", 'el-seedance-2.5', channel_id, enabled, priority, weight, tag
  FROM abilities
 WHERE model = 'el-seedance-2.5flash'
   AND NOT EXISTS (
         SELECT 1 FROM abilities x
          WHERE x.model = 'el-seedance-2.5'
            AND x.channel_id = abilities.channel_id
            AND x."group" = abilities."group"
       );

DELETE FROM abilities WHERE model = 'el-seedance-2.5flash';

-- ---------- 3. models：卡片改名（新名不存在时插入，存在则只改描述里的名字）----------
INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, name_rule, created_time, updated_time)
SELECT 'el-seedance-2.5', replace(description, 'el-seedance-2.5flash', 'el-seedance-2.5'),
       icon, tags, vendor_id, endpoints, status, sync_official, name_rule,
       created_time, EXTRACT(EPOCH FROM NOW())::bigint
  FROM models
 WHERE model_name = 'el-seedance-2.5flash'
   AND deleted_at IS NULL
   AND NOT EXISTS (
         SELECT 1 FROM models x WHERE x.model_name = 'el-seedance-2.5' AND x.deleted_at IS NULL
       );

DELETE FROM models WHERE model_name = 'el-seedance-2.5flash';

-- ---------- 4. ModelPrice：键改名（旧键删除，不留别名）----------
UPDATE options
   SET value = (
         (value::jsonb - 'el-seedance-2.5flash')
         || jsonb_build_object('el-seedance-2.5',
                               (value::jsonb -> 'el-seedance-2.5flash'))
       )::text
 WHERE key = 'ModelPrice'
   AND value::jsonb ? 'el-seedance-2.5flash';

COMMIT;

-- ---------- 验收 ----------
-- SELECT models FROM channels WHERE models LIKE '%el-seedance%';
-- SELECT model, "group", channel_id, enabled FROM abilities WHERE model LIKE 'el-seedance%' ORDER BY model;
-- SELECT model_name, status FROM models WHERE model_name LIKE 'el-seedance%' AND deleted_at IS NULL;
-- SELECT (value::jsonb ? 'el-seedance-2.5flash') AS old_left,
--        (value::jsonb -> 'el-seedance-2.5') AS new_price
--   FROM options WHERE key = 'ModelPrice';
