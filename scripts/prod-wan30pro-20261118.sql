-- ---------------------------------------------------------------------------
-- wan-3.0-pro 上架（有赞 ch17，2026-11-18）
--
-- 契约来源：https://api.youzan666.vip/docs/ （2026-11-17 版）
--   · 时长 4-30 整数秒
--   · 分辨率 720P / 1080P；size 仅 1280x720 / 720x1280 / 1920x1080 / 1080x1920
--   · 画幅仅 16:9 / 9:16
--   · 素材 images / videos / audios 数组
-- 定价（老板 2026-11-18 首次上架）：720P ¥4.5/条、1080P ¥5.5/条，与时长无关。
--
-- ⚠️ 价格已于同日调整（720P ¥5.0 / 1080P ¥7.0），见
--    scripts/prod-wan30pro-price-20261118.sql。本脚本的 ModelPrice 与卡片描述
--    仍是**上架时**的旧价，**不要**为了补路由/补卡片再重跑它 —— 会把价格写回
--    ¥4.5。确需重跑时，请紧接着执行调价脚本。
--
-- 幂等：可重复执行。每一步都带存在性判断。
-- 备份：执行前落 4 张 bak_20261118_wan30pro_* 快照表。
-- ---------------------------------------------------------------------------

BEGIN;

-- 1) 备份 -------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS bak_20261118_wan30pro_channels AS
SELECT * FROM channels WHERE id = 17;

CREATE TABLE IF NOT EXISTS bak_20261118_wan30pro_abilities AS
SELECT * FROM abilities WHERE channel_id = 17;

CREATE TABLE IF NOT EXISTS bak_20261118_wan30pro_modelprice AS
SELECT * FROM options WHERE key = 'ModelPrice';

CREATE TABLE IF NOT EXISTS bak_20261118_wan30pro_models AS
SELECT * FROM models WHERE model_name = 'wan-3.0-pro';

-- 2) 渠道 models 追加（不动其它模型） --------------------------------------
UPDATE channels
SET models = CASE
        WHEN models IS NULL OR btrim(models) = '' THEN 'wan-3.0-pro'
        ELSE models || ',wan-3.0-pro'
    END
WHERE id = 17
  AND NOT (',' || replace(models, ' ', '') || ',') LIKE '%,wan-3.0-pro,%';

-- 3) abilities：沿用该渠道现有分组（default），逐组补行 ---------------------
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT DISTINCT a."group", 'wan-3.0-pro', 17, true, 0, 0, ''
FROM abilities a
WHERE a.channel_id = 17
  AND NOT EXISTS (
      SELECT 1 FROM abilities b
      WHERE b.channel_id = 17 AND b.model = 'wan-3.0-pro' AND b."group" = a."group"
  );

-- 4) ModelPrice：720P 基准价 4.5/7.3 = 0.6164383561643836 --------------------
--    1080P 的 5.5/4.5 倍率在 adaptor（wan30ProResolutionSizeRatio）里，
--    这里只放按条基准价 —— 不要把它写成「每千 token」或按秒口径。
UPDATE options
SET value = (value::jsonb || jsonb_build_object('wan-3.0-pro', 0.6164383561643836))::text
WHERE key = 'ModelPrice';

-- 5) 模型卡片（广场描述，口径与上游文档逐条对齐） ---------------------------
INSERT INTO models (model_name, description, tags, endpoints, icon,
                    name_rule, sync_official, status, created_time, updated_time)
SELECT 'wan-3.0-pro',
       'Wan3.0 Pro 高清视频生成（独立上游模型，非 Wan3 标准版别名）。' ||
       '按条计费：720P ¥4.50/条、1080P ¥5.50/条，价格与生成时长无关。' ||
       '时长 4-30 秒整数秒可选（不传默认 5 秒）。' ||
       '画幅仅支持 16:9 / 9:16，输出尺寸 1280x720 / 720x1280 / 1920x1080 / 1080x1920。' ||
       '参考素材支持图片（最多 10）、视频（最多 5）、音频（最多 5），合计最多 20 项，' ||
       '均须公网 http(s) URL；平台校验参考图短边 ≥720px、参考视频合计 ≤15 秒、' ||
       '参考音频合计 ≤15 秒、有参考视频时生成时长 ≤15 秒。' ||
       '计费可预期，适合批量出片。',
       '', '', '', 0, 0, 1,
       extract(epoch FROM now())::bigint, extract(epoch FROM now())::bigint
WHERE NOT EXISTS (
    SELECT 1 FROM models WHERE model_name = 'wan-3.0-pro' AND deleted_at IS NULL
);

COMMIT;

-- 6) 验收 -------------------------------------------------------------------
SELECT id, models FROM channels WHERE id = 17;
SELECT "group", model, channel_id, enabled FROM abilities WHERE channel_id = 17 ORDER BY model;
SELECT value::jsonb -> 'wan-3.0-pro' AS wan30pro_price FROM options WHERE key = 'ModelPrice';
SELECT id, model_name, left(description, 60) AS desc_head, status FROM models WHERE model_name = 'wan-3.0-pro';
