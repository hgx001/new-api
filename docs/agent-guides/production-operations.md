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

## nginx 中间层与模型目录

- **`/v1/models` 若出现「本不该有的模型」，先怀疑 nginx 中间层，别急着翻 new-api 业务代码**（2026-11-04 实测：受限账号下游看到满屏 gpt，根因是 nginx `location = /v1/models` 指向自建 merge shim，绕过了 TokenAuth/分组/白名单；new-api 侧代码完全正确）。
- 排查固定手法：**直连后端端口 vs 经 nginx 各打一次**（`curl http://127.0.0.1:3000/v1/models` 对比 `curl https://<域名>/v1/models`）。直连对、经 nginx 错 ⇒ 问题在 nginx 或其后面的中间进程。`ss -lntp` 找端口归属进程，`cat /proc/<pid>/cmdline` 看真实启动命令。
- 站点专属日志在 `/var/log/nginx/<站点>_error.log`；**全局 `error.log` 是所有 server 的噪音**，别拿它判断本站点问题。用 `nginx -T` 看**实际生效**的配置。
- **nginx 在本机有两份配置且都生效**：`sites-enabled/api.heibaidao.cn.conf` 是**独立实体文件、不是** `sites-available/` 的软链，两份内容本就不同（enabled 版另有 `= /v1/videos`、`= /v1/images`，`location /v1/` 的 timeout 也不同）。改配置必须两份都改，否则 `nginx -t` 通过、reload 成功但**行为完全没变**。下线某个 location 后若请求变成 502，说明还有另一份配置仍指向已停进程。
- 自建 shim/补丁进程用 `crontab -l` 查 `@reboot` 自启条目并一并清除，否则**重启即复活**；下线时优先 `mv` 归档而非删除，保留回滚可能。

## 渠道与OAuth恢复

- AutoBan恢复优先 `scripts/restore-channel.sh`，同时改channels.status与abilities.enabled；不要只见status正常就宣称全模型可用。
- 上游key的实际格式/编码要检查，但不输出key全文；恢复后区分未授权、套餐限制与短时限流，不能每种故障都重导OAuth。
- sub2api是自家账号池服务，按new-api渠道→sub2api路由/账号日志→上游响应关联，不只看status=active或/v1/models=200。
- 本机历史恢复脚本为 `deploy-vpn/sub2api_autocheck_task.py`，该目录不入Git，**先确认本机文件与计划任务存在且现行**；不能把机器私有脚本当所有源码副本都具备。
- OAuth重导需明确授权；读取本机凭据时不打印/转存明文，expires_at按实际新JWT exp更新，不能沿用旧过期时间。
- 模型rate_limited等待既定冷却，plan_gated不能靠重导解决；Token revoked才走对应凭据恢复。
- 状态字段不等于真实可用，但真实探活也不是天然免费。按费用规则用已授权最小文本验证，不用图片/视频或可能触发refresh的/test接口盲测。
- Windows交互式计划任务在用户注销后可能不运行；调整登录方式需权限/用户确认，不为保活把凭据写入脚本或关闭安全边界。

## 任务排障入口

- 异步任务在**提交成功、每次提交失败（含重试）、状态迁移、后处理阶段**各写一行 `task_logs`（`stage/level/message/payload`），保留30天，由轮询入口每天清一次；不记每轮轮询原文，避免表失控。
- 查法：`GET /api/task/logs?task_id=...`（或 `upstream_task_id=...`），仅管理员；也可直接查库。`payload.media` 存客户端原始URL，用来回答“参考图到底有没有随请求发出去”。
- `payload.media_fields` 记请求里**出现过哪些**媒体字段（`media`/`images`/`image`/`input_reference`，各几条），`payload.media_types` 按类型汇总。判断“图没发出去”时必须看 `media_fields`：只看 `media` 分不清“客户端没传图”和“传了但被另一个字段挡住”。（2026-11-02 的丢图故障就是靠这两个字段之外的旁证才定性的。）
- `media` 与 legacy 字段（`images`/`image`/`input_reference`）现在是**合并**关系，不再是“media 非空就跳过 legacy”。首帧/尾帧与参考类素材仍然互斥，矛盾入参返回 400 而不是静默择一。
- 参考图在**建单前**做可达性探测（HEAD→Range GET，判 content-type 与图片魔数），失败直接400 `invalid_input_reference`，不建单不预扣。
- **图片 URL 在建单前会被代下载转存**（2026-11-01）：网关把公网图片拉下来转存到自家 staging 暂存图床，再把自家地址交给上游。上游是“直传URL、生成时下载”模式，探测只证明“**网关**能打开”，不证明“**上游机房**能打开”——客户用境外图床时探测能过、上游却拉不到，任务会在生成阶段以 `Failed to download` 失败。转存后上游不再碰客户的图床。
  - 转存在**预扣费之前**完成，失败即拒单，不会出现「先扣后退」的抖动。
  - 暂存图床本身不可用（未配置漫屋渠道）时**降级放行**原 URL，不阻断用户请求。
  - data URL（base64 内联）不转存，数据已随请求体到达。

## 运维记录边界

固定渠道ID、某日账号数/余额/Token时间、支付订单号、容器磁盘快照和逐秒复盘不再放入口文档。日志、DB与脚本可直接定位的普通故障不另存一份“永久经验”；保留的是会丢数据、重复扣费、错误结算或泄露凭据的条件与护栏。
