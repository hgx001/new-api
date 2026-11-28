-- 语音模型描述定稿：把「卡片上的 token 单价」与「客户真正关心的单位」对上，
-- 并写进 2026-11-28 的实测数据。只改展示，不动计费。
BEGIN;

UPDATE models SET
  description = '语音识别：把音频转写成文本，支持中英混合识别与 20 种语言提示，可选说话人分离与 srt / vtt 字幕导出。单次音频不超过 500 秒、50 MB，格式支持 wav / mp3 / aac / flac / m4a / ogg / opus。按音频时长计费 ¥0.5/小时（不足 1 分钟按分钟折算）。卡片上的「¥8.3333/1M」是 token 单价，折算关系为 1 分钟音频 = 1000 token；实测 4.5 秒音频扣费 ¥0.0007。',
  tags = 'audio,asr',
  status = 1,
  updated_time = EXTRACT(EPOCH FROM now())::bigint
WHERE model_name = 'asr-1.0';

UPDATE models SET
  description = '语音合成（高清档）：文本转语音，支持音量、语调、语速与混音参数，可调采样率与比特率；单次文本不超过 1 万字符。按输入字符数计费 ¥3.5/万字符。卡片上的「¥350/1M」是 token 单价，1 个字符 = 1 token，与「¥3.5/万字符」等价；实测 17 字短句扣费 ¥0.0034。不传 voice 时默认使用 male-qn-qingse 音色。',
  tags = 'audio,tts',
  status = 1,
  updated_time = EXTRACT(EPOCH FROM now())::bigint
WHERE model_name = 'speech-2.8-hd';

UPDATE models SET
  description = '语音合成（极速档）：参数与高清档一致，速度更快、单价更低；单次文本不超过 1 万字符。按输入字符数计费 ¥2.0/万字符。卡片上的「¥200/1M」是 token 单价，1 个字符 = 1 token，与「¥2.0/万字符」等价；实测 17 字短句扣费 ¥0.0034（约 1.3 秒产生 73KB mp3）。不传 voice 时默认使用 male-qn-qingse 音色。',
  tags = 'audio,tts',
  status = 1,
  updated_time = EXTRACT(EPOCH FROM now())::bigint
WHERE model_name = 'speech-2.8-turbo';

COMMIT;

SELECT model_name, length(description) AS desc_len FROM models
WHERE model_name IN ('asr-1.0','speech-2.8-hd','speech-2.8-turbo');
