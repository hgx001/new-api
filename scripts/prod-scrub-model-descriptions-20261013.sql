-- 清洗模型广场卡片描述：移除上游供应商 / 渠道 / 自动化来源线索
-- 2026-10-13
--
-- 背景：卡片描述是对外公开的，原文里写死了「漫屋」「豆包官网」「Dola」
-- 「有赞」「二春（Erchun）」「Gemini 官网」「即梦」等上游身份，等于把
-- 「我们用自动化驱动各家官网/供应商 API 转卖」这件事直接告诉客户，
-- 客户据此可以绕过我们直连上游��渠道价更低）。描述里只保留对客户
-- 有用的产品事实：能力、时长、比例、分辨率、参考图数量、价格。
--
-- 保留 Seedance / Wan3.0 等模型家族名：它们已经出现在公开模型 ID 里
-- （客户调用时必须知道），不是本次要隐藏的信息。
-- 隐藏的是「谁供的货 / 从哪调的」。

BEGIN;

-- ---------- 备份（回滚依赖）----------
CREATE TABLE IF NOT EXISTS _bak_desc_scrub_models_20261013 AS
  SELECT model_name, description, icon, vendor_id
    FROM models WHERE deleted_at IS NULL;

-- ---------- 漫屋 Seedance / Gemini / 反解 / 图片 ----------
UPDATE models SET description = 'Seedance 2.0 Fast 文/图生视频。5–15 秒逐秒可选，¥0.10/秒；7 档比例（含 auto），≤10 张参考图'
 WHERE model_name = 'seedance-2-0-fast' AND deleted_at IS NULL;

UPDATE models SET description = 'Seedance 2.5 文/图生视频。5/10/15/30 秒四档，¥3.00/次；7 档比例（含 auto），≤10 张参考图'
 WHERE model_name = 'seedance-2-5' AND deleted_at IS NULL;

-- 2026-10-20 补记：seedance-2.0 卡片后来又追加了「卡人脸」备注
-- （见 scripts/prod-note-seedance20-face-20261020.sql）。此处同步为当前线上文案，
-- 重跑本脚本不会把该备注冲掉。
UPDATE models SET description = 'Seedance 2.0 文/图生视频，5/10/15 秒，6 档比例，≤10 张参考图，卡人脸'
 WHERE model_name = 'seedance-2.0' AND deleted_at IS NULL;

UPDATE models SET description = 'Seedance 2.5 文/图生视频，固定 30 秒 720P，6 档比例，≤10 张参考图'
 WHERE model_name = 'seedance-2.5' AND deleted_at IS NULL;

UPDATE models SET description = '文/图生视频，固定 10 秒时长'
 WHERE model_name = 'gemini-web-video' AND deleted_at IS NULL;

UPDATE models SET description = '图片生成与编辑'
 WHERE model_name = 'Nano Banana Pro' AND deleted_at IS NULL;

UPDATE models SET description = '视频反解（输出提示词）'
 WHERE model_name = 'jimeng-video-reverse' AND deleted_at IS NULL;

-- ---------- Wan 系列（有赞 / 二春 / 官网渠道）----------
UPDATE models SET description = 'Wan2.7 参考生视频。需 1-3 张参考图，时长支持 5/10 秒，720P/1080P 统一 ¥0.10/s。'
 WHERE model_name = 'wan2.7-r2v' AND deleted_at IS NULL;

UPDATE models SET description = 'Wan 3.0 视频生成，仅开放 480P，¥0.28/秒，支持图片/视频/音频参考，时长 2-30 秒。'
 WHERE model_name = 'wan3.0-480p' AND deleted_at IS NULL;

UPDATE models SET description = 'Wan3.0 智能调度视频生成。支持 480P（¥0.28/s）、720P（¥0.28/s）、1080P（¥0.30/s），时长 2-30 秒。'
 WHERE model_name = 'wan3.0-smart' AND deleted_at IS NULL;

UPDATE models SET description = 'Wan3.0 视频生成。支持 480P（¥0.27/s）、720P（¥0.54/s）、1080P（¥1.08/s），时长 2-30 秒。'
 WHERE model_name = 'wan3.0-video-官网' AND deleted_at IS NULL;

UPDATE models SET description = '图片生成/编辑，8 种比例，2K/3K'
 WHERE model_name = 'doubao-seedream-5-0-260128' AND deleted_at IS NULL;

UPDATE models SET description = '图片生成，2K 按张计费'
 WHERE model_name = 'gpt-image-2.5-官方' AND deleted_at IS NULL;

COMMIT;

\echo '=== 清洗后：是否还有供应商线索残留 ==='
SELECT count(*) AS leaks
  FROM models
 WHERE deleted_at IS NULL
   AND (description ~ '漫屋|豆包|Dola|有赞|二春|Erchun|官网|即梦|Gemini|Veo');

\echo '=== 清洗后的 seedance / wan 卡片文案 ==='
SELECT model_name, description FROM models
 WHERE deleted_at IS NULL
   AND (model_name LIKE 'seedance%' OR model_name LIKE 'wan%'
     OR model_name IN ('gemini-web-video','Nano Banana Pro','jimeng-video-reverse'))
 ORDER BY model_name;

-- 回滚：
--   UPDATE models m SET description = b.description, icon = b.icon, vendor_id = b.vendor_id
--     FROM _bak_desc_scrub_models_20261013 b
--    WHERE b.model_name = m.model_name;