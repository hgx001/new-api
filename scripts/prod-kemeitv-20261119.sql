-- ---------------------------------------------------------------------------
-- 客美 Kemeitv 上架（渠道 71，2026-11-19）
--
-- 契约来源：https://ai.kemeitv.cn/docs/quickstart/ （2026-11-19 版）
--   · 提交 POST {base}/v1/video/generations（OpenAI 兼容）
--   · 轮询 GET  {base}/v1/video/generations/{id}
--   · 鉴权   Authorization: Bearer sk-...
--   · 素材   images/videos/audios/files/links，元素为 URL 串或 {url, role}
--   · 分辨率 480P / 720P / 1080P；画幅 16:9 / 9:16 / 4:3 / 3:4 / 1:1（无 adaptive）
--   · 时长   2-30 秒，缺省 5 秒
--
-- ⚠️ **只接两个模型**（用户 2026-11-19 明确口径）：
--     wan3.0-video-tj / wan3.0-video-prime-tj
--     tj = 上游「特价」供应商档，与阿里云直连的 wan3.0-video /
--     wan3.0-video-prime 是两条独立货源，只是上游 rate 档位结构相同。
--
-- 定价：与对应标准档对齐（480P 基准，720P/1080P 走 2 / 4 倍率）
--     wan3.0-video-tj       ¥0.27/秒  （0.27/7.3 = 0.0369863）
--     wan3.0-video-prime-tj ¥0.405/秒 （0.405/7.3 = 0.05547945）
--
-- 幂等：可重复执行。每一步都带存在性判断。
-- 备份：执行前落 bak_20261119_kemeitv_* 快照表。
--
-- ⚠️ 执行前必须先确认：
--     ① 新建渠道（type=71，base_url=https://ai.kemeitv.cn，key=客美 API Key）
--        —— 本脚本**不含**建渠道语句，因为 key 不能进仓库；
--     ② 渠道 id 不是固定的，下面的 :channel_id 占位符要替换成实际 id。
-- ---------------------------------------------------------------------------

-- >>> 把下面这行替换成实际渠道 id，例如 72 <<<
-- \set channel_id 72

BEGIN;

-- 1) 备份 -------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS bak_20261119_kemeitv_channels AS
SELECT * FROM channels WHERE type = 71;

CREATE TABLE IF NOT EXISTS bak_20261119_kemeitv_abilities AS
SELECT * FROM abilities WHERE channel_id IN (SELECT id FROM channels WHERE type = 71);

CREATE TABLE IF NOT EXISTS bak_20261119_kemeitv_modelprice AS
SELECT * FROM options WHERE key = 'ModelPrice';

CREATE TABLE IF NOT EXISTS bak_20261119_kemeitv_models AS
SELECT * FROM models WHERE model_name IN ('wan3.0-video-tj', 'wan3.0-video-prime-tj');

-- 2) 渠道 models 追加（不动其它模型） --------------------------------------
--    把 :channel_id 替换成实际渠道 id 后执行。
UPDATE channels
SET models = CASE
        WHEN models IS NULL OR btrim(models) = '' THEN 'wan3.0-video-tj,wan3.0-video-prime-tj'
        ELSE models || ',wan3.0-video-tj,wan3.0-video-prime-tj'
    END
WHERE type = 71
  AND NOT (',' || replace(models, ' ', '') || ',') LIKE '%,wan3.0-video-tj,%';

UPDATE channels
SET models = CASE
        WHEN models IS NULL OR btrim(models) = '' THEN 'wan3.0-video-prime-tj'
        ELSE models || ',wan3.0-video-prime-tj'
    END
WHERE type = 71
  AND NOT (',' || replace(models, ' ', '') || ',') LIKE '%,wan3.0-video-prime-tj,%';

-- 3) abilities：沿用该渠道现有分组，逐组补两行 -----------------------------
--    ⚠️ 这里按「渠道已有的 abilities 分组」补；若该渠道还没有任何 abilities 行，
--    先手工插一条 default 分组的种子行再跑本段。
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT DISTINCT a."group", m.model, c.id, true, 0, 0, ''
FROM abilities a
JOIN channels c ON c.id = a.channel_id
CROSS JOIN (VALUES ('wan3.0-video-tj'), ('wan3.0-video-prime-tj')) AS m(model)
WHERE c.type = 71
  AND NOT EXISTS (
      SELECT 1 FROM abilities b
      WHERE b.channel_id = c.id AND b.model = m.model AND b."group" = a."group"
  );

-- 4) ModelPrice：480P 基准价（内部按 USD 等值存储） ------------------------
UPDATE options
SET value = (value::jsonb ||
             jsonb_build_object(
                 'wan3.0-video-tj',       0.0369863,
                 'wan3.0-video-prime-tj', 0.05547945
             ))::text
WHERE key = 'ModelPrice';

-- 5) 模型卡片（广场描述） --------------------------------------------------
INSERT INTO models (model_name, description, tags, endpoints, icon,
                    name_rule, sync_official, status, created_time, updated_time)
SELECT 'wan3.0-video-tj',
       'Wan3.0 特价档视频生成（客美独立上游模型，非 Wan3 标准版别名）。' ||
       '按秒计费：480P ¥0.27/秒、720P ¥0.54/秒、1080P ¥1.08/秒。' ||
       '时长 2-30 秒整数秒可选（不传默认 5 秒）。' ||
       '画幅支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1。' ||
       '支持文生视频、首尾帧生视频与多图参考生视频；' ||
       '参考素材支持图片（最多 10）、视频（最多 5）、音频（最多 5），' ||
       '均须公网 http(s) URL。首尾帧与参考类素材不可混用。' ||
       '生成时长按秒计费，适合批量出片。',
       '', '', '', 0, 0, 1,
       extract(epoch FROM now())::bigint, extract(epoch FROM now())::bigint
WHERE NOT EXISTS (
    SELECT 1 FROM models WHERE model_name = 'wan3.0-video-tj' AND deleted_at IS NULL
);

INSERT INTO models (model_name, description, tags, endpoints, icon,
                    name_rule, sync_official, status, created_time, updated_time)
SELECT 'wan3.0-video-prime-tj',
       'Wan3.0 特价满血档视频生成（客美独立上游模型，非 Wan3 prime 版别名）。' ||
       '按秒计费：480P ¥0.405/秒、720P ¥0.81/秒、1080P ¥1.62/秒。' ||
       '时长 2-30 秒整数秒可选（不传默认 5 秒）。' ||
       '画幅支持 16:9 / 9:16 / 4:3 / 3:4 / 1:1。' ||
       '支持文生视频、首尾帧生视频与多图参考生视频；' ||
       '参考素材支持图片（最多 10）、视频（最多 5）、音频（最多 5），' ||
       '均须公网 http(s) URL。首尾帧与参考类素材不可混用。' ||
       '满血算力档，画质与稳定性优于标准档。',
       '', '', '', 0, 0, 1,
       extract(epoch FROM now())::bigint, extract(epoch FROM now())::bigint
WHERE NOT EXISTS (
    SELECT 1 FROM models WHERE model_name = 'wan3.0-video-prime-tj' AND deleted_at IS NULL
);

COMMIT;

-- 6) 验收 -------------------------------------------------------------------
SELECT id, type, name, base_url, models FROM channels WHERE type = 71;

SELECT a."group", a.model, a.channel_id, a.enabled
FROM abilities a JOIN channels c ON c.id = a.channel_id
WHERE c.type = 71 ORDER BY a.model;

SELECT value::jsonb -> 'wan3.0-video-tj'       AS tj_price,
       value::jsonb -> 'wan3.0-video-prime-tj' AS prime_tj_price
FROM options WHERE key = 'ModelPrice';

SELECT model_name, status, left(description, 40) AS desc_head
FROM models WHERE model_name IN ('wan3.0-video-tj', 'wan3.0-video-prime-tj');
