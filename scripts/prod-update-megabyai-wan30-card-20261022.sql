-- 生产：wan-3.0 广场卡片去掉「（第三方 MegaAI 渠道）」字样（2026-10-22 晚）
--
-- 与 seedance2.5-特惠 卡片同一诉求：卡片文案里不暴露渠道来源。
--
-- ⚠️ 这是**纯文案改动**：不动 option、不动代码、不动能力行 → **无需发版**，
--    改完等广场缓存过期（≤1min）即可生效。
--
-- 幂等：可重复执行。

UPDATE models
SET description = 'Wan3.0 满参视频生成。按次计费：480P ¥3.00/次、720P ¥3.50/次、1080P ¥4.00/次，时长 5/10/15/30 秒（不随时长加价）。支持 10 张参考图、5 段参考视频、5 条参考音频，全比例，提示词 ≤5000 字符。',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'wan-3.0';

-- 复核
SELECT model_name, description FROM models WHERE model_name = 'wan-3.0';
