# 新服务器迁移设计方案（new-api + sub2api）

> 2026-11-12 起草，同日按老机实际部署逐项复核并修正（见 §11 审查记录）；当日晚间按新机实测再次复核回写（见 §13）。
> **当前状态（2026-11-12 晚间复核）**：§5 步骤 1-9 的代码与流程均已落地，新机已独立发版 3 次；步骤 10 的安全收尾（关闭密码登录、root 仅密钥、轮换 root 密码）已完成。`https://api2.heibaidao.cn` 可用（证书 2027-02-10 到期，certbot 自动续期）。§8 验收清单 10 项中 **4 项实测通过**，4 项仅完成配置层/代码层/数据层确认，2 项按约束未跑真实任务（详见 §13.2）。
> **仍未完成**：一期 COS 真机 1 条验证（需单独授权，会产生 1 条视频费用）、`sub2api.conf` 8581 站点 502（老机遗留，不在范围）、双机代码同步（§13.5）。COS API 密钥轮换已于 2026-11-12 20:22 完成（§13.6）。
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

> 注意：以上 healthcheck / depends_on 是 **compose 文件里的声明**。实际运行容器由 `docker run` 启动，不受 compose 编排管辖，详见 §12、§13.4。

## 4. CDN / COS 分期

**一期（随迁移上线）——无损转存**

- **已建桶（2026-11-12）**：`hbd-video-1256715892`，广州 `ap-guangzhou`，ACL 私有，生命周期 30 天（规则 `expire-30d`）。
  - 本账号 APPID = **`1256715892`**（`cam:GetUserAppId` 实查，主账号 UIN `100005629999`）。
  - ⚠️ 老机 options `TencentMPSSetting` 里的桶是 `mps-test-1397292858`，**属于另一个腾讯云账号**（本账号 `head_bucket` 返回 403），其 secret 不能用于新桶；新桶单独用 `hgx001` 的密钥。
  - 密钥已写入新机 `/root/.cos.env`（`600`，含 `TENCENT_SECRET_ID` / `TENCENT_SECRET_KEY` / `TENCENT_APPID` / `COS_BUCKET` / `COS_REGION` / `COS_ENDPOINT`），不入库、不进聊天。
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

1. **新机基座（已完成）**：swap 4G、Docker、nginx、certbot 均已就绪；`api2` 443 已签发证书并挂载，`sudo nginx -t` 通过（§13.1）。起草时记录的「nginx 未装、待补」是初始状态，已过期。
2. **前置条件（已完成 2026-11-12）**：`hgx001` 的 API 密钥已创建（已关联 `AdministratorAccess`），COS 桶 `hbd-video-1256715892` 已建，密钥落在新机 `/root/.cos.env`。**该密钥曾在聊天中明文出现，已于 2026-11-12 20:22 轮换、旧密钥置 `Inactive`（§13.6）。** **轮换状态：已完成（2026-11-12 20:22，见 §13.6）**，旧密钥已置 `Inactive`，新密钥已生效。凭据位置索引见 `AGENTS.md` 「新生产实例」速查表（第 95/99 行）：密钥源 `/root/.cos.env`、下发脚本 `/root/cosdirect-provision.sh`，仓库与聊天里不存明文；轮换只需同步 2 处。
3. **打包上传（老机）**：
   - `pg_dump -Fc` 两库（89 MB / 553 MB）
   - `docker save` 两镜像（231 MB / 135 MB）
   - tar：new-api 当前 release + classic、`/home/ubuntu/new-api/{docker-compose.yml,.env,data}`、`new-api-src`（剔 node_modules）、`sub2api` 目录 + custom-build、`sub2api_sub2api_data` volume、两个 nginx conf
   - 全部经 COS 内网传到中转前缀（如 `migration-20261112/`），完成后删除
4. **新机还原**：COS 内网拉取 → `pg_restore`、`docker load`、按 §3 起容器（含固定 subnet、8080 绑定收紧）→ 重建前端软链 → `logs/` 建空目录并处理 gid 归属
5. **配置改写**：§2 的三项 options 改为 `api2.heibaidao.cn`；`NODE_NAME` 改 `new-api-node-2`
6. **健康验收**（§8 清单，实测结果见 §13.2）
7. **DNS**：DNSPod 加 `api2` A 记录 → certbot 签证书 → nginx 挂 443（配置保留老机超时参数）
8. **一期 COS 链路（代码已完成，真机未验证）**：`cos_direct.go` / `cos_presign.go` / `media_relocate.go` + 单测 + 设置项已落地，新机为修复 PUT 拒收连续发版 3 次；**真机 1 条验证未做**（需单独授权，1 条视频费用）。截至 §13 复核，库内无 `cos-proxy://` 产物、近 8 小时日志 0 条 COS 命中。
9. **发布流程适配（已完成并在新机走通）**：新机连不上 github，采用独立脚本 `/home/ubuntu/deploy-new-api.sh` 走 bundle 直传 + 远程构建 + 健康检查；已留 3 份 `/tmp/backend-deploy-*.log`，最新一次 `FINAL_SHA: 78dc7b851` + `HEALTH_OK`。起草时设想的「给 `deploy-backend.sh` 加 `DEPLOY_*` 目标参数」按新机独立脚本方案替代，老机发布流程未改动。
10. **收尾（已完成，2026-11-12 19:36）**：COS 中转前缀已清理；新机 root 密码已轮换（新密码存 `/root/.root-password-20261112`，权限 `600`，不进聊天），密码登录已关闭、`PermitRootLogin prohibit-password`，改前配置备份在 `/root/ssh-hardening-backup-20261112-1935/`。注意：COS API 密钥轮换在同日晚些时候（20:22）单独完成，见 §13.6。

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

> §13 复核：11 个渠道的 base_url 与表中一致，ch13 / ch29 的 `172.18.0.1:8080` 已从 new-api 容器内实测可达、宿主机 8080 返回 200；其余上游按「不主动发探针」约束未逐一验证。

## 8. 验收清单（2026-11-12 晚间复核状态）

- [x] `/api/status`：success + setup，`SQL_DSN` 指向新库，后端 postgres —— 实测 `success=true / setup=true`，DSN host=`postgres:5432`
- [ ] 管理员登录、模型目录/渠道卡片/定价可见 —— 数据侧已核（63 用户 / 11 渠道 / 33 abilities / 107 令牌 / 42 options，`/v1/models` 正常）；**管理员登录未测**（无凭据，不擅自使用）
- [ ] 客户令牌调用（测试令牌，非生产计费渠道）返回正常 —— 未测，会触达计费链路
- [ ] sub2api 管理端可登录；ch13/ch29 从容器内经 `172.18.0.1:8080` 通 —— 后半段已实测通过（容器内 wget 通、宿主机 8080 返回 200）；管理端登录未测，故整项不勾
- [ ] 任务提交 → 轮询 → 取片：`/content` 200 全量 + `Range: bytes=N-` 206 —— **按用户要求未跑视频任务**；代码侧 `f979e6796` Range 断点续传已落地
- [ ] 长任务接口经 nginx 不 504（`proxy_read_timeout 18000s` 生效）—— 配置层已核（`100M / 600s / 18000s / 300s` 原样、`nginx -t` 通过），实际长任务未测
- [ ] 一期 COS：新完成任务写 `cos-proxy://`、302 直链可下载、失败自动回落代理 —— 代码与 3 次发版齐，但库内无产物、日志无命中，**真机未验证**（需单独授权 1 条视频）
- [x] `api2` HTTPS 证书有效，`nginx -t` 通过 —— Let's Encrypt 2026-11-12 → 2027-02-10，`ssl_verify=0`，公网 HTTPS 200
- [x] 发布流程：bundle 直传 + 远程构建 + 健康检查在新机走通一次 —— 实测 3 份部署日志，最新 `FINAL_SHA: 78dc7b851` + `HEALTH_OK`
- [x] 老机所有服务原样运行（对照迁移前容器/站点清单确认零改动）—— 13 个容器含 dcp 全套、license-server 全在

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
| SSH 加固把人锁在门外 | 改前备份、`sshd -t` 校验、reload 不断现有连接、180s 自动回滚守护、新连接验证后才取消（§13.3） |
| 回滚 | 删 `api2` A 记录即回滚；老机全程未动 |
| 密钥安全 | `.env`、两份 compose、密钥文件一律 scp/COS，不打印不截图；root 密码已轮换、密码登录已禁、COS API 密钥已轮换（旧密钥 `Inactive`，§13.6） |

## 10. 待用户确认的决策点

> 状态（§13 复核）：1 已按 `api2.heibaidao.cn` 落地；2 未授权（真机验证仍未做）；3 快照已拍；4 8581 站点仍 502，当前实际走 `172.18.0.1:8080`，独立公网入口未启用。

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

## 12. 实施完成记录（2026-11-12，§13 已更正）

| 项 | 结果 |
|---|---|
| 传输 | 老机 → COS 内网（前缀 `migration-20261112/`）→ 新机，10 文件字节校验一致；src tar 首传截断（上传早于打包完成），重传修复 |
| 容器 | 6 个容器均在运行。原写「全部 healthy」不成立：compose 第 37 行给 new-api 配了 healthcheck，但运行容器由 `docker run` 启动（无 compose 标签），`docker inspect` 返回 `NO_HEALTHCHECK`，healthcheck / depends_on 对运行容器不生效；老机同样如此（§13.4） |
| 网络 | compose 固定 `172.18.0.0/16`；sub2api 8080 只绑 `172.18.0.1`（老机为 0.0.0.0 公网） |
| DNS/证书 | `api2.heibaidao.cn` A 记录已建（域名属另一账号 UIN `100046072329`，用其密钥调 DNSPod API 创建）；Let's Encrypt 证书 2027-02-10 到期，certbot 自动续期；轻量防火墙经 Lighthouse API（版本 `2020-03-24`）放行 443 |
| options | `ServerAddress`/`AlipayNotifyURL`/`AlipayReturnURL` → api2；原值备份 `bak_20261112_options_api2`；§13 复核 options 中无残留 `api.heibaidao.cn` |
| .env | CRLF→LF（老机容器内实测密钥不含 `\r`，转换安全）；`NODE_NAME=new-api-node-2` |
| 验收 | `/api/status` success=True；令牌+路由管道通（`model_not_found` 分发器响应而非 401）；`/v1/models` 正常；nginx 超时契约 100M/600s/18000s/300s 原样；老机零改动。§8 清单逐项实测结果见 §13.2 |
| 遗留（§13 复核后） | ① `sub2api.conf` 8581 站点仍 502（老机遗留 8580 服务未迁，不在范围，sub2api 实际走 `172.18.0.1:8080`）；② `logs/` 属主实测已是 `1000:1001`，**已解决**；③ 步骤 8/9 的代码与流程已完成，仅剩 COS 真机 1 条验证；④ COS API 密钥已于 20:22 轮换完成（§13.6）；⑤ 新增：双机/本地代码已分叉（§13.5） |
| 清理 | COS 中转前缀已删；老机打包目录已删；新机打包目录已删 |
| 安全收尾（§13 补做） | 关闭密码登录、root 改为仅密钥、root 密码已轮换；改前配置备份 `/root/ssh-hardening-backup-20261112-1935/`；负向验证返回 `Permission denied (publickey)`。COS API 密钥已于 20:22 轮换完成，旧密钥置 `Inactive`（§13.6） |

**已验证成立的关键假设**：

- 老机与新机解析 `cos.ap-guangzhou.myqcloud.com` 均得到 `169.254.0.47`（腾讯云内网服务地址），两侧都走内网，COS 中转方案成立。
- 新机 80/443 空闲、nginx 未装（起草时），直接装即可，无端口冲突。
- 老机 `.env` 仅 463 B，可直接 scp，无需走 COS。
- `vpn-deploy` 无运行中的 systemd/docker 服务，确认可安全不迁。

## 13. 复核记录（2026-11-12 晚，只读实测 + 安全收尾 + 密钥轮换）

**范围与约束**：连接两台服务器做只读核实；按用户要求**不提交任何视频任务、不调用视频上游、不做生成/计费探针**（仅对自家 sub2api 打过一次根路径 HTTP）；全程不打印密钥与 DSN。写操作仅两处：新机 SSH 安全收尾（§13.3）与 COS API 密钥轮换（§13.6）。COS 轮换阶段只做了对象存储权限探针（PUT/GET/Range 后立即删除），**未提交任何视频任务**。

### 13.1 基础实测

| 项 | 结果 |
|---|---|
| `/api/status` | `success=true` / `setup=true`，后端 PostgreSQL，DSN host=`postgres:5432` |
| 数据 | 63 用户 / 11 渠道 / 33 abilities / 107 令牌 / 42 options |
| nginx | `sites-enabled` 只挂 api2 / default / sub2api（老域名 conf 未启用，无冲突）；`100M / 600s / 18000s / 300s` 超时契约原样；`sudo nginx -t` 通过 |
| 证书 | Let's Encrypt，2026-11-12 → 2027-02-10，`ssl_verify=0`，公网 HTTPS 200 |
| 域名绑定 | 三项 options 均指向 api2，无残留 `api.heibaidao.cn`，备份表 `bak_20261112_options_api2` 存在 |
| 网络 / 容器 | subnet `172.18.0.0/16`；`NODE_NAME=new-api-node-2`；new-api 容器内 `wget 172.18.0.1:8080` 通、宿主机 8080 返回 200 |
| 前端 | 软链指向 `20261112T034413Z-7a32c6e1421f` |
| 发布 | 3 份 `/tmp/backend-deploy-*.log`，最新 `FINAL_SHA: 78dc7b851` + `HEALTH_OK`；新机源码 HEAD = `78dc7b851`（复核时与本地一致） |
| 老机 | 13 个容器含 dcp 全套、license-server 全在，零改动 |

### 13.2 §8 验收清单判定

10 项中 **4 项实测通过**（1 / 8 / 9 / 10）、**4 项仅配置层或代码层确认**（2 / 4 / 6 / 7）、**2 项按约束未跑**（3 / 5）。逐项原因已写在 §8 每条之后。原文档「§8 验收清单已核」与 10 个空 checkbox 自相矛盾，本次已改为按实测勾选并逐条备注。

### 13.3 本轮安全收尾（已完成 19:36）

| 动作 | 结果 |
|---|---|
| 改前状态 | `sshd_config` 第 33 行 `PermitRootLogin yes`、第 123 行 `PasswordAuthentication yes`；drop-in `50-cloud-init.conf` 里还有一条 `PasswordAuthentication yes`（因 `Include` 在第 12 行，drop-in 优先） |
| 备份 | `/root/ssh-hardening-backup-20261112-1935/`（`sshd_config` + `50-cloud-init.conf`） |
| 改动 | 三处 `yes` 改为 `PasswordAuthentication no` / `PermitRootLogin prohibit-password`；`KbdInteractiveAuthentication` 原本已是 `no` |
| 校验 | `sudo sshd -t` 通过；生效值 `passwordauthentication no` / `permitrootlogin without-password` / `pubkeyauthentication yes` |
| 防锁死 | 先挂 180 秒自动回滚 timer，再 `systemctl reload ssh`（不重启、不断现有连接）；新连接验证成功后立即取消 timer |
| 验证 | 密钥登录连续成功；纯密码方式连接返回 `Permission denied (publickey)`（服务端已不提供密码认证） |
| root 密码 | 已用随机强密码轮换，**密码只写在新机 `/root/.root-password-20261112`（`600`，root:root），不进聊天**。建议取走后删除该文件 |

回滚：把备份目录里两个文件拷回原路径并 `sudo systemctl reload ssh` 即可。

### 13.4 「全部 healthy」更正

compose 第 37 行确实给 new-api 配了 healthcheck，但实际运行容器是 `docker run` 起的（无 compose 标签），`docker inspect` 返回 `NO_HEALTHCHECK`；部署脚本 `deploy-new-api.sh` 同样走 `docker run`，所以 compose 的 healthcheck / depends_on 对运行容器都不生效。老机也是同样方式，属既有部署习惯，不是新机独有故障，但文档原写法会误导后续排障。若要让健康检查真正生效，需把容器改由 `docker compose up -d` 托管或重建容器时带上 `--health-cmd` 等参数——**本轮未改**。

### 13.5 双机 / 本地代码分叉

复核时：新机 `78dc7b851`，老机 `dcc5bf676`，后者是新机 HEAD 的**祖先**，新机领先老机 7 个提交（含 COS 直存、Range 断点续传、迁移文档、seedance 台账）。本地开发线随后继续推进到 `070402afc`，已领先新机。§6 警告的分叉已实际发生，方向是「本地 > 新机 > 老机」。若这些能力要覆盖老机业务，需按各自发布流程单独同步；本轮未做。

### 13.6 COS API 密钥轮换（已完成 2026-11-12 20:22）

**凭据位置**：`AGENTS.md` 「新生产实例」速查表第 95/99 行记录密钥源与下发脚本，仓库内**没有明文 key**（按全局约束也不应写入）。密钥源 `/root/.cos.env`（`600`），另经 `/root/cosdirect-provision.sh` 明文下发一份到 `options.CosDirectSetting`，**两处同源、须成对轮换**。

`options.TencentMPSSetting` 虽也有 `secret_id` / `secret_key`，但实测与 `/root/.cos.env` **不是同一把**（老 MPS 账号、桶 `mps-test-1397292858` 属另一腾讯云账号），轮换时不得改动；已用指纹前后比对确认 `MPS_UNTOUCHED`。老机没有 `/root/.cos.env`、也没有 `CosDirectSetting`，只有另一账号的 `TencentMPSSetting`，故本次禁用对老业务零影响（已实测健康、无 `AccessDenied`）。

**实施步骤**（用户授权「全套轮换」后执行）：

1. 服务器上无 `tccli` / `tencentcloud` SDK，故用 Python 标准库手写 TC3-HMAC-SHA256 签名调 CAM API；脚本 `/root/cos-key-rotation-20261112/cam_rotate.py`（`700`），全程不打印密钥值。
2. `ListAccessKeys` 盘点：轮换前 `hgx001` 只有 1 个 `Active` 密钥（即 cos.env 那把）。备份 `cos.env` 与 DB 原值到 `/root/cos-key-rotation-20261112/`（全 `600`）。
3. `CreateAccessKey`（`Description=rotated-20261112-agent`）→ 新密钥立即落盘备份 → 原子替换 `/root/.cos.env` 两行 → 跑 `sudo bash /root/cosdirect-provision.sh` 同步 `CosDirectSetting`。
4. 验证：新密钥 `ListObjects` 通过；`PUT` / `GET` / `Range` 探针通过（14 B 全量、4 B 分段，探针对象随即删除）；cos.env 与 DB 密钥指纹一致；`TencentMPSSetting` 未变；应用 `/api/status` 正常。
5. 应用配置由 `model.SyncOptions(common.SyncFrequency)` 定时从 DB 重载，默认 60s。切换发生在 20:22:02，日志确认 20:22:46 / 20:23:46 两次同步完成，**进程内存已换成新密钥**后才执行下一步。
6. `UpdateAccessKey` 把旧密钥置 `Inactive`（而非删除，**可逆**）。回归：旧密钥 `ListObjects` 返回 `CosServiceError`（已拒绝）；新密钥仍通过 `ListObjects` 与 PUT/GET/Range；应用健康、公网 HTTPS 200、近 10 分钟无 COS 鉴权错误。

**回滚**：`sudo python3 /root/cos-key-rotation-20261112/cam_rotate.py enable-old` 可重新激活旧密钥；如需整体回退，用同目录 `cos.env.before` / `cosdirect.before.json` 恢复两处配置并重跑下发脚本。该目录含旧密钥与当前新密钥的 600 权限副本，**确认稳定后建议清理 `new_key.env`**。

> 说明：本次只完成密钥轮换，一期 COS 链路的**真机 1 条视频验证**仍需单独授权（§13.7）。`UpdateAccessKey` 置 `Inactive` 本身可逆，且 COS 转存设计为失败自动回落本机代理，不会让任务失败。

### 13.7 仍需用户拍板

1. 一期 COS 真机 1 条验证 + 取片 206 端到端（需单独授权，会花 1 条视频费用）——密钥轮换已完成，此项仍待授权
2. 是否给老机补发领先的提交（§13.5）
3. 是否把 sub2api 8581 站点 502 纳入修复范围
4. 是否把运行容器改为 compose 托管以让 healthcheck 生效（§13.4）
5. 确认轮换稳定后清理 `/root/cos-key-rotation-20261112/new_key.env`（当前有效密钥的副本），并取走 `/root/.root-password-20261112` 后删除该文件
