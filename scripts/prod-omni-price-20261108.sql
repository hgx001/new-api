-- 生产变更：2026-11-08 minimax_h3_omni 调价（480P ¥0.03→¥0.02、768P ¥0.05→¥0.04、1080P 不变 ¥0.06）
-- 授权依据：用户 2026-11-08「minimax_h3_omni 这个模型改为480p 价格0.02 768价格改为 0.04
--           1080p 不变，然后加上速度很快 768p只需3分钟出片的描述」
--
-- 配套代码变更（必须先发版再跑本脚本，顺序见下）：
--   relay/channel/task/seeta/constants.go  omniResolutionRatios 1.0 / 2.0 / 3.0
--   setting/ratio_setting/model_ratio.go   seetaOmniBasePrice = 0.02 / USD2RMB
--
-- 为什么基准价与倍率要成对改：
--   结算 = ModelPrice × 群组倍率 × 秒数 × 分辨率倍率（relay_task.go applyTaskOtherRatios）。
--   只改 ModelPrice（0.03→0.02）不改倍率：768p 算 ¥0.0333、1080p 算 ¥0.04（少收）；
--   只改倍率（1.667/2.0 → 2.0/3.0）不改 ModelPrice：768p 算 ¥0.06、1080p 算 ¥0.09（多收）。
--
-- 执行顺序（本脚本**先于**后端发版执行）：
--   ModelPrice 先降到 0.02，此时旧倍率算出的三档价为 0.02 / 0.0333 / 0.04，
--   **全部 ≤ 新价目**，差额方向是少收；发版后倍率生效即与价目一致。
--   反过来（先发版后改价）会有一段按 ¥0.06 / ¥0.09 多收的窗口，不能选。
--
-- v2 完全不动：480P ¥0.03 / 768p ¥0.05（v2ResolutionRatios 未改）。
-- 幂等：重复执行结果一致。

BEGIN;

-- ── 备份 ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS bak_20261108_omni_price AS
SELECT key, value, extract(epoch from now())::bigint AS snap_at
FROM options WHERE key IN ('ModelPrice', 'ModelRatio');

DELETE FROM bak_20261108_omni_price;
INSERT INTO bak_20261108_omni_price (key, value, snap_at)
SELECT key, value, extract(epoch from now())::bigint
FROM options WHERE key IN ('ModelPrice', 'ModelRatio');

CREATE TABLE IF NOT EXISTS bak_20261108_omni_card (LIKE models INCLUDING DEFAULTS);
TRUNCATE bak_20261108_omni_card;
INSERT INTO bak_20261108_omni_card
SELECT * FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL;

-- ── 1. 价格：ModelPrice 基价 ¥0.02/秒（0.02 / 7.3）──────────────────────
UPDATE options
SET value = jsonb_set(value::jsonb, '{minimax_h3_omni}', to_jsonb(0.0027397260273972603))::text
WHERE key = 'ModelPrice';

-- ── 2. 广场卡片：新价目 + 出片速度 ──────────────────────────────────────
UPDATE models
SET description = 'FastH3-Omni 多模态参考视频模型：最多 9 张设定参考图 + 3 个参考视频 + 3 个参考音频（首尾帧可另用 metadata.first_frame / last_frame 锚定，与设定参考同请求共存）。出片快：768P 约 1-2 分钟出片，3 分钟内可交付。按秒计费 ¥0.02（480P）/ ¥0.04（768p，720P 档）/ ¥0.06（1080P），时长 1-15 秒可选（默认 5 秒），支持 9 档画面比例（480p/768p/1080p × 横屏/竖屏/1:1）。prompt 中用 <Picture 1> / <Video 1> / <Audio 1> 依次引用对应素材。',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'minimax_h3_omni'
  AND deleted_at IS NULL;

COMMIT;

\echo '=== 复核：ModelPrice（omni 应为 0.0027397260273972603，v2 应仍为 0.00410958904109589）==='
SELECT (value::jsonb ->> 'minimax_h3_v2') AS v2_base,
       (value::jsonb ->> 'minimax_h3_omni') AS omni_base
FROM options WHERE key = 'ModelPrice';

\echo '=== 复核：卡片描述 ==='
SELECT id, model_name, status, length(description) AS desc_len, left(description, 120) AS head
FROM models WHERE model_name = 'minimax_h3_omni' AND deleted_at IS NULL;

\echo '=== 复核：折算对客单价（omni 0.02/0.04/0.06，v2 0.03/0.05）==='
SELECT
  round((value::jsonb ->> 'minimax_h3_omni')::numeric * 7.3, 4) AS omni_480p_cny,
  round((value::jsonb ->> 'minimax_h3_omni')::numeric * 7.3 * 2.0, 4) AS omni_768p_cny,
  round((value::jsonb ->> 'minimax_h3_omni')::numeric * 7.3 * 3.0, 4) AS omni_1080p_cny,
  round((value::jsonb ->> 'minimax_h3_v2')::numeric * 7.3, 4) AS v2_480p_cny,
  round((value::jsonb ->> 'minimax_h3_v2')::numeric * 7.3 * 1.667, 4) AS v2_768p_cny
FROM options WHERE key = 'ModelPrice';
