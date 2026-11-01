-- =============================================================================
-- 上架 wan3.0-720p-按次（心笔 Model Router / xinbiapi 渠道）
-- 日期：2026-11-01
--
-- 前置：先发版到 ea7978c8d（含渠道类型 68 与 xinbiapi 适配器），再执行本脚本。
--       顺序不能反 —— 库里先有模型、代码还不认识这个渠道类型时，
--       请求会路由到空适配器，直接 500。
--
-- 执行方式（API Key 不进仓库，走 psql 变量）：
--   psql -U root -d new-api -v xkey='mrk_production_xxx' -f 本文件
--
-- 商品定义：
--   Wan3.0 官方模型，**原生 720P**，**固定 30 秒**，按次计费 ¥4.5/次。
--   与 wan3.0-720p-特惠 的区别只在计费口径（特惠按秒 ¥0.20/秒，本模型按次）。
--   ⚠️ 30 秒是这一档的成本上限（毛利约 5.6%），上游涨价这档立刻倒挂。
--   ⚠️ 对外文案一律「原生 720P」，不得出现交付链路的实现细节。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_modelprice_20261101 AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';

-- ---------- 1. 渠道 ----------
-- key 用 psql 变量传入，不落库到仓库文件。
-- 幂等：按 type=68 判重，重复执行不会建出第二个渠道。
INSERT INTO channels (
    type, key, status, name, created_time, base_url, models,
    "group", priority, weight, model_mapping, auto_ban, remark
)
SELECT
    68,
    :'xkey',
    1,
    '心笔 Model Router',
    EXTRACT(EPOCH FROM NOW())::bigint,
    'https://www.xinbiapi.com',
    'wan3.0-720p-按次',
    'default',
    0,
    0,
    '{"wan3.0-720p-按次": "wan3.0-video"}',
    1,
    'Wan3.0 原生 720P，固定 30 秒；按次 ¥4.5/次'
WHERE NOT EXISTS (SELECT 1 FROM channels WHERE type = 68);

-- ---------- 2. abilities（模型广场与路由都靠它）----------
-- ⚠️ 直改 channels.models 不会自动同步 abilities（只有后台界面保存渠道才重建），
--    漏了这步：广场看不到、路由也匹配不到，表现为「模型不存在」。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT 'default', 'wan3.0-720p-按次', c.id, true, c.priority, c.weight, c.tag
  FROM channels c
 WHERE c.type = 68
   AND NOT EXISTS (
       SELECT 1 FROM abilities a
        WHERE a.model = 'wan3.0-720p-按次' AND a.channel_id = c.id
   );

-- ---------- 3. 模型广场卡片 ----------
INSERT INTO models (
    model_name, description, icon, tags, vendor_id, endpoints,
    status, sync_official, created_time, updated_time, name_rule
)
SELECT
    'wan3.0-720p-按次',
    'Wan3.0 官方模型，原生 720P 输出，固定时长 30 秒。支持文生视频、图生视频与多张参考图（≤9 张），6 档画面比例（自适应 / 16:9 / 9:16 / 1:1 / 4:3 / 3:4）。特点：按次计费 ¥4.50/次，不随时长与分辨率变化；支持稳定高并发，适合批量出片。⚠️ 不支持负向提示词（negativePrompt）。',
    '',
    '',
    NULL,
    '',
    1,
    0,
    EXTRACT(EPOCH FROM NOW())::bigint,
    EXTRACT(EPOCH FROM NOW())::bigint,
    0
WHERE NOT EXISTS (
    SELECT 1 FROM models WHERE model_name = 'wan3.0-720p-按次' AND deleted_at IS NULL
);

-- ---------- 4. 价格（ModelPrice）----------
-- 4.5 / 7.3 = 0.6164383561643836（内部按 USD 等值存储，站内展示 ¥4.5/次）。
-- 与 setting/ratio_setting/model_ratio.go 的 defaultModelPrice 必须一致：
-- 代码默认值只在库里查不到时才兜底，两处不一致会出现「新装环境一个价、生产另一个价」。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan3.0-720p-按次}', to_jsonb(0.6164383561643836))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'wan3.0-720p-按次');

-- ---------- 校验 ----------
\echo '=== 渠道 ==='
SELECT id, type, name, status, base_url, models, model_mapping
  FROM channels WHERE type = 68;

\echo '=== abilities ==='
SELECT "group", model, channel_id, enabled, priority, weight
  FROM abilities WHERE model = 'wan3.0-720p-按次';

\echo '=== 卡片 ==='
SELECT model_name, status, left(description, 60) AS desc_head
  FROM models WHERE model_name = 'wan3.0-720p-按次' AND deleted_at IS NULL;

\echo '=== 价格（应显示 0.6164383561643836）==='
SELECT value::jsonb ->> 'wan3.0-720p-按次' AS model_price
  FROM options WHERE key = 'ModelPrice';

COMMIT;

-- =============================================================================
-- 回滚（按需手执行）：
--   DELETE FROM abilities WHERE model = 'wan3.0-720p-按次';
--   DELETE FROM models WHERE model_name = 'wan3.0-720p-按次';
--   DELETE FROM channels WHERE type = 68;
--   UPDATE options SET value = (SELECT value FROM _bak_modelprice_20261101 WHERE key='ModelPrice')
--    WHERE key = 'ModelPrice';
-- ⚠️ 回滚后同样要等 ~60 秒路由/配置缓存过期才生效，别急着判定失败。
-- =============================================================================
