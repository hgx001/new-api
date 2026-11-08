-- 生产变更：2026-11-08 minimax_h3_omni 参考视频按秒并入计费，同步模型广场描述
-- 授权依据：用户 2026-11-08「minimax_h3_omni 模型广场 这个模型 如果有参考视频的话
--           定价还要加上参考视频的秒数，可以参考 wan3 的模式，并且描述写清楚」
--
-- 配套代码变更（计费逻辑，必须先发版）：
--   relay/channel/task/seeta/refvideo.go   新增探测（taskcommon.ProbeReferenceVideoDuration）
--   relay/channel/task/seeta/adaptor.go    ValidateRequestAndSetAction 探测并写 context；
--                                         EstimateBilling 把参考视频秒数并入 seconds
--   结算 = ModelPrice × 群组倍率 × seconds × size
--   带参考视频时 seconds = 生成秒数 + 参考视频总秒数（逐条四舍五入后相加）
--   ⚠️ 基准价、分辨率倍率、abilities、ModelPrice 全部不动，本脚本只改卡片文案。
--
-- 执行顺序建议：**描述先行，再发后端**。
--   这次是「原本不收、现在要收」的新增计费。描述先上线 = 提前告知客户，
--   窗口内实际仍按旧口径（少收，客户不吃亏）；反过来先发版再改描述，
--   会出现「实际多扣但广场没写」的对客口径错误，不能选。
--   两者应放在同一个变更窗口内完成，间隔越短越好。
--
-- 本脚本只改 omni 一张卡片，不碰 v2 卡片 / abilities / ModelPrice / 渠道配置。
-- ⚠️ 2026-11-08 同日另有 V2 退役下架（scripts/prod-retire-v2-20261108.sql，
--    由另一个变更窗口负责），两者互不依赖、不要合并执行。
-- 幂等：UPDATE 同一段文案，重复执行结果一致；备份表每次重灌执行前原值。

BEGIN;

-- ── 备份 ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS bak_20261108_omni_refvideo_card (LIKE models INCLUDING DEFAULTS);
TRUNCATE bak_20261108_omni_refvideo_card;
INSERT INTO bak_20261108_omni_refvideo_card
SELECT * FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL;

-- ── 广场卡片：补参考视频计费规则与算例 ───────────────────────────────────
UPDATE models
SET description = 'FastH3-Omni 多模态参考视频模型：最多 9 张设定参考图 + 3 个参考视频 + 3 个参考音频（首尾帧可另用 metadata.first_frame / last_frame 锚定，与设定参考同请求共存）。出片快：768P 约 1-2 分钟出片，3 分钟内可交付。按秒计费 ¥0.02（480P）/ ¥0.04（768p，720P 档）/ ¥0.06（1080P），时长 1-15 秒可选（默认 5 秒），支持 9 档画面比例（480p/768p/1080p × 横屏/竖屏/1:1）。⚠️ 带参考视频时计费秒数 = 生成秒数 + 参考视频总秒数（每条按实际时长四舍五入到整秒后相加，与生成同价）；例：768p 生成 5 秒 + 11 秒参考视频 = ¥0.04 × 16 秒 = ¥0.64。参考图片与参考音频不额外计费。参考视频须为可解析时长的 MP4/MOV，解析失败在提交阶段 400 拒单（不建单不扣费）。prompt 中用 <Picture 1> / <Video 1> / <Audio 1> 依次引用对应素材。',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'minimax_h3_omni'
  AND deleted_at IS NULL;

COMMIT;

\echo '=== 复核：卡片描述 ==='
SELECT id, model_name, status, length(description) AS desc_len, left(description, 120) AS head
FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL;

\echo '=== 复核：描述必须含计费公式与算例 ==='
SELECT
  description LIKE '%计费秒数 = 生成秒数 + 参考视频总秒数%' AS has_formula,
  description LIKE '%¥0.04 × 16 秒 = ¥0.64%'               AS has_example,
  description LIKE '%不额外计费%'                          AS has_free_media_note,
  description LIKE '%MP4/MOV%'                            AS has_format_note
FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL;

\echo '=== 复核：v2 卡片与价格键不应被本次改动波及 ==='
SELECT model_name, left(description, 80) AS head
FROM models
WHERE model_name IN ('minimax_h3_v2', 'minimax_h3_omni') AND deleted_at IS NULL
ORDER BY model_name;
