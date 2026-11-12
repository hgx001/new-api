# 新服务器迁移设计方案（new-api + sub2api）

> 2026-11-12 起草，同日按老机实际部署逐项复核并修正（见 §11 审查记录）。
> **2026-11-12 当日实施完成（§5 步骤 1-7、10 已完成）**：新机 new-api + sub2api 全套容器 healthy，`https://api2.heibaidao.cn` 已可用（证书 2027-02-10 到期，certbot 自动续期）。§8 验收清单已核（详情见文末 §12）。未完成：§5 步骤 8（一期 COS 转存 adaptor，待 TDD）与步骤 9（新机发布流程适配）。
> 目标：老服务器（119.29.253.97，广州，到期在即）保持原样继续运行现有业务；新服务器（114.132.252.150，广州，腾讯云轻量 2C4G/59G/5M）完整复刻 new-api + sub2api 两套业务，承载**新业务**，并新增 COS/CDN 产物分发链路。

## 1. 迁移范围

### 1.1 new-api 全套

| 项 | 老机位置 | 实测大小 | 迁移方式 |
|---|---|---|---|
| 应用镜像 | `new-api-custom:latest` | **231 MB** | `docker save` → COS 中转 → 新机 `docker load`（同镜像，无构建差异；`rollback-*` 标签不迁） |
| 数据库 | 容器 `postgres`（postgres:15-alpine，库 `new-api`，用户 root） | **89 MB** | `pg_dump -Fc` → COS 中转 → 新机 `pg_restore` |
| 缓存 | 容器 `redis`（redis:7-alpine） | 纯缓存 | 新机跑空实例，数据不迁 |
| 前端产物 | `/opt/new-api/frontend/default/dist` 是**软链** → `releases/20261112T034413Z-7a32c6e1421f` | 当前 release **52 MB**；目录合计 266 MB（含 3 个历史 release + dist.bak + legacy-dist） | 只复制当前 release 目录 + classic（43 MB），到新机重建同名软链，不搬历史 release |
| 应用编排 | `/home/ubuntu/new-api/`：`docker-compose.yml`、`.env`（463 B）、`data/`（1.5 MB）、`logs/`（**219 MB 历史日志，不迁**） | 小 | compose + `.env` + `data/` 打包；`logs/` 新机建空目录 |
| 源码 | `/home/ubuntu/new-api-src`（含 `.git` 141 MB） | **246 MB**（已剔除 node_modules 2.0G） | tar → COS；后续发布走 `git bundle` 直传流程 |
| 部署脚本 | `/home/ubuntu/deploy-new-api.sh`、`/home/ubuntu/new-api/{deploy-prod.sh,replace-container.sh,restart.sh}` | 小 | 直接 scp |
| VPN 部署目录 | `/home/ubuntu/new-api/vpn-deploy`（静态二进制 160 MB + 两个历史 tar.gz） | 252 MB | **不迁**：无 systemd/docker 服务在跑（仅 `tat_agent`），属历史构建产物；`.env` 里的 `VPN_NODE_AGENT_TOKEN` 保留 |
| nginx 配置 | `/etc/nginx/sites-available/api.heibaidao.cn.conf` | 小 | 复制后改 server_name；**超时参数必须原样保留**：任务接口 `proxy_read_timeout 18000s`、轮询 600s、其他 300s，`client_max_body_size 100M` |
| 证书 | `/etc/letsencrypt/live/api.heibaidao.cn/` | 小 | 新域名 certbot 新签（不复制旧证书） |

### 1.2 sub2api 全套（账号池上游）

| 项 | 老机位置 | 实测大小 | 迁移方式 |
|---|---|---|---|
| 应用镜像 | `sub2api:0.2.3-codex-image-v6`（容器 `sub2api`） | **135 MB** | `docker save` → COS 中转 → `docker load` |
| 数据库 | 容器 `sub2api-postgres`（postgres:18-alpine），volume `sub2api_postgres_data` | **553 MB** | `pg_dump -Fc` → COS 中转 → 导入 |
| 数据卷 | volume `sub2api_sub2api_data` → `/app/data` | 待实测（上线前补） | tar → COS 中转 |
| 缓存 | 容器 `sub2api-redis`（redis:8-alpine） | 纯缓存 | 新机跑空实例 |
| compose | `/home/ubuntu/sub2api/docker-compose.yml` | 小 | 复制（端口绑定收紧要改，见 §3） |
| 自定义构建源 | `/home/ubuntu/sub2api-custom-build`（59 MB）+ `sub2api-custom-build.tar.gz`（12 MB） | ~70 MB | tar → COS（便于以后重新构建） |
| 运行时 env | `JWT_SECRET`、`TOTP_ENCRYPTION_KEY`、`GEMINI_*_OAUTH_CLIENT_SECRET`、`ANTIGRAVITY_*`、`ADMIN_*`、`DATABASE_*`、`REDIS_*` 等约 60 个 | 小 | 随 compose/`.env` 整体 scp；含密钥，不打印、不进聊天 |
| nginx 配置 | `/etc/nginx/sites-available/sub2api.conf`（listen 8581 → 127.0.0.1:8580） | 小 | 复制 |

**不迁**（老机继续承载）：dcp 全套（server/mysql/emqx/redis/turn/gateway）、license-server、arcreel、crmeb/niushop/yshop/postal/trace/tuoluojiang 等站点、`/home/ubuntu/sub2api-backups`（387 MB）、`vpn-deploy`（252 MB）、`new-api/logs`（219 MB 历史日志）。

**传输量合计约 1.4 GB**（含两个镜像 366 MB；不含不迁项）。老机与 COS 均走内网（实测两侧 `cos.ap-guangzhou.myqcloud.com` 均解析到 `169.254.0.47`，非公网），不受老机 2.6 Mbps 出口限制。

## 2. 域名与切流策略

- 老机继续独占 `api.heibaidao.cn`，**DNS 不动，零切流风险**。
- 新机使用新子域名（建议 **`api2.heibaidao.cn`**），DNSPod 加 A 记录 → 114.132.252.150，certbot 新签证书。
- 新业务客户端、令牌直接配 `api2.heibaidao.cn`；老机业务、支付回调、ArcReel/Worker 链路零改动。
- 回滚 = 删 `api2` A 记录。
- **库内 3 个绝对地址必须改**（已实测）：`options.ServerAddress` = `https://api.heibaidao.cn`、`AlipayNotifyURL` = `.../api/user/alipay/notify`、`AlipayReturnURL` = `.../api/user/alipay/return`。其余 OAuth 回调如有绑定同一域名，一并排查。**不改则新机生成的支付回调与绝对链接仍指向老机**。
- `options.TencentMPSSetting` 内含 COS/MPS 的 secret_id/secret_key（明文存储），随库迁移；不打印、不截图。

## 3. 新机架构

```
客户端(新业务) → api2.heibaidao.cn → nginx(新机, certbot 新证书)
  ├─ API / 页面        → new-api 容器(127.0.0.1:3000，compose 限内存 512M)
  │                        ├─ postgres 容器 (postgres:15, 库 new-api, volume)
  │                        └─ redis 容器 (redis:7, volume)
  ├─ 账号池上游        → sub2api 容器(宿主机 8080)
  │                        ├─ sub2api-postgres (postgres:18)
  │                        └─ sub2api-redis (redis:8)
  └─ 视频 /content 取片
       ├─ cos-proxy://  → 302 → COS 预签名直链（一期）
       │                       → 二期换成 cdn 域名（CDN → COS）
       └─ 其他 URL      → 本机代理回源（保留 Range 206 续传逻辑兜底）
```

**拓扑复刻的两个硬约束（必须显式配置，不能靠自动分配）**：

1. **compose 网络必须固定 `172.18.0.0/16`**。渠道 ch13/ch29 的 `base_url` 是 `http://172.18.0.1:8080`——`172.18.0.1` 是 `new-api_new-api-network` 的网关，不是默认 bridge（默认 bridge 在新机是 `172.17.0.1`）。若新机 compose 网络自动分配到 172.19/172.20，这两个渠道会全部连不上。写法：
   ```yaml
   networks:
     new-api-network:
       ipam:
         config:
           - subnet: 172.18.0.0/16
   ```
2. **sub2api 的 8080 绑定要收紧**。老机是 `0.0.0.0:8080`（公网可达）；新机建议绑 `172.18.0.1:8080:8080`，既让 new-api 容器经网关访问，又不暴露公网。若保留 `0.0.0.0`，必须用安全组/防火墙限制来源。

其他复刻细节：new-api 容器 `user: "1000:1001"`、`command: --log-dir /app/logs`、healthcheck `wget /api/status`、`depends_on postgres healthy`、`restart: always`、`TZ`、`ERROR_LOG_ENABLED`、`BATCH_UPDATE_ENABLED`、`MEMORY_CACHE_ENABLED`、`NODE_NAME` 全部照搬，仅 `NODE_NAME` 改 `new-api-node-2`。新机需确认 gid `1001` 存在（或调整挂载目录属组），否则 `data/`、`logs/` 写入失败。

## 4. CDN / COS 分期

**一期（随迁移上线）——无损转存**

- **已建桶（2026-11-12）**：`hbd-video-1256715892`，广州 `ap-guangzhou`，ACL 私有，生命周期 30 天（规则 `expire-30d`）。
  - 本账号 APPID = **`1256715892`**（`cam:GetUserAppId` 实查，主账号 UIN `100005629999`）。
  - ⚠️ 老机 options `TencentMPSSetting` 里的桶是 `mps-test-1397292858`，**属于另一个腾讯云账号**（本账号 `head_bucket` 返回 403），其 secret 不能用于新桶；新桶单独用 `hgx001` 的密钥。
  - 密钥已写入新机 `/root/.cos.env`（`600`，含 `COS_BUCKET`/`COS_REGION`/`COS_ENDPOINT`），不入库、不进聊天。
  - 已验证：新机 `cos.ap-guangzhou.myqcloud.com` 解析 `169.254.0.47`（内网），预签名 `PUT 200` / `GET 200` / `Range 206`。
- 任务成功后：new-api 从上游拉片 → 同地域内网 PUT 到 COS → 库里写 `cos-proxy://` 占位 → 客户取片 302 COS 预签名直链。
- **直传直存，不经 MPS 转码**——避免此前实测的 1.8 Mbps 重编码画质损失，产物与上游完全一致。
- 实现路径：复用 `relay/channel/task/wan3/relocate.go` 的 `cos-proxy://` 协议与 `controller/video_proxy.go` 的 302 分支，为有赞 wan3 渠道新增 postprocess adaptor（TDD）。
- 未配置 COS、转存失败、超时 → 自动回落本机代理，行为与老机一致。

**二期（一期验证稳定后）——CDN 加速**

- COS 绑 CDN 加速域名（如 `cdn.heibaidao.cn`）+ 鉴权 URL（TypeA/时间戳签名）。
- 302 目标从 COS 预签名换成 CDN 鉴权 URL，CDN 流量单价约为 COS 外网下行一半。
- 纯控制台 + 配置扩展，代码基本不动。

## 5. 实施步骤

1. **新机基座**：swap 4G、Docker 已就绪；**待补** nginx、certbot（实测 nginx 未装，80/443 空闲）。
2. **前置条件（已完成 2026-11-12）**：`hgx001` 的 API 密钥已创建（已关联 `AdministratorAccess`），COS 桶 `hbd-video-1256715892` 已建，密钥落在新机 `/root/.cos.env`。**该密钥曾在聊天中明文出现，迁移完成后应轮换。**
3. **打包上传（老机）**：
   - `pg_dump -Fc` 两库（89 MB / 553 MB）
   - `docker save` 两镜像（231 MB / 135 MB）
   - tar：new-api 当前 release + classic、`/home/ubuntu/new-api/{docker-compose.yml,.env,data}`、`new-api-src`（剔 node_modules）、`sub2api` 目录 + custom-build、`sub2api_sub2api_data` volume、两个 nginx conf
   - 全部经 COS 内网传到中转前缀（如 `migration-20261112/`），完成后删除
4. **新机还原**：COS 内网拉取 → `pg_restore`、`docker load`、按 §3 起容器（含固定 subnet、8080 绑定收紧）→ 重建前端软链 → `logs/` 建空目录并处理 gid 归属
5. **配置改写**：§2 的三项 options 改为 `api2.heibaidao.cn`；`NODE_NAME` 改 `new-api-node-2`
6. **健康验收**（§8 清单）
7. **DNS**：DNSPod 加 `api2` A 记录 → certbot 签证书 → nginx 挂 443（配置保留老机超时参数）
8. **一期 COS 链路**：新桶 + 密钥 → 有赞 wan3 转存 adaptor（TDD）→ 真机 1 条验证（费用另授权）
9. **发布流程适配**：`deploy-backend.sh` 增加 `DEPLOY_*` 目标参数（`deploy-frontend.sh` 已支持），双机独立发版；新机连不上 github，沿用 bundle 经 ssh 管道直传流程，传完 `wc -c` 验字节数
10. **收尾**：改新机 root 密码、关闭密码登录只留密钥；清理 COS 中转前缀

## 6. 数据一致性（双机并行期）

- 快照时间点之后，老机上的渠道/价格/options 变更**不会自动同步**到新机。
- 约束：迁移上线后配置变更要么冻结、要么两边手动同步；新业务数据（用户、订单、日志）只存在于新机。
- sub2api 尤其注意：GPT-Pool 账号 OAuth 状态在 sub2api 库内，**老机刷新的 token 不会同步到新机副本**。双机并行期若 ch29 在新机报 `Token revoked`/`No available compatible accounts`，需在新机 sub2api 管理端单独处理账号（禁止自动触发 OAuth 恢复，先报用户）。

## 7. 渠道核对清单（新机起容器后逐条过）

| 渠道 | 老机 base_url（实测） | 新机处置 |
|---|---|---|
| ch13 / ch29 GPT-Pool（type 1） | `http://172.18.0.1:8080` | 依赖 §3 固定 subnet；起容器后 `curl` 从 new-api 容器内验证 8080 可达；验证 group 4 账号可用 |
| ch17 有赞 Wan3（type 63） | `https://ai.youzan666.vip` | 不变，验证连通 |
| ch20 / ch30 漫屋ArcReel（type 66） | `https://arcreel.heibaidao.cn` | ArcReel 不迁，验证新机到 ArcReel 出网与回调 |
| ch25 心笔 Model Router（type 68） | `https://www.xinbiapi.com` | 不变，验证连通 |
| ch28 SeetaCloud（type 70） | `https://u1133789-a936-c07acd4f.westd.seetacloud.com:8443` | 不变 |
| 其他文本渠道 | 各上游 | 逐条验证 base_url 可达 |

## 8. 验收清单

- [ ] `/api/status`：success + setup，`SQL_DSN` 指向新库，后端 postgres
- [ ] 管理员登录、模型目录/渠道卡片/定价可见
- [ ] 客户令牌调用（测试令牌，非生产计费渠道）返回正常
- [ ] sub2api 管理端可登录；ch13/ch29 从容器内经 `172.18.0.1:8080` 通
- [ ] 任务提交 → 轮询 → 取片：`/content` 200 全量 + `Range: bytes=N-` 206
- [ ] 长任务接口经 nginx 不 504（`proxy_read_timeout 18000s` 生效）
- [ ] 一期 COS：新完成任务写 `cos-proxy://`、302 直链可下载、失败自动回落代理
- [ ] `api2` HTTPS 证书有效，`nginx -t` 通过
- [ ] 发布流程：bundle 直传 + 远程构建 + 健康检查在新机走通一次
- [ ] 老机所有服务原样运行（对照迁移前容器/站点清单确认零改动）

## 9. 风险与回滚

| 风险 | 对策 |
|---|---|
| 渠道 `172.18.0.1` 失效 | compose 显式固定 `172.18.0.0/16`；验收时容器内 curl 双确认 |
| sub2api 8080 公网暴露 | 新机绑 `172.18.0.1:8080` 或安全组限制（老机为 `0.0.0.0`，属既有隐患，不在本次改动） |
| 双库数据分叉 | 新域名只接新业务；配置冻结或双写；老机退役时做最终同步 |
| sub2api OAuth 账号状态漂移 | 各自管理；新机账号异常先报用户，不自动恢复 |
| COS 转存失败 | 自动回落本机代理，任务不失败 |
| 新机 2C4G 内存紧（老机 4C4G，swap 已用 1.6G） | 已配 4G swap、swappiness=10；new-api 容器沿用 512M 限制；不在新机跑前端构建 |
| 2 核构建慢/OOM | 后端 `docker build` 可接受（配 swap）；前端构建不在服务器做，产物 tar 发布 |
| 回滚 | 删 `api2` A 记录即回滚；老机全程未动 |
| 密钥安全 | `.env`、两份 compose、密钥文件一律 scp/COS，不打印不截图；完成后轮换 root 密码、禁密码登录 |

## 10. 待用户确认的决策点

1. 新域名：`api2.heibaidao.cn`？
2. 一期 COS 真机验证条（约 1 条视频生成费用）是否授权？
3. DB 快照时间点：现在拍，还是域名确定后拍？
4. sub2api 是否需要独立公网入口（新子域名 + nginx 8581），还是只走 docker 网关/安全组白名单？

## 11. 审查记录（2026-11-12 复核，已修正初稿 5 处偏差）

| # | 初稿写法 | 实测事实 | 处置 |
|---|---|---|---|
| 1 | 前端产物 309 MB，复制 `dist` | `dist` 是软链 → `releases/20261112T034413Z-7a32c6e1421f`；当前 release 52 MB，目录合计 266 MB 含历史 release | 只迁当前 release + classic，重建软链 |
| 2 | 未列应用编排目录 | `/home/ubuntu/new-api/` 含 compose、`.env`(463 B)、`data/`(1.5 MB)、`logs/`(219 MB)、`vpn-deploy`(252 MB) | 迁 compose+env+data；logs/vpn-deploy 不迁 |
| 3 | sub2api 镜像记为 hash `6b37380f49ea` | 实际 tag `sub2api:0.2.3-codex-image`（容器引用）与 `0.2.3-codex-image-v6`（tag）同指 ID `6b37380f49ea` | 按 ID `6b37380f49ea` save，load 后补两个 tag |
| 4 | 「拓扑天然复刻」，未提网段 | 老机默认 bridge 是 172.17.0.1，`172.18.0.1` 属 compose 网络；新机自动分配可能变成 172.19 | compose 显式固定 subnet（已实施） |
| 5 | 未列域名绑定配置 | options 实测含 `ServerAddress`、`AlipayNotifyURL`、`AlipayReturnURL` 三项指向 api.heibaidao.cn，另 `TencentMPSSetting` 含明文密钥 | 已改写为 api2（备份表 `bak_20261112_options_api2`） |

## 12. 实施完成记录（2026-11-12）

| 项 | 结果 |
|---|---|
| 传输 | 老机 → COS 内网（前缀 `migration-20261112/`）→ 新机，10 文件字节校验一致；src tar 首传截断（上传早于打包完成），重传修复 |
| 容器 | new-api / postgres / redis / sub2api / sub2api-postgres / sub2api-redis 全部 healthy |
| 网络 | compose 固定 `172.18.0.0/16`；sub2api 8080 只绑 `172.18.0.1`（老机为 0.0.0.0 公网） |
| DNS/证书 | `api2.heibaidao.cn` A 记录已建（域名属另一账号 UIN `100046072329`，用其密钥调 DNSPod API 创建）；Let's Encrypt 证书 2027-02-10 到期，certbot 自动续期；轻量防火墙经 Lighthouse API（版本 `2020-03-24`）放行 443 |
| options | `ServerAddress`/`AlipayNotifyURL`/`AlipayReturnURL` → api2；原值备份 `bak_20261112_options_api2` |
| .env | CRLF→LF（老机容器内实测密钥不含 `\r`，转换安全）；`NODE_NAME=new-api-node-2` |
| 验收 | `/api/status` success=True；令牌+路由管道通（`model_not_found` 分发器响应而非 401）；`/v1/models` 正常；nginx 超时契约 100M/600s/18000s/300s 原样；老机零改动 |
| 遗留 | ① `sub2api.conf` 的 8581→127.0.0.1:8580 站点 502（老机遗留 8580 服务未迁，不在范围，sub2api 实际走 `172.18.0.1:8080`）；② new-api `logs/` 需 chown 1000:1001（compose 启动会重建目录）；③ §5 步骤 8/9 未做 |
| 清理 | COS 中转前缀已删；老机打包目录已删；新机打包目录已删 |

**已验证成立的关键假设**：

- 老机与新机解析 `cos.ap-guangzhou.myqcloud.com` 均得到 `169.254.0.47`（腾讯云内网服务地址），两侧都走内网，COS 中转方案成立。
- 新机 80/443 空闲、nginx 未装，直接装即可，无端口冲突。
- 老机 `.env` 仅 463 B，可直接 scp，无需走 COS。
- `vpn-deploy` 无运行中的 systemd/docker 服务，确认可安全不迁。
