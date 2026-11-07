-- 生产台账：2026-11-07 SeetaCloud 渠道模型改版（文生/图生合并 + Omni 多模态参考）
-- 授权依据：用户 2026-11-07「newapi 之前的两个文生图生的是不是要改下，现在不是合并为一个了吗，然后又多了多模态参考的模型」
-- quota 一律 0（真实扣费已在 logs 入账）。成本真相源：logs 12312-12342。
--   正向 4/4 completed：v2 t2v 60.5s、v2 i2v(首帧) 54.3s、omni 设定参考 2图+音频 60.4s、omni 1080p 69.2s；
--   负向 3/3 本地 400：v2 传 3 图（关键帧上限 2）/ v2 传音频（无槽位）/ v2 传 1080p（仅 Omni）；
--   计费口径实测：v2 480p 5s=¥0.15、omni 480p 5s=¥0.15、omni 1080p 5s=¥0.30（倍率 2.0 生效）；
--   2 单因测试脚本 PNG 数据损坏失败（上游 avcodec inflate -3），已自动退款 ¥0.30，换合法 PNG 补跑通过；
--   净支出 ¥0.90（含 2 单连通性探针 ¥0.30）。

INSERT INTO logs (user_id, created_at, type, content, username, token_name, model_name,
                   quota, prompt_tokens, completion_tokens, use_time, is_stream,
                   channel_id, channel_name, token_id, "group", ip, request_id, other)
VALUES
(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] minimax_h3_v2 改版实测（文生/图生合并为单模型）。证据：t2v task_KR8ehRkuoTq380nCq3vmi0EIxm54ems1 completed 60.5s；i2v 首帧 task_rgt3zAr8fcd93p53yfZ2nlruAqIPxE8v completed 54.3s；负向 3/3 本地 400（3 图超关键帧上限 / 音频无槽位 / 1080p 仅 Omni）；计费 480p 5s=¥0.15，失败单自动退款。旧名 minimax_h3-文生/图生 退役为隐藏别名（ModelList 与广场不再出现，老请求仍路由）。生产 ch28 models/abilities/ModelPrice 已同步（备份 bak_20261107_seeta_*），部署 2dca0fab3。依据：用户 2026-11-07 授权。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'minimax_h3_v2', 0, 0, 0, 0, false,
 28, 'SeetaCloud', 131, 'default', '', '', ''),

(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] minimax_h3_omni 上架实测（FastH3-Omni ref2va 多模态参考）。证据：设定参考 2图+1音频 task_fVFrwSBYNTtaT7qdANy7KZ5x5ucRYp2Y completed 60.4s（ref_image_0/1 + ref_audio_0 data URI 下发）；1080p 档 task_HqWf5Dl7EL6ZpC2DkUl4NvATm4mVS1sc completed 69.2s，计费 ¥0.30/5s（倍率 2.0 暂定价生效）。上限：9 图 + 3 视频 + 3 音频；首尾帧走 metadata.first_frame/last_frame。注意：1080p 暂定价待商务复核，调价改 seetaResolutionRatios 一处。依据：用户 2026-11-07 授权。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'minimax_h3_omni', 0, 0, 0, 0, false,
 28, 'SeetaCloud', 131, 'default', '', '', '');

\echo '=== 台账行 ==='
SELECT id, model_name, quota, channel_id, left(content, 60) AS head FROM logs
WHERE token_name='OPERATOR-COST-ACCOUNTING' AND created_at > extract(epoch from now())::bigint - 60
ORDER BY id;
