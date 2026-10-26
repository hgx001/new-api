-- 生产：seedance-2.0（dola）时长恢复为 5–15 秒区间（2026-10-26）
--
-- 背景：2026-10-24 曾把 dola 的时长收窄为「固定 10 秒」，2026-10-26 按用户要求
--       恢复为 **5–15 秒区间逐秒透传**（区间内任意整数秒原样透传、不吸附；
--       区间外含 30s 一律 400 invalid_duration；未指定时缺省 15）。
--       价格不变：仍按次计费 ¥1.00/次，与时长无关。
--
-- ⚠️ 执行顺序：先发版（后端 validateSeedance20Duration + 教程文案）→ 再跑本脚本。
--
-- 幂等：可重复执行。

\set ON_ERROR_STOP on

BEGIN;

-- 备份旧卡片文案
CREATE TABLE IF NOT EXISTS _bak_seedance20_card_20261026 AS
  SELECT id, model_name, description, now() AS backup_at
  FROM models WHERE model_name = 'seedance-2.0';

-- 广场卡片：时长口径改回区间
UPDATE models
SET description = 'Seedance 2.0 文/图生视频，5–15 秒逐秒可选（默认 15 秒），6 档比例，≤10 张参考图，卡人脸',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'seedance-2.0';

COMMIT;

-- 复核
SELECT id, model_name, status, description FROM models WHERE model_name = 'seedance-2.0';
SELECT value::jsonb ->> 'seedance-2.0' AS seedance20_usd_per_call,
       round((value::jsonb ->> 'seedance-2.0')::numeric * 7.3, 4) AS seedance20_cny_per_call
  FROM options WHERE key = 'ModelPrice';

-- ============================ 回滚 SQL ============================
--   UPDATE models SET description = (SELECT description FROM _bak_seedance20_card_20261026)
--     WHERE model_name = 'seedance-2.0';
-- ============================================================================
