-- wan2.7-r2v 恢复上架后的**授权真机验证**成本台账（2026-10-26）
--
-- 背景：老板授权「测试下这个模型能不能用」，上限 1 条真实任务。
--       任务 task_o6e2c3eopOsvTJSz3b4Zph4s3FK3UUFp（wan2.7-r2v / 5 秒 / 16:9 / 720p /
--       1 张参考图）→ 上游 SUCCESS，成片 5.038 秒、4.36MB、video/mp4。
--
-- 真实成本（这一单上游是真扣了，不是失败退款）：
--   · 上游有赞：chargedPoints=10、refundedPoints=0（2 积分/秒 × 5 秒 = 10 积分）
--     → 折 ¥0.10（1 积分 ≈ ¥0.01，按有赞 prime 档 20 积分/秒 ≈ ¥0.2/秒 折算）。
--   · 本站：收 34245 quota = ¥0.50（5 秒 × ¥0.10/秒），毛利 ¥0.40（80%）。
--   · 本条成本由运营方承担（这是验证成本，不是客户付费订单）。

\set ON_ERROR_STOP on

INSERT INTO logs (user_id, created_at, type, content, username, token_name, model_name, quota, channel_id)
SELECT t.user_id, EXTRACT(EPOCH FROM TIMESTAMP '2026-10-26 13:57:21' AT TIME ZONE 'Asia/Shanghai')::bigint, 5,
       '[OPERATOR-COST-ACCOUNTING] wan2.7-r2v 恢复上架后的授权真机验证（1 条，task '
       || t.task_id || '）。上游真实扣 10 积分（2 积分/秒 × 5 秒，chargedPoints=10、'
       || 'refundedPoints=0）≈ ¥0.10（1 积分 ≈ ¥0.01）；本站收 34245 quota = ¥0.50'
       || '（5 秒 × ¥0.10/秒），毛利 ¥0.40（80%）。成片 5.038 秒 / 4.36MB / video/mp4。'
       || ' 本单为用户授权的恢复上架验证（上限 1 条），未额外跑量，成本由运营方承担。',
       'auto_teslfkac', 'OPERATOR-COST-ACCOUNTING', 'wan2.7-r2v', 0, 17
  FROM tasks t
 WHERE t.task_id = 'task_o6e2c3eopOsvTJSz3b4Zph4s3FK3UUFp'
   AND NOT EXISTS (
     SELECT 1 FROM logs WHERE content LIKE '%OPERATOR-COST-ACCOUNTING%'
       AND content LIKE '%task_o6e2c3eopOsvTJSz3b4Zph4s3FK3UUFp%'
   );

\echo '--- 台账已落库（近 3 条）---'
SELECT id, type, model_name,
       to_char(to_timestamp(created_at) AT TIME ZONE 'Asia/Shanghai','MM-DD HH24:MI') AS at,
       left(replace(content, E'\n', ' '), 150) AS content
  FROM logs WHERE content LIKE '%OPERATOR-COST-ACCOUNTING%' ORDER BY id DESC LIMIT 3;
