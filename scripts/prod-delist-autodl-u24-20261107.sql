-- 生产：下架 autodl:minimax-h3-u24（2026-11-07 老板指令）
--
-- 执行前逐项核实（生产库只读查询）：
--   · 该名字只挂渠道 11「AutoDL-ComfyUI」(type 61)，且渠道 11 的 models
--     **只有这一个模型** → 摘掉后渠道为空，按 SOP 置 status=2（手动禁用）。
--   · abilities：1 行（default 组 / channel_id=11 / enabled=true）
--   · models 广场卡片：0 行（`/api/pricing` 里那条是 enabled ability 动态生成的，
--     删 ability 即消失，不是卡片表）
--   · 价格表：ModelPrice 有该键（0.0136986301369863）；ModelRatio 无；
--     CompletionRatio 防御性一并摘除
--   · 在途任务 0：tasks 表按 channel_id=11 只有 SUCCESS(91) / FAILURE(8) 两种终态
--   · tokens.model_limits、channels.model_mapping 均不含该名字
--   · 旧别名 autodl:multiaudio-video-1 在生产库（abilities/models/options）不存在，
--     仅存在于 constant/autodl.go 的迁移映射；没有 ability 就没有路由
--   · 渠道 11 的 other/settings 为空 → UpstreamModelUpdateCheckEnabled=false，
--     定时「上游模型检测」只扫 status=1 渠道，不会把该模型自动加回来
--
-- 为什么必须动 channels.models 而不只是删 abilities：
--   model/ability.go 的 FixAbility()（管理端「修复能力」按钮、main.go 启动兜底）
--   会按 channels.models 重建 abilities。只删 abilities，一次修复就复活。
--
-- 代码侧**不动**（沿用 2026-10-25 起同类下架口径）：
--   constant/autodl.go 别名映射、relay/channel/task/autodl/constants.go 的 ModelList、
--   model/pricing.go 的 `autodl:` 白名单 —— 没有 ability 就没有路由，白名单/倍率
--   不会被走到；真删干净要连 adaptor 重构，不属于「下架」范围。
--   ⚠️ 后续若有人对渠道 11 手动点「上游模型更新 → 应用」，静态 ModelList 里
--   仍含 autodl:minimax-h3-u24，会被加回来。渠道已禁用，正常路径不会触发；
--   若确需彻底移除，须同时改代码 ModelList 并发版。
--
-- 客户可见文档/教程另一次前端发版同步。
--
-- 全程单事务 + 备份表，幂等可重复执行。

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS _bak_delist_autodl_u24_20261107_channels AS
  SELECT * FROM channels WHERE id = 11;
CREATE TABLE IF NOT EXISTS _bak_delist_autodl_u24_20261107_abilities AS
  SELECT * FROM abilities WHERE model = 'autodl:minimax-h3-u24';
CREATE TABLE IF NOT EXISTS _bak_delist_autodl_u24_20261107_models AS
  SELECT * FROM models WHERE model_name = 'autodl:minimax-h3-u24';
CREATE TABLE IF NOT EXISTS _bak_delist_autodl_u24_20261107_options AS
  SELECT * FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');

-- ---------- ① 从渠道模型列表里摘除（不整体覆写 channels.models）----------
UPDATE channels
   SET models = array_to_string(
         array_remove(string_to_array(models, ','), 'autodl:minimax-h3-u24'), ',')
 WHERE id = 11
   AND models ~ '(^|,)autodl:minimax-h3-u24(,|$)';

-- ---------- ② 删能力行（否则这个别名仍可路由）----------
DELETE FROM abilities WHERE model = 'autodl:minimax-h3-u24';

-- ---------- ③ 删广场卡片（本次 0 行，幂等）----------
DELETE FROM models WHERE model_name = 'autodl:minimax-h3-u24';

-- ---------- ④ 摘价格键 ----------
UPDATE options
   SET value = (value::jsonb - 'autodl:minimax-h3-u24')::text
 WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio')
   AND value::jsonb ? 'autodl:minimax-h3-u24';

-- ---------- ⑤ 渠道 11 已被摘空 → 置「手动禁用」----------
-- 空渠道留 status=1 会在下次编辑保存时生成 model='' 的垃圾能力行；
-- 置为禁用后即便误存，生成的也是 enabled=false（沿用 2026-10-25 SOP）。
UPDATE channels
   SET status = 2
 WHERE id = 11
   AND status = 1
   AND trim(both ',' from models) = '';

COMMIT;

-- ---------- 复核（事务外只读）----------
\echo '--- ① 渠道 11：models 应为空、status 应为 2 ---'
SELECT id, name, status, models FROM channels WHERE id = 11;

\echo '--- ② 能力表：应为 0 行 ---'
SELECT count(*) AS ability_rows FROM abilities WHERE model = 'autodl:minimax-h3-u24';

\echo '--- ③ 广场卡片：应为 0 行 ---'
SELECT count(*) AS card_rows FROM models WHERE model_name = 'autodl:minimax-h3-u24';

\echo '--- ④ 价格键：应全为 NULL ---'
SELECT key, value::jsonb -> 'autodl:minimax-h3-u24' AS price_key
  FROM options WHERE key IN ('ModelPrice', 'ModelRatio', 'CompletionRatio');

\echo '--- ⑤ 备份表行数 ---'
SELECT 'channels' AS tbl, count(*) FROM _bak_delist_autodl_u24_20261107_channels
UNION ALL SELECT 'abilities', count(*) FROM _bak_delist_autodl_u24_20261107_abilities
UNION ALL SELECT 'models', count(*) FROM _bak_delist_autodl_u24_20261107_models
UNION ALL SELECT 'options', count(*) FROM _bak_delist_autodl_u24_20261107_options;
