-- 2026-10-28 有赞 Wan3 对齐上游命名上架：wan3.0-video / wan3.0-video-prime
--
-- 背景：上游 2026-10-27 换到 V1 协议（/v1/tasks + Idempotency-Key + /v1/assets），
-- 且 /v1/models 只剩 wan3.0-video、wan3.0-video-prime 两个模型，wan2.7-r2v 已下架。
-- 价格口径（上游 ⚡/秒，1 ⚡ = ¥1，另加 ¥0.05/秒）：
--   wan3.0-video       上游 0.18  → 售 ¥0.23/秒（480P/720P/1080P 同价）
--   wan3.0-video-prime 上游 720P 0.15 / 1080P 0.20 → 售 ¥0.20 / ¥0.25 每秒
--
-- 前置条件：后端已发布包含新协议适配器的镜像（relay/channel/task/youzanwan3 走 /v1/tasks）。
-- 否则旧镜像会按「prime 固定 1080P/30 秒按次 ¥8」计价。
--
-- 生效：/v1/models 立即；广场 /api/pricing TTL 1min；路由（渠道缓存）≤60s。无需重启。

BEGIN;

-- 1) 渠道 17 路由。新模型上架；旧名保留路由兼容：
--    wan3.0-smart 代码内映射到 wan3.0-video，wan2.7-r2v 会返回明确的 400
--    （「上游已不再提供」），比「无可用渠道」更容易让客户看懂。
UPDATE channels
   SET models = 'wan3.0-video,wan3.0-video-prime,wan3.0-smart,wan2.7-r2v'
 WHERE id = 17;

-- 2) 广场可见性：只挂两个上游同名模型。
--    注意：渠道 17 的 models 里仍保留旧名（见上一步），而 UpdateAbilities 会按
--    channels.models 重建能力行 —— 渠道一旦被编辑，旧名的能力行会自动复活。
--    因此 model/pricing.go 的 isAllowedPricingModel 故意**不再放行**
--    wan3.0-smart / wan2.7-r2v，广场不会展示它们（可调用、不展示）。
DELETE FROM abilities
 WHERE channel_id = 17
   AND model IN ('wan3.0-smart', 'wan2.7-r2v');

INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight)
VALUES ('default', 'wan3.0-video', 17, true, 10, 0),
       ('default', 'wan3.0-video-prime', 17, true, 10, 0)
ON CONFLICT ("group", model, channel_id)
DO UPDATE SET enabled = true, priority = 10;

-- 3) 广场文案
UPDATE models
   SET description = 'Wan3 标准版视频生成。480P / 720P / 1080P 同价 ¥0.23/秒，时长 2-30 秒；参考素材支持图片最多 10 张、视频/音频各最多 5 个（合计不超过 20 项）。',
       status = 1,
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan3.0-video' AND deleted_at IS NULL;

INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time)
SELECT 'wan3.0-video',
       'Wan3 标准版视频生成。480P / 720P / 1080P 同价 ¥0.23/秒，时长 2-30 秒；参考素材支持图片最多 10 张、视频/音频各最多 5 个（合计不超过 20 项）。',
       1, 0,
       EXTRACT(EPOCH FROM now())::bigint,
       EXTRACT(EPOCH FROM now())::bigint
 WHERE NOT EXISTS (
   SELECT 1 FROM models WHERE model_name = 'wan3.0-video' AND deleted_at IS NULL
 );

UPDATE models
   SET description = 'Wan3 Prime 增强画质。按分辨率分档：720P ¥0.20/秒、1080P ¥0.25/秒，时长 2-30 秒；参考素材上限与标准版一致。',
       status = 1,
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan3.0-video-prime' AND deleted_at IS NULL;

INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time)
SELECT 'wan3.0-video-prime',
       'Wan3 Prime 增强画质。按分辨率分档：720P ¥0.20/秒、1080P ¥0.25/秒，时长 2-30 秒；参考素材上限与标准版一致。',
       1, 0,
       EXTRACT(EPOCH FROM now())::bigint,
       EXTRACT(EPOCH FROM now())::bigint
 WHERE NOT EXISTS (
   SELECT 1 FROM models WHERE model_name = 'wan3.0-video-prime' AND deleted_at IS NULL
 );

-- 4) 售价（ModelPrice 以 USD 计价，人民币 ÷ 7.3）
--    ⚠️ 直接改 options 表不会刷新进程内缓存：应优先用 PUT /api/option/ 改，
--       或改完重启容器。此处的语句用于核对/重建配置。
UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan3.0-video}', '0.031506849315068493'::jsonb)::text
 WHERE key = 'ModelPrice';

UPDATE options
   SET value = jsonb_set(value::jsonb, '{wan3.0-video-prime}', '0.027397260273972602'::jsonb)::text
 WHERE key = 'ModelPrice';

COMMIT;

-- 校验
--   SELECT value::jsonb -> 'wan3.0-video' AS video, value::jsonb -> 'wan3.0-video-prime' AS prime
--     FROM options WHERE key = 'ModelPrice';
--   SELECT id, models FROM channels WHERE id = 17;
--   SELECT "group", model, enabled FROM abilities WHERE channel_id = 17 ORDER BY model;
