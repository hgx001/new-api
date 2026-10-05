-- 模型广场卡片文案修正：卡片数据来自 models 表，与代码/option 三处独立，调价后最容易漏改。
-- 背景：2026-10-05 调价（seedance-2.0/2.5、wan3.0-smart 720p/1080p）后卡片仍写旧价；
-- 顺带发现 wan3.0-video-官网 的 1080P 一直少报（写 ¥0.85/s，实际 ¥1.08/s，少 21%）。
--
-- 实际价格依据（2026-10-05 生产实测反解，非估算）：
--   wan3.0-smart      channel 17 type=63 youzanwan3，ModelPrice 0.038356164383561646 USD=¥0.28/秒
--                      为 480P 基准，smartResolutionSizeRatio = 1.0 / 1.0 / 0.30÷0.28
--                      → 480P ¥0.28、720P ¥0.28、1080P ¥0.30
--   wan3.0-video-官网  channel 12 type=60 taskwan3，ModelPrice 0.0369863 USD=¥0.27/秒 为 480P 基准，
--                      resolutionSizeRatio = 1.0 / 2.0 / 4.0 → 480P ¥0.27、720P ¥0.54、1080P ¥1.08
--                      真实账目反解（12 笔，2026-09-21~10-05）确认基准 ¥0.27、size 取 1 与 2
--                      与代码倍率表一致；1080P 无成交样本，倍率取自代码常量。
--
-- wan3.0-video 未改：它没有任何 ability 行，调不通、也不在广场（models 表 status=1 但
-- modelGroupsMap 只收 enabled ability），属死数据；价格文案 10 倍错误但无对外影响，
-- 要不要清理另议。
BEGIN;

CREATE TABLE IF NOT EXISTS _bak_model_card_20261005 AS
  SELECT model_name, description FROM models
 WHERE model_name IN ('wan3.0-smart', 'wan3.0-video-官网');

UPDATE models
   SET description = '有赞 Wan3.0 智能调度视频生成。支持 480P（¥0.28/s）、720P（¥0.28/s）、1080P（¥0.30/s），时长 2-30 秒。',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan3.0-smart' AND deleted_at IS NULL;

UPDATE models
   SET description = '官方 Wan3.0 视频生成（官网渠道）。支持 480P（¥0.27/s）、720P（¥0.54/s）、1080P（¥1.08/s），时长 2-30 秒。',
       updated_time = EXTRACT(EPOCH FROM now())::bigint
 WHERE model_name = 'wan3.0-video-官网' AND deleted_at IS NULL;

COMMIT;

SELECT model_name, description FROM models
 WHERE model_name IN ('wan3.0-smart', 'wan3.0-video-官网') AND deleted_at IS NULL
 ORDER BY model_name;
