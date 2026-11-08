-- 生产变更：2026-11-08 minimax_h3_omni 三种创作模式写法 + 多参考档素材约束，
--           同步模型广场描述（只改文案，不动价格 / abilities / 渠道配置）
-- 授权依据：用户 2026-11-08「提交部署吧」＋ 选项「代码+文档全量收口（推荐）」
--
-- 背景（同日真机验证 task_VZmt1axVYojijSxZWNCzHLn4CXZaeTOj）：
--   上游 H3 网关 derive_video_kind() 按 payload 派生 text / first_last /
--   multi_reference 三档。只带参考视频、没有设定参考图时会落到 multi_reference
--   且缺图，上游 400「多参考档需要至少一张参考图」并把内部工作流 ID 回给客户。
--   补一张 ref_image_0 后同一请求 200 出片（768p横，16 计费秒，¥0.64）。
--
-- 配套代码变更（必须先发版，再让客户按新描述对接）：
--   relay/channel/task/seeta/reference_inputs.go  新增 validateOmniReferenceInputs：
--     带参考视频/音频但无设定参考图 → 400 invalid_reference_media；
--     只有 metadata.last_frame 没有 first_frame → 400 invalid_first_frame。
--   relay/channel/task/seeta/adaptor.go          校验挂在 ValidateRequestAndSetAction，
--     位于预扣费与参考视频探测之前（不建单、不扣费、不 AutoBan、不白下载素材）。
--   model/upstream_redact.go                    全局遮蔽上游内部工作流 ID
--     minimax_h3_lightx2v*（omni 不是别名模型，放进 upstreamModelLiterals 不生效）。
--   web/default/src/features/tutorials/content-models.ts  三种模式与硬约束。
--
-- 执行顺序建议：**描述先行，再发后端**。本地校验会把原本「上游 400」变成
--   「网关 400」，错误文案与响应时间都变了；描述先上线可提前告知客户。
--   两者放在同一个变更窗口内完成，间隔越短越好。
--
-- 本脚本只改 omni 一张卡片，不碰 v2 卡片 / abilities / ModelPrice / 渠道配置。
-- 幂等：UPDATE 同一段完整文案，重复执行结果一致；备份表每次重灌执行前原值。

BEGIN;

-- ── 备份 ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS bak_20261108_omni_input_modes_card (LIKE models INCLUDING DEFAULTS);
TRUNCATE bak_20261108_omni_input_modes_card;
INSERT INTO bak_20261108_omni_input_modes_card
SELECT * FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL;

-- ── 广场卡片：补三种创作模式与多参考档硬约束 ─────────────────────────────
UPDATE models
SET description = 'FastH3-Omni 多模态参考视频模型：最多 9 张设定参考图 + 3 个参考视频 + 3 个参考音频（首尾帧可另用 metadata.first_frame / last_frame 锚定，与设定参考同请求共存）。出片快：768P 约 1-2 分钟出片，3 分钟内可交付。按秒计费 ¥0.02（480P）/ ¥0.04（768p，720P 档）/ ¥0.06（1080P），时长 1-15 秒可选（默认 5 秒），支持 9 档画面比例（480p/768p/1080p × 横屏/竖屏/1:1）。⚠️ 带参考视频时计费秒数 = 生成秒数 + 参考视频总秒数（每条按实际时长四舍五入到整秒后相加，与生成同价）；例：768p 生成 5 秒 + 11 秒参考视频 = ¥0.04 × 16 秒 = ¥0.64。参考图片与参考音频不额外计费。参考视频须为可解析时长的 MP4/MOV，解析失败在提交阶段 400 拒单（不建单不扣费）。prompt 中用 <Picture 1> / <Video 1> / <Audio 1> 依次引用对应素材。⚠️ 三种创作模式按素材自动分派：① 纯文生视频＝只传 prompt（零素材）；② 图生视频 / 首尾帧＝只传 metadata.first_frame（+可选 metadata.last_frame），不带参考视频/音频，images 是设定参考不是首帧；③ 多参考＝带设定参考图 / 参考视频 / 参考音频。带参考视频或音频时属多参考档，必须同时至少一张设定参考图，只给尾帧不给首帧同样在提交阶段 400（不建单不扣费）。',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'minimax_h3_omni'
  AND deleted_at IS NULL;

COMMIT;

\echo '=== 复核：卡片描述 ==='
SELECT id, model_name, status, length(description) AS desc_len, left(description, 120) AS head
FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL;

\echo '=== 复核：新口径与旧计费口径都要在 ==='
SELECT
  description LIKE '%三种创作模式按素材自动分派%'       AS has_modes,
  description LIKE '%images 是设定参考不是首帧%'       AS has_setref_note,
  description LIKE '%必须同时至少一张设定参考图%'       AS has_setref_required,
  description LIKE '%只给尾帧不给首帧%'                AS has_first_frame_rule,
  description LIKE '%计费秒数 = 生成秒数 + 参考视频总秒数%' AS has_billing_formula,
  description LIKE '%¥0.04 × 16 秒 = ¥0.64%'         AS has_billing_example
FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL;

\echo '=== 复核：本次不改价格，也不复活 v2 卡片 ==='
SELECT model_name, status, left(description, 60) AS head
FROM models
WHERE model_name IN ('minimax_h3_v2', 'minimax_h3_omni')
ORDER BY model_name;
