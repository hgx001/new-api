# 生产运维与安全验证

修改部署、渠道恢复、真实探针或OAuth运维前读。发布与回滚唯一流程仍是 [deploy/README.md](../../deploy/README.md)，本文件只补安全边界，不复刻另一套部署命令。

## 发布前后

- 后端用 `deploy/deploy-backend.sh <sha>`，在生产机以精确已提交SHA远程构建Dockerfile.deploy并重建容器；本地Docker不是前提，不要求用户开Docker Desktop。
- 前端静态发布与后端分开，不拿前端脚本替代后端发版；不临时脚本绕过仓库的fetch fallback/SHA断言/回滚分支。
- 服务器无法fetch时可按现有流程传bundle，再让canonical脚本继续，不手工拼另一套docker run遗漏原参数。
- 不把未提交WIP随发版上线，不改无关文件；生产容器不是compose管理，不执行compose破坏性命令。
- 发版前检查磁盘；env快照必须完整、受保护、含SQL_DSN/SESSION_SECRET/CRYPTO_SECRET，空文件/缺项在停旧实例前拒绝。
- 健康检查不仅看success:true，还需setup:true、正确数据库后端、SQL_DSN仍在；SQLite回落空库也可能返回success，不能把空壳当健康。失败回滚。
- 实际数据库类型与容器env现查；旧one-api.db不能冒充生产PostgreSQL。备份须匹配真实数据库，不打印DSN或完整env。

## 零成本探针不靠猜参数

- 视频先单测、历史产物、日志/DB；真实提交只在用户明确授权次数/费用后进行，默认最多1条，扩量/重跑另行确认。
- 最小 `{model,prompt:"x"}` 可能完全合法并建单；有些Seedance分支不校验n，传一个怪n不保证拒单。探针必须证明本地校验先拒，而不是猜“上游应该失败”。
- 本地400探针也要核对quota差值0、无新增任务、ArcReel建单数无变化；某些上游402拒绝仍可能实际生成扣费，HTTP失败不证明免费。
- 确定性计费/浮点/顺序问题用单测复现，概率采样用go test -count，不重复花上游额度“看抖动”。
- 交付用历史ready产物与已授权读取身份验证，不新建视频。
- 如误建，必须两端处理：new-api退款/失败留痕及ArcReel取消；本站没有通用 `/v1/videos/{id}/cancel` 可替代两端操作。先确认真实状态，不能取消已交付任务引发退款错账。
- 客户退款不等于上游退款，付费探针按既有OPERATOR-COST-ACCOUNTING记录净成本；具体台账规则见 [定价与目录](pricing-catalog.md)。

## 渠道与OAuth恢复

- AutoBan恢复优先 `scripts/restore-channel.sh`，同时改channels.status与abilities.enabled；不要只见status正常就宣称全模型可用。
- 上游key的实际格式/编码要检查，但不输出key全文；恢复后区分未授权、套餐限制与短时限流，不能每种故障都重导OAuth。
- sub2api是自家账号池服务，按new-api渠道→sub2api路由/账号日志→上游响应关联，不只看status=active或/v1/models=200。
- 本机历史恢复脚本为 `deploy-vpn/sub2api_autocheck_task.py`，该目录不入Git，**先确认本机文件与计划任务存在且现行**；不能把机器私有脚本当所有源码副本都具备。
- OAuth重导需明确授权；读取本机凭据时不打印/转存明文，expires_at按实际新JWT exp更新，不能沿用旧过期时间。
- 模型rate_limited等待既定冷却，plan_gated不能靠重导解决；Token revoked才走对应凭据恢复。
- 状态字段不等于真实可用，但真实探活也不是天然免费。按费用规则用已授权最小文本验证，不用图片/视频或可能触发refresh的/test接口盲测。
- Windows交互式计划任务在用户注销后可能不运行；调整登录方式需权限/用户确认，不为保活把凭据写入脚本或关闭安全边界。

## 运维记录边界

固定渠道ID、某日账号数/余额/Token时间、支付订单号、容器磁盘快照和逐秒复盘不再放入口文档。日志、DB与脚本可直接定位的普通故障不另存一份“永久经验”；保留的是会丢数据、重复扣费、错误结算或泄露凭据的条件与护栏。
