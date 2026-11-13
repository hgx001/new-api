-- new-api 生产库：删除剩余 12 张「下架残留」卡片（无 ability、无渠道引用、无 token 引用）
-- 2026-10-18 老板指令：接续 8 张 gpt-5.x 卡片的清理（prod-delete-gpt5-orphan-cards-20261018.sql），
-- 把同类死数据清干净——防的是日后有人从库里翻名字往渠道里加。
--
-- 目标 12 张（models 表 id → 名称）：
--   1|deepseek                14|wan3.0-video           15|deepseek-v4-flash
--   17|doubao-seedream-5-0-260128
--   18|gemini-3-pro-image-preview   19|gemini-3.1-flash-image-preview
--   20|aliyun:wan-3.0   21|hailuo-h3-cankaosheng-fast   22|hailuo-h3-cankaosheng-night
--   23|hmseedance_v2.0不支持真人   39|MiniMax-H3   40|MiniMax-H3-Max
--
-- 执行前已逐项核实（本次查询）：
--   · abilities 无这些模型的任何行
--   · channels.models 拆词后不含这些名字（无「编辑即复活」风险）
--   · 无任何 token 的 model_limits 引用
--   · logs / tasks / quota_data 的 model_name 是历史快照，删卡片不影响账单
--
-- ⚠️ 不在本次范围：MiniMax-H3-Context-IR（id=41）——它有 abilities 行（enabled=false，
--    挂在 status=3 的禁用渠道 21 上），属「半下架」而非死卡片，需单独决策。
--
-- 回滚（依赖备份表）：
--   INSERT INTO models SELECT * FROM _bak_del_orphan_cards_20261018 ON CONFLICT (id) DO NOTHING;

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_del_orphan_cards_20261018 AS
  SELECT * FROM models
  WHERE model_name IN ('deepseek','wan3.0-video','deepseek-v4-flash',
                       'doubao-seedream-5-0-260128','gemini-3-pro-image-preview',
                       'gemini-3.1-flash-image-preview','aliyun:wan-3.0',
                       'hailuo-h3-cankaosheng-fast','hailuo-h3-cankaosheng-night',
                       'hmseedance_v2.0不支持真人','MiniMax-H3','MiniMax-H3-Max');

-- ---------- 删除 ----------
DELETE FROM models
  WHERE model_name IN ('deepseek','wan3.0-video','deepseek-v4-flash',
                       'doubao-seedream-5-0-260128','gemini-3-pro-image-preview',
                       'gemini-3.1-flash-image-preview','aliyun:wan-3.0',
                       'hailuo-h3-cankaosheng-fast','hailuo-h3-cankaosheng-night',
                       'hmseedance_v2.0不支持真人','MiniMax-H3','MiniMax-H3-Max');

COMMIT;

-- ---------- 复核 ----------
\echo '--- 备份行数（应为 12）---'
SELECT count(*) FROM _bak_del_orphan_cards_20261018;
\echo '--- 剩余无 ability 卡片（应为 0 行）---'
SELECT m.id, m.model_name FROM models m
 WHERE NOT EXISTS (SELECT 1 FROM abilities a WHERE a.model = m.model_name);
\echo '--- models 表总数（30-12=18）---'
SELECT count(*) FROM models;
\echo '--- 在线能力全貌（不受影响，应为 23+1=24 个去重模型）---'
SELECT count(DISTINCT model) AS distinct_models, count(*) FILTER (WHERE enabled) AS enabled_rows FROM abilities;
