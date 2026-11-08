-- 生产变更：2026-11-08 给 minimax_h3_v2 / minimax_h3_omni 补模型广场描述与定价
-- 授权依据：用户 2026-11-08「minimax_h3_omni 这个的描述加上 9图 3音频 3视频，定价也要标出来；
--           minimax_h3_v2 这个是首尾帧模型，价格同样标出来的模型广场的描述」
--
-- 背景：/api/pricing 的描述取自 models 表卡片（model/pricing.go 里 pricing.Description = meta.Description），
-- 两个新模型 2026-11-07 上架时只写了 abilities + ModelPrice，没有卡片行 → 广场描述为空。
-- 旧名卡片 67/68（minimax_h3-文生 / -图生）保留不动：它们已无 ability，不会出现在广场，
-- 但旧名仍可路由，留着卡片不影响对客。
--
-- 幂等：先 UPDATE 命中则改文案，未命中再 INSERT；重复执行结果一致。
-- 价格口径与 relay/channel/task/seeta/constants.go 的 seetaResolutionRatios 一致：
--   480P ¥0.03/秒（基准 ratio 1.0）、768p（720P 档）¥0.05/秒（1.667）、1080p ¥0.06/秒（2.0，暂定）。

BEGIN;

-- ── 备份（幂等：已存在则清空重灌，保留上一次执行前的原值）──────────────────
CREATE TABLE IF NOT EXISTS bak_20261108_h3_cards (LIKE models INCLUDING DEFAULTS);
TRUNCATE bak_20261108_h3_cards;
INSERT INTO bak_20261108_h3_cards
SELECT * FROM models WHERE model_name IN ('minimax_h3_v2', 'minimax_h3_omni');

-- ── v2：文生 / 首帧图生 / 首尾帧三合一，关键帧 ≤2，六档 480p/768p ──────────
UPDATE models
SET description = 'FastH3 V2 首尾帧视频模型（三合一）：不带参考图=文生视频，1 张=首帧图生，2 张=首尾帧驱动。按秒计费 ¥0.03（480P）/ ¥0.05（768p，720P 档），时长 1-15 秒可选（默认 5 秒），支持 6 档画面比例（480p/768p × 横屏/竖屏/1:1）。⚠️ 参考图即关键帧，最多 2 张（第 1 张=首帧、第 2 张=尾帧），不接受参考视频/音频；最高只到 768p，不支持 1080P。',
    status = 1,
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'minimax_h3_v2'
  AND deleted_at IS NULL;

INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time, name_rule)
SELECT 'minimax_h3_v2',
       'FastH3 V2 首尾帧视频模型（三合一）：不带参考图=文生视频，1 张=首帧图生，2 张=首尾帧驱动。按秒计费 ¥0.03（480P）/ ¥0.05（768p，720P 档），时长 1-15 秒可选（默认 5 秒），支持 6 档画面比例（480p/768p × 横屏/竖屏/1:1）。⚠️ 参考图即关键帧，最多 2 张（第 1 张=首帧、第 2 张=尾帧），不接受参考视频/音频；最高只到 768p，不支持 1080P。',
       1, 0, extract(epoch from now())::bigint, extract(epoch from now())::bigint, 0
WHERE NOT EXISTS (
  SELECT 1 FROM models WHERE model_name = 'minimax_h3_v2' AND deleted_at IS NULL
);

-- ── omni：9 图 + 3 视频 + 3 音频设定参考，九档含 1080p ────────────────────
UPDATE models
SET description = 'FastH3-Omni 多模态参考视频模型：最多 9 张设定参考图 + 3 个参考视频 + 3 个参考音频（首尾帧可另用 metadata.first_frame / last_frame 锚定，与设定参考同请求共存）。按秒计费 ¥0.03（480P）/ ¥0.05（768p，720P 档）/ ¥0.06（1080P），时长 1-15 秒可选（默认 5 秒），支持 9 档画面比例（480p/768p/1080p × 横屏/竖屏/1:1）。prompt 中用 <Picture 1> / <Video 1> / <Audio 1> 依次引用对应素材。',
    status = 1,
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'minimax_h3_omni'
  AND deleted_at IS NULL;

INSERT INTO models (model_name, description, status, sync_official, created_time, updated_time, name_rule)
SELECT 'minimax_h3_omni',
       'FastH3-Omni 多模态参考视频模型：最多 9 张设定参考图 + 3 个参考视频 + 3 个参考音频（首尾帧可另用 metadata.first_frame / last_frame 锚定，与设定参考同请求共存）。按秒计费 ¥0.03（480P）/ ¥0.05（768p，720P 档）/ ¥0.06（1080P），时长 1-15 秒可选（默认 5 秒），支持 9 档画面比例（480p/768p/1080p × 横屏/竖屏/1:1）。prompt 中用 <Picture 1> / <Video 1> / <Audio 1> 依次引用对应素材。',
       1, 0, extract(epoch from now())::bigint, extract(epoch from now())::bigint, 0
WHERE NOT EXISTS (
  SELECT 1 FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL
);

COMMIT;

\echo '=== 复核：两模型卡片 ==='
SELECT id, model_name, status, length(description) AS desc_len, left(description, 70) AS head
FROM models
WHERE model_name IN ('minimax_h3_v2', 'minimax_h3_omni') AND deleted_at IS NULL
ORDER BY model_name;

\echo '=== 复核：每个名字必须恰好 1 行 ==='
SELECT model_name, count(*) AS rows
FROM models
WHERE model_name IN ('minimax_h3_v2', 'minimax_h3_omni') AND deleted_at IS NULL
GROUP BY model_name;
