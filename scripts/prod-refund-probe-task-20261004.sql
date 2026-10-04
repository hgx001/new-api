-- 误建探针任务退款：task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz（seedance-2.0，prompt "probe"）
-- 该任务是验证脚本用「合法最小入参」探测造成的误建（违反只用必然失败入参的规则）。
-- ArcReel 侧已 cancel（job gen-03577f991a74e9669017e695，钱包预扣 150 分已释放，
-- Worker mac-mini-001 已回 job.cancelled，未真正提交官网）。
-- new-api 侧无 /v1/videos/{id}/cancel 端点，轮询因任务停在 IN_PROGRESS 不会自动退款，
-- 故手工按「取消 + 退款 + 标记日志」处理。
BEGIN;

CREATE TABLE IF NOT EXISTS _bak_probe_task_20261004 AS
  SELECT * FROM tasks WHERE task_id = 'task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz';

-- 退款金额取任务实际预扣（quota 字段），避免手写常量与预扣脱节
-- （CTE 只在单条语句内可见，三处退款各写一次子查询，不建临时表）
UPDATE users u
   SET quota = u.quota + (
         SELECT quota FROM tasks
          WHERE task_id = 'task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz')
 WHERE u.id = 2;

UPDATE tokens t
   SET used_quota = GREATEST(t.used_quota - (
         SELECT quota FROM tasks
          WHERE task_id = 'task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz'), 0)
 WHERE t.id = 1;

UPDATE tasks
   SET status = 'FAILURE',
       updated_at = EXTRACT(EPOCH FROM now())::bigint
 WHERE task_id = 'task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz';

UPDATE logs
   SET content = content || ' [REFUNDED: probe task task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz cancelled on both sides (ArcReel job gen-03577f991a74e9669017e695, wallet reservation released, worker confirmed cancel); never submitted to dola official site]'
 WHERE type = 2
   AND content LIKE '%task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz%';

COMMIT;

SELECT task_id, status, quota FROM tasks WHERE task_id = 'task_jSsGH01Lziws18qlMrnW49JZRpHyvOrz';
SELECT 'user_quota=' || quota FROM users WHERE id = 2;
SELECT 'token1_used=' || used_quota FROM tokens WHERE id = 1;
