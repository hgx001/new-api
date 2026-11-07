-- 生产台账：2026-11-07 用户授权的 4 个图片模型真机实测（各 1 张，n=1）
-- 授权依据：用户 2026-11-07「这几个 模型能用吗 实测下」
-- quota 字段一律写 0（真实扣费已在各自 logs 行入账，台账行不重复计入余额统计）
-- 上游侧成本（ArcReel/官网出账）本站不可见，只记客户侧实扣与交付物证据。

INSERT INTO logs (user_id, created_at, type, content, username, token_name, model_name,
                   quota, prompt_tokens, completion_tokens, use_time, is_stream,
                   channel_id, channel_name, token_id, "group", ip, request_id, other)
VALUES
(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] gpt-image-2.5-官方 授权真机实测（1 张，n=1，quality standard，OpenAI 兼容同步端点 POST /v1/images/generations）。客户侧实扣 10274 quota = ¥0.15（ModelPrice 0.02054794520547945 USD，1 张，无倍率），预扣=实收，无退款、无补扣。交付：HTTP 200 / 40.6s / 返回 b64_json（base64 2,978,792 字符 → PNG 约 2.23MB，magic 89504e47 合法 PNG）；**只回 b64_json、没有 url**，客户端必须自行解码。上游侧：channel 13「GPT-Pool」(type 1 官方)，upstream_model_name=gpt-image-2（公开名 gpt-image-2.5-官方 是官方渠道独立命名），上游出账本站不可见。依据：用户 2026-11-07 授权「能用吗 实测下」。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'gpt-image-2.5-官方', 0, 24, 472, 33, false,
 13, 'GPT-Pool', 131, 'default', '', '', ''),

(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Nano Banana Pro 授权真机实测（1 张，n=1，异步端点 POST /v1/videos，**不走 /v1/images/generations**）。客户侧实扣 20547 quota = ¥0.30（ModelPrice 0.0410958904109589 USD × 张数 1），预扣=实收，无退款、无补扣。交付：task 1566 / task_XAIgqKFpYCxSODe858kHOgiUmkCreWdr SUCCESS，提交 0.2s、轮询约 110s，GET /v1/videos/{id}/content → HTTP 200 / 970977 bytes / image/png（magic 89504e47）/ 尺寸 1024x572（未传比例 → 走 16:9 缺省，与教程「宽度 1024」一致）。上游侧：channel 20「漫屋ArcReel」(type 66)，upstream_task_id=gen-cfd0d995e6cff8d9f287fc47，官网出账本站不可见。依据：用户 2026-11-07 授权「能用吗 实测下」。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Nano Banana Pro', 0, 0, 0, 0, false,
 20, '漫屋ArcReel', 131, 'default', '', '', ''),

(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Seedream 4.5 授权真机实测（1 张，n=1，异步端点 POST /v1/videos，**不走 /v1/images/generations**）。客户侧实扣 13698 quota = ¥0.20（ModelPrice 0.0273972602739726 USD × 张数 1），预扣=实收，无退款、无补扣。交付：task 1567 / task_1EMMi9llRluVvzDYN8recjMU6DPr7wYc SUCCESS，提交 0.1s、轮询约 160s（dola 官网档），GET /v1/videos/{id}/content → HTTP 200 / 5090572 bytes / image/png（magic 89504e47）/ 尺寸 2848x1600（未传比例 → 官网「自动」档出 16:9）。上游侧：channel 20「漫屋ArcReel」(type 66)，upstream_task_id=gen-b1cfc304bde12087724fce7c，upstream_model=dola-seedream-4-5，官网出账本站不可见。依据：用户 2026-11-07 授权「能用吗 实测下」。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Seedream 4.5', 0, 0, 0, 0, false,
 20, '漫屋ArcReel', 131, 'default', '', '', ''),

(28, extract(epoch from now())::bigint, 5,
 '[OPERATOR-COST-ACCOUNTING] Seedream 5.0 Flash 授权真机实测（1 张，n=1，异步端点 POST /v1/videos，**不走 /v1/images/generations**）。客户侧实扣 13698 quota = ¥0.20（ModelPrice 0.0273972602739726 USD × 张数 1），预扣=实收，无退款、无补扣。交付：task 1568 / task_H0qzPkMS9bMOn0QMzHLqaP1d537uGiML SUCCESS，提交 0.1s、轮询约 50s（4 个里最快），GET /v1/videos/{id}/content → HTTP 200 / 4172641 bytes / image/png（magic 89504e47）/ 尺寸 2048x2048（未传比例 → 官网「自动」档出 1:1）。上游侧：channel 20「漫屋ArcReel」(type 66)，upstream_task_id=gen-a4fd4c91c3f2c24c71f0577f，upstream_model=doubao-seedream-5-0-flash，官网出账本站不可见。依据：用户 2026-11-07 授权「能用吗 实测下」。',
 'admin', 'OPERATOR-COST-ACCOUNTING', 'Seedream 5.0 Flash', 0, 0, 0, 0, false,
 20, '漫屋ArcReel', 131, 'default', '', '', '');

\echo '=== 台账行 ==='
SELECT id, model_name, quota, channel_name, left(content, 60) AS head FROM logs
WHERE token_name='OPERATOR-COST-ACCOUNTING' AND created_at > extract(epoch from now())::bigint - 60
ORDER BY id;

\echo '=== 客户侧实扣合计核对（真实扣费行，不是台账行）==='
SELECT model_name, sum(quota) AS quota_sum, round(sum(quota)::numeric/500000*7.3,4) AS cny
FROM logs
WHERE created_at > extract(epoch from now())::bigint - 1800
  AND model_name IN ('gpt-image-2.5-官方','Nano Banana Pro','Seedream 4.5','Seedream 5.0 Flash')
GROUP BY model_name ORDER BY model_name;
