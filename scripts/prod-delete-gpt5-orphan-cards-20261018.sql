-- new-api 生产库：删除 8 张下架残留卡片（gpt-5 家族），它们无任何 abilities 行
-- 却会出现在下游 /v1/models 列表里（调用全部 503，纯展示噪音）。
-- 2026-10-18 老板指令：「做，2个幽灵先不管」——即只删这 8 张卡片，
-- gpt-5.6 / gpt-5.6-terra 两个幽灵名（无卡片无 ability，疑似 fork 构建的 ListModels
-- 内置名单）暂不处理。
--
-- 目标清单（models 表 id → 名称）：
--   2|gpt-5  3|gpt-5.1  4|gpt-5.4  12|gpt-5.4-mini  5|gpt-5.5
--   10|gpt-5-mini  11|gpt-5-nano  13|gpt-5-chat-latest
--
-- 安全性已预先核实：
--   · abilities 无这些模型的任何行（不需要动能力表）
--   · channels.models 拆词后不含这些名字（不需要动渠道字段，无「编辑即复活」风险）
--   · 无任何 token 的 model_limits 引用它们
--   · logs / tasks 的 model_name 是字符串快照，删卡片不影响历史账单
--
-- 回滚（依赖备份表）：
--   INSERT INTO models SELECT * FROM _bak_del_gpt5_cards_20261018 ON CONFLICT (id) DO NOTHING;

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_del_gpt5_cards_20261018 AS
  SELECT * FROM models
  WHERE model_name IN ('gpt-5','gpt-5.1','gpt-5.4','gpt-5.4-mini','gpt-5.5',
                       'gpt-5-mini','gpt-5-nano','gpt-5-chat-latest');

-- ---------- 删除残留卡片 ----------
DELETE FROM models
  WHERE model_name IN ('gpt-5','gpt-5.1','gpt-5.4','gpt-5.4-mini','gpt-5.5',
                       'gpt-5-mini','gpt-5-nano','gpt-5-chat-latest');

COMMIT;

-- ---------- 复核 ----------
\echo '--- 备份行数（应为 8）---'
SELECT count(*) FROM _bak_del_gpt5_cards_20261018;
\echo '--- 残留卡片（应为 0 行）---'
SELECT id, model_name FROM models
 WHERE model_name IN ('gpt-5','gpt-5.1','gpt-5.4','gpt-5.4-mini','gpt-5.5',
                      'gpt-5-mini','gpt-5-nano','gpt-5-chat-latest');
\echo '--- 剩余 cards 数（38-8=30）---'
SELECT count(*) FROM models;
\echo '--- 仍在线的模型能力（不受影响）---'
SELECT DISTINCT model FROM abilities WHERE enabled=true ORDER BY 1;
