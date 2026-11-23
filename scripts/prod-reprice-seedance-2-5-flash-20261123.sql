-- =============================================================================
-- seedance-2-5-flash 调价 + 放宽时长（2026-11-23）
--
-- 用户口径（2026-11-23）：「seedance-2-5-flash 这个模型 价格改为 4 元/条
-- 支持 4-30 秒 你的描述也要改下」。
--
-- 这次变更分两半，**必须同批发**：
--   代码（先发版）：
--     1. 时长上界 4–15 → 4–30（relay/channel/task/manwu/adaptor.go 的
--        doubao25FlashMin/MaxDuration）；
--     2. 计价口径 按秒 → **按条一口价**：EstimateBilling 对本档返回 nil，
--        并把本档登记进 constant.TaskPerCallModels（挡倍率合并与差额结算）。
--   数据（本脚本，两台各跑一次）：
--     3. options.ModelPrice：¥0.25/秒 → ¥4.0/条；
--     4. models.description：卡片文案同步（时长 4–30、按条 ¥4.0）。
--
-- ⚠️ 顺序不能颠倒：**先发版、再改价**。
--   只改库不发版 → 旧代码仍按秒返回 seconds 倍率，¥4.0 会变成 ¥4.0 × 秒数
--   （30 秒的片子收 ¥120），是静默超收，没有任何报错。
--   反过来只发版不改库 → 旧价 ¥0.25 被当成一口价，每单只收 ¥0.25。
--
-- ⚠️ 内部价一律 USD 等值（/7.3）：¥4.0 → 0.547945205479452。
--
-- ⚠️ 双机：老机 api.heibaidao.cn 与新机 api2.heibaidao.cn 各执行一次（幂等）。
-- ⚠️ 生效时延：MEMORY_CACHE_ENABLED=true 时 pricing 有 ~1min TTL，改完不要立刻判定失败。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份 ----------
CREATE TABLE IF NOT EXISTS bak_20261123_s25flash_reprice_modelprice AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';
CREATE TABLE IF NOT EXISTS bak_20261123_s25flash_reprice_models AS
  SELECT * FROM models WHERE model_name = 'seedance-2-5-flash';

-- ---------- 1. 价格：¥4.0/条（覆盖旧值，不只是「缺了才补」）----------
UPDATE options
   SET value = jsonb_set(value::jsonb, '{seedance-2-5-flash}',
                         to_jsonb(0.547945205479452::double precision))::text
 WHERE key = 'ModelPrice';

-- ---------- 2. 模型卡片描述 ----------
UPDATE models
   SET description = 'Seedance 2.5 Flash 文/图生视频。4–30 秒逐秒可选，固定 720p，按条计费 ¥4.0/条（与时长无关）；7 档比例（含 auto），≤10 张参考图',
       updated_time = (EXTRACT(EPOCH FROM now()))::bigint
 WHERE model_name = 'seedance-2-5-flash'
   AND deleted_at IS NULL
   AND description IS DISTINCT FROM 'Seedance 2.5 Flash 文/图生视频。4–30 秒逐秒可选，固定 720p，按条计费 ¥4.0/条（与时长无关）；7 档比例（含 auto），≤10 张参考图';

COMMIT;

-- ---------- 验收 ----------
-- 1) 价格（应为 4.0000）： SELECT round((value::jsonb->>'seedance-2-5-flash')::numeric * 7.3, 4) FROM options WHERE key='ModelPrice';
-- 2) 卡片描述：           SELECT model_name, description FROM models WHERE model_name='seedance-2-5-flash' AND deleted_at IS NULL;
-- 3) 价格生效（约 1min）：GET /api/pricing 里 seedance-2-5-flash 的 model_price 应为 0.547945205479452
-- 4) 时长上限（代码侧）：提交 duration=30 应受理，duration=31 应 400 invalid_duration
--
-- 回滚：
--   UPDATE options SET value = jsonb_set(value::jsonb, '{seedance-2-5-flash}', to_jsonb(0.03424657534246575::double precision))::text WHERE key='ModelPrice';
--   UPDATE models SET description = 'Seedance 2.5 Flash 文/图生视频。4–15 秒逐秒可选，固定 720p，¥0.25/秒；7 档比例（含 auto），≤10 张参考图' WHERE model_name='seedance-2-5-flash' AND deleted_at IS NULL;
--   （同时把代码回滚到旧版本 —— 数据与代码必须成对回滚）
