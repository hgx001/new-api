# scripts/ 目录索引

一次性运维脚本与历史 SQL 的归档区。**部署入口已统一在 [deploy/](../deploy/README.md)**，本目录不再有部署脚本。

⚠️ 跑任何脚本前先读内容确认。凡会创建真实任务、扣费、退款、充值或改生产数据的脚本，受 [AGENTS.md](../AGENTS.md)「生产运维硬约束」管制：未经用户明令不得运行；每个付费探针跑完必须写 `OPERATOR-COST-ACCOUNTING` 台账；验证类探针只允许用必然被 400 拦截的非法入参（范例见 `probe-manwu-dola-dual.sh`）。

| 类别 | 前缀匹配 | 代表 | 说明 |
|---|---|---|---|
| 部署 | — | （无，已迁至 `deploy/`） | 前端 `deploy/deploy-frontend.sh`，后端 `deploy/deploy-backend.sh` |
| ⚠️ 危险·动钱动产 | `probe-*` `refund-*` `cancel-*` `topup-*` `cleanup-*` `smoke-*` `run-*` | `probe-r2v-*.sh`、`refund-accidental-task.sh`、`topup-user.sh`、`cancel-arcreel-jobs.sh` | 可能真实建单扣费、退款、撤单、充值、改任务状态 |
| 审计/只读 | `audit-*` `coverage-*` `stat-*` `diag-*` `erchun-readonly-*` `prod-check-*` | `audit-pricing-vs-billing.js`、`diag-task249.sh` | 查询与核对，不改数据（跑前仍建议读一遍） |
| 一次性留痕 | `prod-*.sql`、`setup-*`、`create-*` 及其余 dated 脚本 | `prod-rename-doubao-seedance-20261012.sql`、`setup-minimax-h3-prod.sh` | 历史操作凭证，勿重复执行 |

分类按命名惯例归置，不代表逐一审核过内容；拿不准就当危险类处理。
