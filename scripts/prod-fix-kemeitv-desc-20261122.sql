-- ---------------------------------------------------------------------------
-- 客美 tj 两个模型的卡片描述纠价（2026-11-22）
--
-- 背景：2026-11-20 客户调价把 480P 基准价抬了，但只改了 options.ModelPrice，
--       models.description 里印给客户看的单价还是 11-19 上架时的旧值：
--         wan3.0-video-tj        描述 480P ¥0.138 → 实际 ¥0.15
--         wan3.0-video-prime-tj  描述 480P ¥0.24  → 实际 ¥0.25
--       720P / 1080P 由 kemeitv/constants.go 的 modelResolutionPriceCNY
--       锁死绝对价（tj 0.25 / 0.552，prime-tj 0.48 / 0.96），描述本来就对。
--
-- 只改这两条 description，不动 ModelPrice / 渠道 / abilities。
-- 两机（api.heibaidao.cn、api2.heibaidao.cn）的 ModelPrice 已核对为
-- tj 0.02054794520547945 USD（×7.3 = ¥0.15）、prime-tj 0.03424657534246575
-- （×7.3 = ¥0.25），两台一致，故同一份文案对两机都成立。
--
-- 幂等：可重复执行；执行前落 bak_20261122_kemeitv_desc 快照。
-- ---------------------------------------------------------------------------

BEGIN;

CREATE TABLE IF NOT EXISTS bak_20261122_kemeitv_desc AS
SELECT model_name, description, updated_time
FROM models
WHERE model_name IN ('wan3.0-video-tj', 'wan3.0-video-prime-tj');

UPDATE models
SET description = 'Wan3.0 特价档视频生成。按秒计费：480P ¥0.15/秒、720P ¥0.25/秒、1080P ¥0.552/秒。' ||
                  '时长 2-30 秒整数秒可选（不传默认 5 秒）。' ||
                  '画幅支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1。' ||
                  '支持文生视频、首尾帧生视频与多图参考生视频；' ||
                  '参考素材支持图片（最多 10，jpg/jpeg/png/bmp/webp）、视频（最多 5，mp4/mov）、音频（最多 5，wav/mp3），' ||
                  '均须公网 http(s) URL。首尾帧与参考类素材不可混用。',
    updated_time = extract(epoch FROM now())::bigint
WHERE model_name = 'wan3.0-video-tj';

UPDATE models
SET description = 'Wan3.0 特价满血档视频生成。按秒计费：480P ¥0.25/秒、720P ¥0.48/秒、1080P ¥0.96/秒。' ||
                  '时长 2-30 秒整数秒可选（不传默认 5 秒）。' ||
                  '画幅支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1。' ||
                  '支持文生视频、首尾帧生视频与多图参考生视频；' ||
                  '参考素材支持图片（最多 10，jpg/jpeg/png/bmp/webp）、视频（最多 5，mp4/mov）、音频（最多 5，wav/mp3），' ||
                  '均须公网 http(s) URL。首尾帧与参考类素材不可混用。' ||
                  '满血算力档，画质与稳定性优于标准档。',
    updated_time = extract(epoch FROM now())::bigint
WHERE model_name = 'wan3.0-video-prime-tj';

COMMIT;

-- 验收
SELECT model_name, left(description, 48) AS desc_head
FROM models
WHERE model_name IN ('wan3.0-video-tj', 'wan3.0-video-prime-tj')
ORDER BY model_name;
