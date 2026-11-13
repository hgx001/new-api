-- 三元巡狩 wan3.0-1080p-慢速 生产配置（2026-10-14）
--
-- 为什么必须写 option：types.LoadFromJsonStringWithCallback 是整表替换，
-- 生产 option 里没有的键，代码 model_ratio.go 的默认值会被冲掉（价格变 0）。
-- 见 AGENTS.md「生产 ModelPrice option 会覆盖代码默认值」。
--
-- 定价依据：对外 ¥2.00/次 ÷ USD2RMB(7.3) = 0.273972602739726 USD/次。
-- 上游成本 ¥1/次（wanwan 1080P 拼车，charge_mode=once），毛利 50%。
-- 按次计费由 adaptor EstimateBilling 返回 nil 实现，绝不能进 TASK_PRICE_PATCH
--（进了会被 relay_task.go 当按次跳过倍率相乘——本模型本来就是按次，无倍率可乘，
-- 但为口径统一仍不进白名单）。
--
-- 前置：deploy-backend.sh 1a444e0e 已发版（先发版后跑 SQL，保证无「可见但不可调」窗口）。
-- 回滚：见文件末尾注释。

BEGIN;

CREATE TABLE IF NOT EXISTS _bak_modelprice_20261014_sanyuan AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';

UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan3.0-1080p-慢速}', to_jsonb(2.0 / 7.3))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'wan3.0-1080p-慢速');

-- 模型广场卡片：与 wan3 系现有卡片同风格（vendor/icon/tags 留空，description 承载全部信息）。
INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time)
SELECT 'wan3.0-1080p-慢速',
       'wan3.0 1080P 拼车慢速档，按次 ¥2.00（与时长、分辨率无关）。时长 1–30 秒逐秒任意，480p/720p/1080p 竖横可选，参考图 ≤9 张（首张即首帧）、参考音频 ≤3 条。需要一天时间生成，适合今天耕耘明天收获。',
       1, 1, EXTRACT(EPOCH FROM NOW())::bigint, EXTRACT(EPOCH FROM NOW())::bigint
 WHERE NOT EXISTS (
   SELECT 1 FROM models WHERE model_name = 'wan3.0-1080p-慢速' AND deleted_at IS NULL
 );

-- 已存在则刷新描述（幂等）
UPDATE models
   SET description = 'wan3.0 1080P 拼车慢速档，按次 ¥2.00（与时长、分辨率无关）。时长 1–30 秒逐秒任意，480p/720p/1080p 竖横可选，参考图 ≤9 张（首张即首帧）、参考音频 ≤3 条。需要一天时间生成，适合今天耕耘明天收获。',
       updated_time = EXTRACT(EPOCH FROM NOW())::bigint
 WHERE model_name = 'wan3.0-1080p-慢速' AND deleted_at IS NULL;

COMMIT;

SELECT 'ModelPrice' AS item,
       (value::jsonb ->> 'wan3.0-1080p-慢速') AS usd_per_call,
       round(((value::jsonb ->> 'wan3.0-1080p-慢速')::numeric * 7.3), 2) AS cny_per_call
  FROM options WHERE key = 'ModelPrice';

SELECT model_name, left(description, 40) AS desc_head, status
  FROM models WHERE model_name = 'wan3.0-1080p-慢速' AND deleted_at IS NULL;

-- 回滚：
-- BEGIN;
-- UPDATE options SET value = (SELECT value FROM _bak_modelprice_20261014_sanyuan) WHERE key='ModelPrice';
-- DELETE FROM models WHERE model_name = 'wan3.0-1080p-慢速';
-- COMMIT;
