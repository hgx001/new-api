-- new-api 生产库：实验——删除 ModelRatio 中 8 个死模型的倍率键，验证 /v1/models 是否随之缩减
-- 2026-10-18 老板指令：「2个都做」之二。
--
-- 假设：生产 fork 的 ListModels 会把一份 Codex/GPT 模型池合并进 /v1/models，
--       且合并时经过 HasModelBillingConfig 过滤（依赖 ModelRatio/ModelPrice）。
--       若成立，删掉这 8 个倍率键后，列表应从 33 → 25。
--
-- 目标 8 键（当前均无 ability、调用必 503，倍率键纯属死配置）：
--   gpt-5=0.064212  gpt-5.1=0.064212  gpt-5.4=0.064212  gpt-5.4-mini=0.012842
--   gpt-5.5=0.064212 gpt-5-mini=0.012842 gpt-5-nano=0.002569 gpt-5-chat-latest=0.064212
--
-- 安全性：这些模型没有 abilities 行，不可能被路由计费；logs/tasks 是历史快照不受影响。
--        ModelRatio 是 Go 侧 json.Unmarshal 消费，jsonb 重排键序无数值影响。
--
-- 生效时延：options 每 60s sync 一次（DB → 内存），观察窗口建议 ≥70s。
--
-- 回滚（一行恢复，依赖备份表）：
--   UPDATE options SET value = (SELECT value FROM _bak_options_modelratio_20261018)
--    WHERE key = 'ModelRatio';

BEGIN;

-- ---------- 备份整行 ----------
CREATE TABLE IF NOT EXISTS _bak_options_modelratio_20261018 AS
  SELECT * FROM options WHERE key = 'ModelRatio';

-- ---------- 删除 8 个死模型倍率键 ----------
UPDATE options
   SET value = (value::jsonb
                 - 'gpt-5' - 'gpt-5.1' - 'gpt-5.4' - 'gpt-5.4-mini'
                 - 'gpt-5.5' - 'gpt-5-mini' - 'gpt-5-nano' - 'gpt-5-chat-latest'
               )::text
 WHERE key = 'ModelRatio';

COMMIT;

-- ---------- 复核 ----------
\echo '--- 剩余键数（268-8=260）---'
SELECT (SELECT count(*) FROM jsonb_object_keys(value::jsonb)) AS keys_left FROM options WHERE key='ModelRatio';
\echo '--- 8 键应全部消失 ---'
SELECT key, value ? 'gpt-5.5' AS has_gpt55, value ? 'gpt-5.6-terra' AS has_terra
  FROM options WHERE key = 'ModelRatio';
\echo '--- 抽查未误伤：gpt-5.6-luna / gpt-6-sol / wan3.0-smart 应仍在 ---'
SELECT value ? 'gpt-5.6-luna' AS luna, value ? 'gpt-6-sol' AS sol, value ? 'wan3.0-smart' AS smart
  FROM options WHERE key = 'ModelRatio';
