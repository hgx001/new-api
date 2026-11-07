-- 生产台账：2026-11-07 Phase 7 部署复测（双协议 cloud_image_fallback.py 上线后，逐模型 n=1）
-- 授权依据：用户 2026-11-07「图片模型在测试下 现在应该通了」（部署复测属授权范围）
-- quota 字段一律写 0（真实扣费已在 logs 12169/12170/12171 入账，台账行不重复计入余额统计）
-- 本次走 m3only 沙箱组（token 166 m3only受限令牌，通道 ch30 漫屋ArcReel-image(m3only)）。
-- 失败反证：gpt-image-2.5-官方（logs 12167/12168，ch29，quota=0 未扣费）连打 502/503
--   「No available compatible accounts」——sub2api group 4 唯一账号 codex-plus-snehitoge828
--   OAuth 被上游吊销（Token revoked 401）后 schedulable=f，非 new-api 路由问题。
--   恢复须 sub2api 管理端重新授权，禁自动触发 OAuth 恢复（见 AGENTS.md 未决坑）。

INSERT INTO logs (user_id, created_at, type, content, username, token_name, model_name,
                   quota, prompt_tokens, completion_tokens, use_time, is_stream,
                   channel_id, channel_name, token_id, "group", ip, request_id, other)
VALUES
(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Seedream 4.5 部署复测（1 张，n=1，异步端点 POST /v1/videos 提交→轮询→content）。客户侧实扣 13698 quota = ¥0.20（与 logs 12169 同笔，台账不重复计）。交付：HTTP 200 / 全程 198.1s / 5,052,392 bytes / image/png（magic 89504e47）。部署物：cloud_image_fallback.py 双协议版（_try_task_model 提交→轮询→取图），arcreel 网关自检 194/194。上游：channel 30 漫屋ArcReel-image(m3only)。依据：用户 2026-11-07「图片模型在测试下 现在应该通了」。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Seedream 4.5', 0, 0, 0, 0, false,
 30, '漫屋ArcReel-image(m3only)', 166, 'm3only', '', '', ''),

(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Nano Banana Pro 部署复测（1 张，n=1，异步端点 POST /v1/videos）。客户侧实扣 20547 quota = ¥0.30（与 logs 12170 同笔，台账不重复计）。交付：HTTP 200 / 全程 51.3s（比首测 110s 快，上游波动）/ 1,032,164 bytes / image/png。ratio 未下发走 16:9 缺省。上游：channel 30 漫屋ArcReel-image(m3only)。依据：用户 2026-11-07「图片模型在测试下 现在应该通了」。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Nano Banana Pro', 0, 0, 0, 0, false,
 30, '漫屋ArcReel-image(m3only)', 166, 'm3only', '', '', ''),

(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Seedream 5.0 Flash 部署复测（1 张，n=1，异步端点 POST /v1/videos）。客户侧实扣 13698 quota = ¥0.20（与 logs 12171 同笔，台账不重复计）。交付：HTTP 200 / 全程 109.7s（首测 50s，上游波动）/ 4,054,863 bytes / image/png。上游：channel 30 漫屋ArcReel-image(m3only)。依据：用户 2026-11-07「图片模型在测试下 现在应该通了」。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Seedream 5.0 Flash', 0, 0, 0, 0, false,
 30, '漫屋ArcReel-image(m3only)', 166, 'm3only', '', '', '');

\echo '=== 台账行 ==='
SELECT id, model_name, quota, channel_id, left(content, 60) AS head FROM logs
WHERE token_name='OPERATOR-COST-ACCOUNTING' AND created_at > extract(epoch from now())::bigint - 60
ORDER BY id;

\echo '=== 本次复测真实扣费核对（logs 12169-12171，不是台账行）==='
SELECT id, model_name, quota, round(quota::numeric/500000*7.3,4) AS cny
FROM logs WHERE id BETWEEN 12169 AND 12171 ORDER BY id;
