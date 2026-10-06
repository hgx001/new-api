-- 调价探针的真实成本台账（2026-10-05）
--
-- 背景：修复 relay_task.go 的 OtherRatios 逐项 int() 截断 bug（同一请求扣费随机）时，
-- 为了观察「扣费抖动」跑了 9 个**付费真实任务**。客户侧已全部退款，但**上游有赞侧
-- 真实扣掉的点数撤不回来**，这 810 分是净损失，由运营方承担。
--
-- 有赞对 wan3.0-smart 不按分辨率区分扣点：720p 与 1080p 均为 90 分/任务
-- （task 314 是 720p，size=1；306-313 是 1080p，size=1.0714，costPoints 都是 90）。
-- 所以那 8 个 1080p 探针相对 720p 并没有多花上游成本，纯粹是没意识到可以只跑 720p。

INSERT INTO logs (user_id, created_at, type, content, username, token_name, model_name, quota, channel_id)
SELECT 2, EXTRACT(EPOCH FROM TIMESTAMP '2026-10-05 22:35:00')::bigint, 5,
       '[OPERATOR-COST-ACCOUNTING] 调价探针上游净损 810 有赞积分（9 任务 × 90 分，task 306-314）。'
       || ' 客户侧已按 410948+410956+95890 quota 全额退款并留痕；'
       || '上游 costPoints 已实扣且无法撤销，810 分由运营方承担。'
       || ' 方法错误：确定性计费 bug 应用单元测试 + go test -count=N 验证，真实任务只需 1 个。'
       || ' 参见 AGENTS.md「计费验证的付费探针纪律」。',
       'admin', 'OPERATOR-COST-ACCOUNTING', 'wan3.0-smart', 0, 17
WHERE NOT EXISTS (
  SELECT 1 FROM logs WHERE content LIKE '%OPERATOR-COST-ACCOUNTING%'
);

SELECT '=== 台账已落库 ===';
SELECT id, type, model_name, to_char(to_timestamp(created_at) AT TIME ZONE 'Asia/Shanghai','MM-DD HH24:MI') AS at
  FROM logs WHERE content LIKE '%OPERATOR-COST-ACCOUNTING%';
SELECT '=== 同时刻客户侧退款留痕（对照）===';
SELECT count(*) AS refunded_logs, sum(quota) AS refunded_quota
  FROM logs WHERE content LIKE '%REFUNDED-BY-OPERATOR%';
