-- wan3.0-video-prime-1080p 卡片描述补全（2026-10-13）
--
-- 原文「高速高质量，只支持 30 秒。」缺三样客户必须知道的信息：
--   1. 价格（唯一一张不标价的视频卡，其他卡都写了价）
--   2. 固定分辨率（固定 1080P）
--   3. 素材能力（图/视频/音频参考的实际上限）
--
-- 上限取自 relay/channel/task/youzanwan3/constants.go：
--   primeMaxReferenceImages = 8   （smart 为 10，prime 单独收紧）
--   maxReferenceVideos     = 5
--   maxReferenceAudios     = 5
--   primeResolution        = "1080P"，primeDuration = 30
-- 已核 buildRequestBody：reference_video/reference_audio 各自映射到
-- uploadAsset 的 video/audio kind 并写入 mentions，prime 与 smart 走同一条
-- 路径，无 prime 专属降级，故「支持」是实际下发而非仅过校验。

BEGIN;

CREATE TABLE IF NOT EXISTS _bak_prime_desc_20261013 AS
  SELECT model_name, description FROM models WHERE model_name = 'wan3.0-video-prime-1080p';

UPDATE models
   SET description = '满血 30 秒档，固定 30 秒 1080P，¥8.00/次。支持图 ≤8 张、参考视频 ≤5 个、参考音频 ≤5 个、首尾帧各 1 张（首尾帧不能与参考素材混用）。'
 WHERE model_name = 'wan3.0-video-prime-1080p' AND deleted_at IS NULL;

COMMIT;

\echo '=== 结果 ==='
SELECT model_name, description FROM models
 WHERE model_name = 'wan3.0-video-prime-1080p';