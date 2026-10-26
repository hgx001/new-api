-- 生产：上架 gpt-6.1-sol（上游 sub2api 实测可用，2026-10-26）
--
-- 背景：上游 sub2api（渠道 13 的 base_url）已有 gpt-6.1-sol，直连实测 HTTP 200；
--       本站之前没上架（渠道 models 里没有 → 调它 503 model_not_found）。
--       ⚠️ 上游自己的 /v1/models 列表不可信（15 个条目里连在售的 gpt-6-sol 都没有），
--       只能靠真实调用确认。
--
-- 定价：与 gpt-6-sol 同档同价（ModelRatio 1.28424 → 输入 ¥18.75/1M；
--       补全倍率走 gpt-6* 硬编码兜底 8 → 输出 ¥150.00/1M）。
--       ⚠️ 必须显式登记输入倍率：GetModelRatio 精确查表，不会按 gpt-6 前缀回落，
--       不登记就是「模型无可用价格」。
--
-- ⚠️ 执行顺序：先发版（代码 defaultModelRatio 已有 gpt-6.1-sol）→ 再跑本脚本
--    → 最后用管理 API PUT /api/channel/ 给渠道 13 的 models 加上 gpt-6.1-sol
--    （管理 API 会重建 abilities 并刷新路由缓存；裸 SQL 改 channels 不会立刻生效）。
--
-- 幂等：可重复执行。

\set ON_ERROR_STOP on

BEGIN;

-- 0) 备份 ModelRatio
CREATE TABLE IF NOT EXISTS _bak_modelratio_20261026 (
    id         serial primary key,
    key        text        NOT NULL,
    value      text        NOT NULL,
    backup_at  timestamptz DEFAULT now()
);
INSERT INTO _bak_modelratio_20261026 (key, value)
SELECT key, value FROM options WHERE key = 'ModelRatio';

-- 1) ModelRatio：新增 gpt-6.1-sol = 1.28424（与 gpt-6-sol 同价）
UPDATE options
SET value = jsonb_set(value::jsonb, '{gpt-6.1-sol}', '1.28424'::jsonb)::text
WHERE key = 'ModelRatio'
  AND NOT (value::jsonb ? 'gpt-6.1-sol');

-- 2) 广场卡片：照 gpt-6-sol 的字段复制一张（厂商/图标/端点等），只换模型名与描述
INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, created_time, updated_time, name_rule, deleted_at)
SELECT 'gpt-6.1-sol',
       'GPT-6.1 旗舰文本模型，复杂推理档（与 gpt-6-sol 同价）',
       icon, tags, vendor_id, endpoints, 1, sync_official,
       extract(epoch from now())::bigint, extract(epoch from now())::bigint, name_rule, NULL
FROM models
WHERE model_name = 'gpt-6-sol'
  AND NOT EXISTS (SELECT 1 FROM models WHERE model_name = 'gpt-6.1-sol' AND deleted_at IS NULL);

COMMIT;

-- 3) 复核
SELECT value::jsonb ->> 'gpt-6.1-sol' AS gpt61sol_ratio,
       value::jsonb ->> 'gpt-6-sol'   AS gpt6sol_ratio
FROM options WHERE key = 'ModelRatio';
SELECT id, model_name, status, description FROM models
WHERE model_name IN ('gpt-6-sol', 'gpt-6.1-sol') ORDER BY id;
