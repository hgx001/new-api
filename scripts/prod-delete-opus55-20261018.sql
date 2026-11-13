-- ============================================================================
-- 生产：彻底删除 opus-5.5（对外售卖名，实际路由 gpt-6-luna）
-- 日期：2026-10-18
-- 原因：实测该模型自认「OpenAI 的 ChatGPT」，明确否认是 Anthropic Claude Opus。
--       以 Claude 的名字售卖 GPT 模型属误导，老板决定下架删除，要诚实。
--
-- 删除范围（六处，单事务）：
--   ① channels.models         渠道 13 摘除 opus-5.5
--   ② channels.model_mapping  渠道 13 删除 opus-5.5 → gpt-6-luna 映射
--   ③ abilities               删能力行（决定 /v1/models 与路由）
--   ④ models                  删广场卡片
--   ⑤ options.ModelRatio      删 0.32106 倍率键
--   ⑥ options.CompletionRatio 删 8 倍率键
--
-- 备份表：_bak_del_opus55_{channels,abilities,models,options}_20261018
--
-- ============================ 回滚 SQL ============================
-- 从备份恢复（整行快照，按需还原）：
--   UPDATE channels c SET models = b.models, model_mapping = b.model_mapping
--     FROM _bak_del_opus55_channels_20261018 b WHERE c.id = b.id;
--   INSERT INTO abilities SELECT * FROM _bak_del_opus55_abilities_20261018;
--   INSERT INTO models    SELECT * FROM _bak_del_opus55_models_20261018;
--   UPDATE options o SET value = b.value FROM _bak_del_opus55_options_20261018 b
--     WHERE o.key = b.key;
-- 注意：回滚只恢复「能卖」的状态，不代表推荐这样做。
-- ============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份 ----------
DROP TABLE IF EXISTS _bak_del_opus55_channels_20261018;
CREATE TABLE _bak_del_opus55_channels_20261018 AS
SELECT * FROM channels
WHERE models ~ '(^|,)opus-5\.5(,|$)' OR COALESCE(model_mapping, '') LIKE '%opus-5.5%';

DROP TABLE IF EXISTS _bak_del_opus55_abilities_20261018;
CREATE TABLE _bak_del_opus55_abilities_20261018 AS
SELECT * FROM abilities WHERE model = 'opus-5.5';

DROP TABLE IF EXISTS _bak_del_opus55_models_20261018;
CREATE TABLE _bak_del_opus55_models_20261018 AS
SELECT * FROM models WHERE model_name = 'opus-5.5';

DROP TABLE IF EXISTS _bak_del_opus55_options_20261018;
CREATE TABLE _bak_del_opus55_options_20261018 AS
SELECT key, value FROM options WHERE key IN ('ModelRatio', 'CompletionRatio', 'ModelPrice');

-- ---------- ① 渠道 models 摘除（锚定边界，不会误伤 opus-5.50 之类）----------
UPDATE channels
SET models = trim(both ',' from regexp_replace(models, '(^|,)opus-5\.5(,|$)', '\1', 'g'))
WHERE models ~ '(^|,)opus-5\.5(,|$)';

-- ---------- ② 渠道 model_mapping 删键 ----------
UPDATE channels
SET model_mapping = (model_mapping::jsonb - 'opus-5.5')::text
WHERE COALESCE(model_mapping, '') LIKE '%opus-5.5%';

-- ---------- ③ 删能力行 ----------
DELETE FROM abilities WHERE model = 'opus-5.5';

-- ---------- ④ 删广场卡片 ----------
DELETE FROM models WHERE model_name = 'opus-5.5';

-- ---------- ⑤⑥ 删倍率键 ----------
UPDATE options
SET value = (value::jsonb - 'opus-5.5')::text
WHERE key = 'ModelRatio' AND value::jsonb ? 'opus-5.5';

UPDATE options
SET value = (value::jsonb - 'opus-5.5')::text
WHERE key = 'CompletionRatio' AND value::jsonb ? 'opus-5.5';

-- ModelPrice 本无该键，兜底同删（有则删，无则不动）
UPDATE options
SET value = (value::jsonb - 'opus-5.5')::text
WHERE key = 'ModelPrice' AND value::jsonb ? 'opus-5.5';

COMMIT;

-- ---------- 复核 ----------
\echo '--- 六处残留（全部应为 0）---'
SELECT
  (SELECT count(*) FROM abilities WHERE model = 'opus-5.5')                              AS abilities_left,
  (SELECT count(*) FROM models WHERE model_name = 'opus-5.5')                            AS cards_left,
  (SELECT count(*) FROM channels WHERE models ~ '(^|,)opus-5\.5(,|$)')                    AS channel_models_left,
  (SELECT count(*) FROM channels WHERE COALESCE(model_mapping,'') LIKE '%opus-5.5%')      AS mapping_left,
  (SELECT count(*) FROM options WHERE key = 'ModelRatio' AND value::jsonb ? 'opus-5.5')   AS ratio_left,
  (SELECT count(*) FROM options WHERE key = 'CompletionRatio' AND value::jsonb ? 'opus-5.5') AS comp_left;

\echo '--- 渠道 13 现状 ---'
SELECT id, name, status, models, model_mapping FROM channels WHERE id = 13;

\echo '--- 备份行数 ---'
SELECT
  (SELECT count(*) FROM _bak_del_opus55_channels_20261018)  AS bak_channels,
  (SELECT count(*) FROM _bak_del_opus55_abilities_20261018) AS bak_abilities,
  (SELECT count(*) FROM _bak_del_opus55_models_20261018)    AS bak_models,
  (SELECT count(*) FROM _bak_del_opus55_options_20261018)   AS bak_options;

\echo '--- 存量对照（不应变化）---'
SELECT
  (SELECT count(*) FROM models)               AS cards_total,
  (SELECT count(DISTINCT model) FROM abilities) AS ability_models,
  (SELECT count(*) FROM abilities WHERE enabled) AS enabled_rows;
