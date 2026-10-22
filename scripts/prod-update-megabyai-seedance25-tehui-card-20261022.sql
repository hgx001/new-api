-- 生产：更新 seedance2.5-特惠 的广场卡片描述（补「仅 720p / 固定 30 秒」）
--
-- 该模型的档位在 2026-10-22 被锁死：只出 720p、只出 30 秒一条（代码侧
-- megabyaiLockedTierModels + fail-loud 门控）。卡片描述必须同步写清楚，
-- 否则用户会按 ¥5.00/条 买一个自己以为能选 1080p / 5 秒的模型。
--
-- ⚠️ 执行顺序：先发版（代码里要有锁定门控），再跑本脚本。
-- 幂等：可重复执行。

UPDATE models
SET description = 'Seedance 2.5 特惠视频生成（第三方 MegaAI 渠道，Wan3.0 满参版同源）。固定 ¥5.00/条：**仅 720p 分辨率、固定 30 秒一条**（不分档、不随时长加价）。支持 10 张参考图、5 段参考视频、5 条参考音频，全比例，提示词 ≤5000 字符。',
    updated_time = extract(epoch from now())::bigint
WHERE model_name = 'seedance2.5-特惠';

-- 复核
SELECT model_name, description, status FROM models WHERE model_name = 'seedance2.5-特惠';
