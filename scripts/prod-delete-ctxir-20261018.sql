-- new-api 生产库：彻底清除 MiniMax-H3-Context-IR（三处同改）
-- 2026-10-18 老板指令：「2个都做」之一。
-- 背景：该模型挂在 status=3 的禁用渠道 21（MiniMax-H3-Video）上，ability enabled=false，
--       属「半下架」——留着有被编辑渠道时复活的风险（UpdateAbilities 按 channel.Models 重建）。
--
-- 三处同改（原理同 prod-hide-seedance25-20261010.sql）：
--   ① channels.models 摘除（渠道 21 的 models 字段此刻只有这一个模型，摘除后为空串）
--   ② abilities 删行
--   ③ models 删卡片（id=41）
--
-- 已核实：tokens.model_limits 无引用；无其它渠道挂载。
-- 注意：渠道 21 本身保留（status=3 禁用），只是 models 字段被清空。
--
-- 回滚（依赖备份表）：
--   BEGIN;
--   UPDATE channels SET models = (SELECT models FROM _bak_ctxir_channels_20261018) WHERE id = 21;
--   INSERT INTO abilities   SELECT * FROM _bak_ctxir_abilities_20261018
--     ON CONFLICT ("group", model, channel_id) DO NOTHING;
--   INSERT INTO models      SELECT * FROM _bak_ctxir_models_20261018
--     ON CONFLICT (id) DO NOTHING;
--   COMMIT;

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_ctxir_channels_20261018 AS
  SELECT * FROM channels WHERE id = 21;
CREATE TABLE IF NOT EXISTS _bak_ctxir_abilities_20261018 AS
  SELECT * FROM abilities WHERE model = 'MiniMax-H3-Context-IR';
CREATE TABLE IF NOT EXISTS _bak_ctxir_models_20261018 AS
  SELECT * FROM models WHERE model_name = 'MiniMax-H3-Context-IR';

-- ---------- ① 渠道 models 摘除（正则只匹配独立项） ----------
UPDATE channels
   SET models = trim(both ',' from regexp_replace(models, '(^|,)MiniMax-H3-Context-IR(,|$)', '\1', 'g'))
 WHERE id = 21
   AND models ~ '(^|,)MiniMax-H3-Context-IR(,|$)';

-- ---------- ② 能力表删行 ----------
DELETE FROM abilities WHERE model = 'MiniMax-H3-Context-IR';

-- ---------- ③ 模型卡片硬删除 ----------
DELETE FROM models WHERE model_name = 'MiniMax-H3-Context-IR';

COMMIT;

-- ---------- 复核 ----------
\echo '--- 渠道 21（models 应为空串，status 仍为 3）---'
SELECT id, name, status, '['||models||']' AS models FROM channels WHERE id = 21;
\echo '--- abilities 残留（应为 0 行）---'
SELECT "group", model, channel_id, enabled FROM abilities WHERE model = 'MiniMax-H3-Context-IR';
\echo '--- 卡片残留（应为 0 行）---'
SELECT id, model_name FROM models WHERE model_name = 'MiniMax-H3-Context-IR';
\echo '--- 备份行数（应各为 1）---'
SELECT (SELECT count(*) FROM _bak_ctxir_channels_20261018)  AS bak_channels,
       (SELECT count(*) FROM _bak_ctxir_abilities_20261018) AS bak_abilities,
       (SELECT count(*) FROM _bak_ctxir_models_20261018)    AS bak_models;
\echo '--- 当前 models 表卡片（应 18-1=17 张） ---'
SELECT count(*) FROM models;
