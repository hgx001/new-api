-- 误建探针的运营成本台账（2026-10-26）
--
-- 事件：恢复上架 wan2.7-r2v 后做「零成本探针」，我以为 duration=30 越界会被本地
--      400 拦下（seedance-2.0 那类模型确实是 400）。实际 youzanwan3 的 r2v 是
--      **把时长钳制到 5–10 秒**（resolveDuration 夹取），不是拒绝 → 真建了一单：
--      task 727 / task_U7UeCCoMh222jC3ljCuKcOQA5h96VFYA，10 秒、1 张参考图。
--
-- 实际损失：**0**
--   · 上游有赞：因参考图 URL 是假域名（example.invalid）被上游拒绝，
--     chargedPoints=0、refundedPoints=20（20 积分 ≈ ¥0.20 未实际扣除），
--     失败原因「参考素材已失效或无法访问，请重新上传素材后再试」。
--   · 客户侧：预扣 68490 quota（¥1.00）已由本站自动全额退还（退款日志 id 8416，
--     type=6），用户 2（auto_teslfkac）余额净变化 0。
--
-- 方法教训：探针「用越界参数试探」不等于零成本 —— 钳制型 adaptor 会把非法值
-- 改成合法值并真的建单。恢复上架类验证应改用**不触发建单**的只读手段
-- （/v1/models、/api/pricing、能力行查询），或直接申请 1 个付费任务额度。

\set ON_ERROR_STOP on

INSERT INTO logs (user_id, created_at, type, content, username, token_name, model_name, quota, channel_id)
SELECT 2, EXTRACT(EPOCH FROM TIMESTAMP '2026-10-26 13:49:38')::bigint, 5,
       '[OPERATOR-COST-ACCOUNTING] 恢复上架 wan2.7-r2v 的探针误建 1 个真实任务'
       || '（task 727 / task_U7UeCCoMh222jC3ljCuKcOQA5h96VFYA，10 秒，预扣 68490 quota=¥1.00）。'
       || ' 净损失 0：上游 chargedPoints=0 且 refundedPoints=20（假参考图域名被拒，20 积分已退）；'
       || '客户侧已自动全额退款（退款日志 id 8416），用户 2 余额净变化 0。'
       || ' 方法教训：youzanwan3 的 r2v 把越界时长**钳制**到 5–10 秒而不是 400 拒绝，'
       || '所以「用越界参数探针」不是零成本；恢复上架类验证应只用只读手段。',
       'auto_teslfkac', 'OPERATOR-COST-ACCOUNTING', 'wan2.7-r2v', 0, 17
WHERE NOT EXISTS (
  SELECT 1 FROM logs WHERE content LIKE '%OPERATOR-COST-ACCOUNTING%' AND content LIKE '%task 727%'
);

\echo '--- 台账已落库 ---'
SELECT id, type, model_name, quota,
       to_char(to_timestamp(created_at) AT TIME ZONE 'Asia/Shanghai','MM-DD HH24:MI') AS at
  FROM logs WHERE content LIKE '%OPERATOR-COST-ACCOUNTING%' ORDER BY id;

\echo '--- 近 2 小时新建任务（应只有 task 727 这一个探针单）---'
SELECT id, task_id, platform, status, quota,
       to_char(to_timestamp(created_at) AT TIME ZONE 'Asia/Shanghai','MM-DD HH24:MI') AS at,
       left(coalesce(fail_reason,''),60) AS fail
  FROM tasks WHERE created_at > extract(epoch from now())::bigint - 7200 ORDER BY id;

\echo '--- 用户 2 当前余额（与退款日志核对）---'
SELECT id, username, quota, used_quota FROM users WHERE id = 2;
