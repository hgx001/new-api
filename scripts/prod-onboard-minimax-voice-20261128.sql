-- MiniMax 官方直连（channels.id=1，type 35）语音模型上架。
--
-- 三档：
--   asr-1.0            语音识别，POST /v1/speech_to_text，按音频时长计费
--   speech-2.8-hd      同步 TTS，POST /v1/t2a_v2，按输入字符数计费
--   speech-2.8-turbo   同步 TTS，同上，更便宜更快
--
-- 计费口径（与 service/token_counter.go、service/quota.go 对齐）：
--   转录：1 分钟音频 = 1000 token → 1 小时 = 60000 token
--         quota = token × model_ratio；¥ = quota ÷ 500000 × 7.3
--         ¥0.5/小时 → ratio = 0.5 × 500000 ÷ (60000 × 7.3) = 0.5707762557077626
--   TTS ：我们的 token = 输入字符数（rune），ratio × 14.6 = ¥/1M token
--         ¥3.5/万字符 → ratio = 3.5 × 500000 ÷ (10000 × 7.3) = 23.972602739726027
--         ¥2.0/万字符 → ratio = 2.0 × 500000 ÷ (10000 × 7.3) = 13.698630136986301
--
-- 两台各执行一次；幂等（并集追加 / ON CONFLICT / jsonb 合并）。

BEGIN;

-- 1) 渠道模型清单：并集追加，保留已有三个文本模型
UPDATE channels
SET models = (
  SELECT string_agg(DISTINCT m, ',' ORDER BY m)
  FROM unnest(string_to_array(coalesce(models, '') || ',asr-1.0,speech-2.8-hd,speech-2.8-turbo', ',')) AS m
  WHERE btrim(m) <> ''
)
WHERE id = 1;

-- 2) 能力表：default 分组 × 三个模型 × 渠道 1
INSERT INTO abilities ("group", model, channel_id, enabled, priority, weight, tag)
SELECT g, m, 1, true, 0, 1, NULL
FROM (VALUES ('default')) AS t(g)
CROSS JOIN (VALUES ('asr-1.0'), ('speech-2.8-hd'), ('speech-2.8-turbo')) AS mm(m)
ON CONFLICT ("group", model, channel_id) DO UPDATE SET enabled = true;

-- 3) 模型倍率（没有配倍率会被判 model price not configured，直接 400）
UPDATE options
SET value = (
  value::jsonb || '{
    "asr-1.0": 0.5707762557077626,
    "speech-2.8-hd": 23.972602739726027,
    "speech-2.8-turbo": 13.698630136986301
  }'::jsonb
)::text
WHERE key = 'ModelRatio';

COMMIT;

-- 验收
SELECT id, name, models FROM channels WHERE id = 1;
SELECT "group", model, enabled FROM abilities WHERE channel_id = 1 ORDER BY model;
