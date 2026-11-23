-- =============================================================================
-- 上架 T3-seedance-2.5（MegaAI 重新接入，2026-11-23）
--
-- 授权：用户「对接 https://newapi.megabyai.cc/ 这个渠道的这个模型 T3-seedance-2.5，
--       不用测试」+「每档 +¥1」→ 对外 480p ¥4.00 / 720p ¥5.00（按次）。
--
-- 渠道本身用 scripts/create-megabyai-t3-channel.sh 通过管理 API 建（能力行走内存
-- 缓存，裸 SQL 建渠道不带 abilities），本脚本只负责**上架三件事**：
--   1. 模型卡片（描述 / 标签 / 供应商 / 状态）；
--   2. 价格键 ModelPrice（480p 基准 ¥4.00 的 USD 等值，720p 倍率在适配器里）；
--   3. 模型广场置顶权重（T3 置顶 + 复现原来两个客美 WAN3 的推荐位）。
--
-- 参数口径（服务端定死，客户不可选）：
--   - 分辨率 **480p / 720p**（上游没有 1080p 档；传 1080p 本地 400，不静默降档）
--   - 时长 **4–30 秒**（整数秒；越界 400）
--   - 比例：上游支持全比例，本站透传
--   - 参考图 ≤30 / 参考视频 ≤10 / 参考音频 ≤10（参考视频单个 ≤15 秒）
--
-- ⚠️ 先发版、再改库：后端不认识这个模型名时会算不出价（静默免单）。
-- ⚠️ 两台生产库各自独立，本脚本需在**两台各执行一次**（幂等，可重复跑）。
-- =============================================================================

\set ON_ERROR_STOP on

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS bak_20261123_t3ss25_models AS
  SELECT * FROM models WHERE model_name = 'T3-seedance-2.5';
CREATE TABLE IF NOT EXISTS bak_20261123_t3ss25_modelprice AS
  SELECT key, value FROM options WHERE key = 'ModelPrice';

-- ---------- 1. 模型卡片 ----------
UPDATE models
   SET description = 'Seedance 2.5 视频生成。时长 4–30 秒（整数秒），分辨率 480p / 720p，'
                     || '画面比例支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1 等常用比例，'
                     || '最多 30 张参考图 / 10 个参考视频 / 10 段参考音频'
                     || '（素材须公网可访问，参考视频单个 ≤15 秒），'
                     || '按次计费 480p ¥4.00 / 720p ¥5.00。',
       icon = NULL,
       tags = 'video',
       vendor_id = (SELECT id FROM vendors WHERE name = '字节跳动' LIMIT 1),
       endpoints = NULL,
       status = 1,
       sort_weight = 3,
       updated_time = (EXTRACT(EPOCH FROM now()))::bigint
 WHERE model_name = 'T3-seedance-2.5'
   AND deleted_at IS NULL;

INSERT INTO models (model_name, description, icon, tags, vendor_id, endpoints,
                    status, sync_official, name_rule, sort_weight,
                    created_time, updated_time, deleted_at)
SELECT 'T3-seedance-2.5',
       'Seedance 2.5 视频生成。时长 4–30 秒（整数秒），分辨率 480p / 720p，'
       || '画面比例支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1 等常用比例，'
       || '最多 30 张参考图 / 10 个参考视频 / 10 段参考音频'
       || '（素材须公网可访问，参考视频单个 ≤15 秒），'
       || '按次计费 480p ¥4.00 / 720p ¥5.00。',
       NULL, 'video', (SELECT id FROM vendors WHERE name = '字节跳动' LIMIT 1), NULL,
       1, 1, 0, 3,
       (EXTRACT(EPOCH FROM now()))::bigint,
       (EXTRACT(EPOCH FROM now()))::bigint,
       NULL
 WHERE NOT EXISTS (
   SELECT 1 FROM models WHERE model_name = 'T3-seedance-2.5' AND deleted_at IS NULL
 );

-- ---------- 2. 价格键（¥4.00 / 7.3 = 0.5479452054794521）----------
-- 720p 由 megabyai adaptor 的 t3ResolutionSizeRatio（5.0/4.0）乘上去，
-- 与这里成对维护：改一个必须改另一个。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{T3-seedance-2.5}',
                         to_jsonb(0.5479452054794521::double precision))::text
 WHERE key = 'ModelPrice'
   AND NOT (value::jsonb ? 'T3-seedance-2.5');

-- 已有键则按基准价覆盖（幂等：价格漂了也能靠重跑脚本拉回来）
UPDATE options
   SET value = jsonb_set(value::jsonb, '{T3-seedance-2.5}',
                         to_jsonb(0.5479452054794521::double precision))::text
 WHERE key = 'ModelPrice';

-- ---------- 3. 模型广场置顶权重 ----------
-- 机制：/api/pricing 下发 sort_weight，前端按「权重降序 → 名称」排；
-- 新建卡片由 Model.Insert() 自动取 MAX+1，所以以后上架的模型会自动在最前。
--
-- 本次把改造前**前端硬编码**的两个客美 WAN3 推荐位落进库里（原来顺序 tj 在前）：
--   wan3.0-video-tj       = 2
--   wan3.0-video-prime-tj = 1
--   T3-seedance-2.5       = 3（新上架 → 最前）
-- 其余模型保持 0（未置顶，按名称排）。
UPDATE models SET sort_weight = 2, updated_time = (EXTRACT(EPOCH FROM now()))::bigint
 WHERE model_name = 'wan3.0-video-tj' AND deleted_at IS NULL;

UPDATE models SET sort_weight = 1, updated_time = (EXTRACT(EPOCH FROM now()))::bigint
 WHERE model_name = 'wan3.0-video-prime-tj' AND deleted_at IS NULL;

COMMIT;

-- ---------- 验收 ----------
-- 1) 卡片：        SELECT model_name, status, sort_weight, tags FROM models WHERE model_name='T3-seedance-2.5' AND deleted_at IS NULL;
-- 2) 价格（×7.3）： SELECT round((value::jsonb->>'T3-seedance-2.5')::numeric * 7.3, 4) FROM options WHERE key='ModelPrice';
-- 3) 置顶顺序：    SELECT model_name, sort_weight FROM models WHERE deleted_at IS NULL AND sort_weight <> 0 ORDER BY sort_weight DESC;
-- 4) 路由：        SELECT model, channel_id, enabled FROM abilities WHERE model='T3-seedance-2.5';
