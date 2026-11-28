-- 给三个语音模型补广场卡片的描述与标签（只影响展示，不影响计费/路由）。
-- 幂等：已存在同名元数据行时不覆盖。
BEGIN;

INSERT INTO models (model_name, description, tags, status, name_rule, sort_weight, sync_official, created_time, updated_time)
SELECT v.model_name, v.description, v.tags, 1, 0, 0, 0, EXTRACT(EPOCH FROM now())::bigint, EXTRACT(EPOCH FROM now())::bigint
FROM (VALUES
  ('asr-1.0',
   '语音识别：把音频转写成文本，支持中英混合识别、说话人分离，以及 srt / vtt 字幕导出。单次音频不超过 500 秒、50 MB，格式支持 wav / mp3 / aac / flac / m4a / ogg / opus。按音频时长计费 ¥0.5/小时（不足 1 分钟按分钟折算）。',
   'audio,asr'),
  ('speech-2.8-hd',
   '语音合成（高清档）：文本转语音，支持音量、语调、语速与混音参数，可调采样率与比特率。单次文本不超过 1 万字符。按输入字符数计费 ¥3.5/万字符（1 个汉字按 2 个字符计）。',
   'audio,tts'),
  ('speech-2.8-turbo',
   '语音合成（极速档）：文本转语音，参数与高清档一致，速度更快、单价更低。单次文本不超过 1 万字符。按输入字符数计费 ¥2.0/万字符（1 个汉字按 2 个字符计）。',
   'audio,tts')
) AS v(model_name, description, tags)
WHERE NOT EXISTS (
  SELECT 1 FROM models m WHERE m.model_name = v.model_name AND m.deleted_at IS NULL
);

COMMIT;

SELECT model_name, tags, left(description, 30) AS d FROM models WHERE model_name IN ('asr-1.0','speech-2.8-hd','speech-2.8-turbo');
