# new-api 项目约束

DO NOT send optional commentary

Go/Gin/GORM API网关，前端为 `web/default/`（React/TypeScript/Rsbuild）。根文件只放全局硬约束和按需索引；渠道、定价、支付、运维契约分到 `docs/agent-guides/`，命中主题后必读，不全量加载。

## 费用与生产安全

- **未经用户明确允许，禁止任何真实视频生成测试**，包括连通性/计费探针、真实e2e或重复采样。默认用mock单测、历史产物、日志/DB。
- 获准真测也须明确次数/费用，默认最多1条；重跑/扩量另确认。客户端退款、HTTP400/402不保证上游零成本；不能以“探针”名义绕过授权。
- **测豆包（及任何按额度计费的渠道）只要确认任务已送达该渠道即收工**：以渠道任务号/受理回执/日志改派记录为准，不必等视频生成出来。确认送达后立即在燃境 Ai App 的任务管理取消该任务（`src/components/task/TaskManagementTable.vue` → `cancelQueuedTask` → `window.electronAPI.cancelTask`），避免把当天刷新额度耗在等待产物上。
  - 取消只对**本站调度侧**生效：排队中彻底移除；已提交上游的只能释放本地槽位，**上游已受理的生成成本不可撤销**。所以「省额度」省的是**后续/重复生成**，不是已派发那一单。
  - 网关侧无视频任务取消接口（`router/video-router.go` 只有提交/查询/取片），取消只能在 App 端做；跨端验证时先确认受理再决定要不要等。
- 真实付款、改余额、退款、删数据、渠道上下架/调价、发版均须明确范围并最小操作；不能顺手改同渠道其它公开模型。
- 保留工作区他人改动、未跟踪文件与当前分支；不擅自切分支、清理、改Git身份、改写历史或把WIP带上生产。
- 不输出/提交API Key、OAuth票据、密码、私钥、完整env/DSN或含凭据的探针结果。只读排查不自动触发OAuth恢复/真实调用。
- 官网/上游已受理不能因客户端超时就盲重提；交付、计费、生成状态分别验证，防重复扣费和误退款。

## 三项目关系链路

三仓均属本团队，“漫屋”就是ArcReel，不是另一个第三方供应商。

| 项目 | 位置 | 职责 |
|---|---|---|
| 燃境 Ai App | `C:/work/即梦网站/cron_video_brach` | Electron Worker，执行官网 |
| ArcReel（漫屋） | `C:/work/即梦网站/ArcReel` | 建单、派单、钱包、产物托管；manwu-agent为gitee镜像 |
| new-api（本仓库） | `C:/work/new-api-src`；维护目录 `C:/work/维护newapi` | 对外网关、渠道与计费 |

客户端 → new-api → ArcReel → Worker WSS → 官网 → 托管产物 → 网关交付。

- 改链路、模型公开名、参数/参考图覆盖度需同步三端实现及各自说明，不单改一个白名单。
- 开发与生产的token/地址配对；客户端令牌、OpenAPI service token、Worker token不可互用。
- 供应商联调可能还经自家sub2api；按请求ID、模型、时间联合取证，不以自动选渠道/降级成功证明目标渠道可用。
- 完整协议与素材/归属边界见 [远端生成](docs/agent-guides/remote-generation.md)。

## 后端工程约束

- 业务代码JSON编解码使用 `common/json.go` 包装，不能直接调用encoding/json的marshal/unmarshal；RawMessage/Number等类型引用允许。
- 数据库同时兼容 SQLite、MySQL≥5.7.8、PostgreSQL≥9.6；优先GORM，raw SQL要处理引号/保留列名/布尔值/主库与日志库分支，不引入无回退的专用类型/函数/迁移。
- 不用数据库专属AUTO_INCREMENT/SERIAL代替GORM主键；SQLite迁移不能照搬ALTER COLUMN。业务已决定的布尔默认值不要靠可能跨库漂移的default:true标签反复触发AutoMigrate。
- 客户请求反序列化后再转上游的可选scalar用**指针+omitempty**：未传nil，显式0/false必须保留；不让JSON零值丢弃改变用户请求或计费。
- 代码直接可读、早返回，避免深层控制流；单调用者的机械helper不为缩短函数而抽象，真正领域概念/复杂可测行为/接口或回调可独立成函数。
- 新渠道核对StreamOptions并同步支持列表；未知模型/规格默认明确失败，不悄悄换档。历史兼容例外按渠道契约，不借归档改产品语义。
- 后端测试保护真实行为/协议/资金/兼容性，用确定性表测；不堆随机循环、sleep、只看日志/性能假测试或私有常量/布局断言。
- 新增/重写Go测试使用testify的require（setup/fatal）与assert（非fatal）；fixture显式初始化DB/context/group/settings/cache。清理测试保留有效回归，不为迎合错误fixture改协议。

## 前端与国际化

- 前端使用Bun；具体规则必读 [web/default/AGENTS.md](web/default/AGENTS.md)。classic不是默认工作目录。
- 用户文案使用i18next/react-i18next，English源串作flat JSON key；维护en基准、zh回退及既有fr/ru/ja/vi翻译，不硬编码UI文本。
- 后端i18n保留en/zh。显示-only修改与真实扣费分开，前端不能成为价格唯一来源。

## 自家生产实例 api.heibaidao.cn 调试速查（2026-11-07 用户确认：自家上游，不是第三方）

| 项 | 值 |
|---|---|
| 生产入口 | `https://api.heibaidao.cn`（本仓库代码的生产实例） |
| 服务器 | `ssh -p 877 ubuntu@119.29.253.97`；应用容器 `new-api`（镜像 `new-api-custom:latest`） |
| 数据库 | 容器 `postgres`（库 `new-api`，用户 root）：`sudo docker exec -i postgres psql -U root -d new-api` |
| 维护目录 | `C:/work/维护newapi` |
| 账号池上游 | GPT-Pool = 容器 `sub2api`（`172.18.0.1:8080`，库容器 `sub2api-postgres`，psql 用户/库均 `sub2api`） |

- 路由 = `channels` × `abilities`（group×model×channel_id 笛卡尔积）。**直接改 channels.models 不会同步 abilities**，须手动 INSERT abilities 行；`MEMORY_CACHE_ENABLED=true` 下 SyncChannelCache 每 60s 重建路由，改完等 60s 生效、无需重启。key 一律库内 INSERT...SELECT 复制，不走终端明文。
- **m3only 沙箱组**（视频优先级饿死图片时的服务器侧兜底路由，2026-11-07 配置）：用户 id=53 `m3only_user` / token id=166 `m3only受限令牌`（key 前缀 `FOd34WzRQY`），model_limits=`MiniMax-M3.1,Seedream 4.5,Nano Banana Pro,Seedream 5.0 Flash,gpt-image-2.5-官方`，unlimited_quota=t、cross_group_retry=f。专属通道 ch26 MiniMax-M3.1(type 35)、ch29 GPT-Pool-image(type 1，克隆 ch13)、ch30 漫屋ArcReel-image(type 66，克隆 ch20)；改动前备份表 `bak_20261107_img_channels`/`bak_20261107_img_abilities`。
- **四图片模型调用协议**（真机台账 `scripts/prod-ledger-img4-20261107.sql`，commit `7bae1e06d`；两轨不可混用）：

| 模型 | 协议 | 实测 | 客户侧成本 |
|---|---|---|---|
| gpt-image-2.5-官方 | 同步 `POST /v1/images/generations`，**只回 b64_json、无 url** | 40.6s | ¥0.15 |
| Nano Banana Pro | 任务协议 `POST /v1/videos` → 轮询 `GET /v1/videos/{id}` → `GET /v1/videos/{id}/content` 取 PNG | ~110s | ¥0.30 |
| Seedream 4.5 | 同上（轮询约 160s，timeout 至少 300s） | ~160s | ¥0.20 |
| Seedream 5.0 Flash | 同上 | ~50s | ¥0.20 |

  - 任务三模型走 `/v1/images/generations` 是 404。manwu 通道（type 66）硬约束：参考图只收公网 http(s) URL（data URI → 400 `invalid_input_reference`，不建单不扣费）；比例白名单随模型形态（kindImage 六档缺省 16:9 / kindSeedreamImage 八档含「自动」缺省不下发）；图片形态拒收 seconds/duration/resolution/negative_prompt；张数上限 10/4。
  - 消费侧兜底链在 `ArcReelLocalDesktop/scripts/cloud_image_fallback.py`（双协议自动分派，单模型失败一次即切换），配置 `ARCREEL_CLOUD_IMAGE_*`（arcreel 机 `/opt/arcreel/gateway.env`）。
- **未决坑（2026-11-07）**：gpt-image-2.5-官方 唯一出图账号 = sub2api group 4 的 `codex-plus-snehitoge828`（account 4）；其 OAuth 被上游吊销（`Token revoked (401)`）后 schedulable=f，调用只剩 503「No available compatible accounts」（sub2api 日志 `account_select_failed ... pool=1`）。恢复须在 sub2api 管理端重新授权该账号或补账号进 group 4——**禁止自动触发 OAuth 恢复**，先报用户拍板。
- **参数矩阵实测（2026-11-07，台账 `scripts/prod-ledger-img4-paramatrix-20261107.sql`，¥6.40）**：三模型已在 default 组完整暴露（ch20 + abilities + ModelPrice + `/v1/models` 可见）。结论：
  - Seedream 4.5（dola）：8 档比例 + count=2 + 参考图全过；**部分档交付低清预览**（9:16 216x384/134KB、3:4 288x384、4:3 384x288、ref 384x384），同档其它次为 5-6MB 全清，疑取图节点抓到站点预览图，待查。
  - Seedream 5.0 Flash（doubao）：8 档比例 + count=2 全过（1536x1536~2848x1600）；**参考图当前不可用**——豆包官网无 `input[type=file]` 上传入口（页面结构变更），Worker 硬闸门拒单、未提交不扣费，换号无效。
  - Nano Banana Pro（gemini）：比例**只有 3:4 / 16:9 生效**，9:16 与 1:1 均实出 1024x559（16:9 缺省）——Worker `buildFillImageParamsScript` 在找不到宽高比控件时按设计跳过（`image-params-skipped:aspect-ratio-unavailable`），不报错；唯一 gemini 账号 `MicahMaconv@gmail.com` 掉登录后整链排队/取消，须重登。
  - new-api 侧参数校验（免费负向）3/3 通过：非法比例、`seconds` 字段、data URI 参考图均返回 400。
- **SeetaCloud 渠道（ch28，type 70，2026-11-08 改版收口，台账 `scripts/prod-ledger-seeta-v2omni-20261107.sql`、`scripts/prod-omni-price-20261108.sql`、`scripts/prod-retire-v2-20261108.sql`、`scripts/prod-omni-refvideo-billing-20261108.sql`）**：上游 = 自建 ArcReel H3 网关（`u1133789-a936-c07acd4f.westd.seetacloud.com:8443`，AutoDL ComfyUI 协议，裸 token）。**对外只留 `minimax_h3_omni`**（多模态参考 9 图+3 视频+3 音频+1080p）；`minimax_h3_v2` 已于 2026-11-08 退役下架（ch28 models / abilities / 卡片 / ModelPrice 键均已清理），只在 `workflowByModel` 保留隐藏映射供在途任务与回滚。基准价=生产 ModelPrice（omni `0.0027397260273972603`，即 ¥0.02/7.3）+ 代码 `defaultModelPrice`；倍率 480P 1.0 / 768p 2.0 / 1080p 3.0 在 `seeta/constants.go`，**改价必须两边成对改**。**2026-11-08 起 omni 参考视频按秒并入计费**：`seconds = 生成秒数 + 参考视频总秒数`（逐条解析 mvhd 四舍五入后相加，再乘档位倍率），探测在 validate 阶段完成、解析不出时长即 400 拒单（仅 MP4/MOV 可读），参考图/音频不额外收费。真机 4/4 出片（48-70s）+ 负向 3/3。**2026-11-08 输入模式收口（台账 `scripts/prod-omni-input-modes-20261108.sql`）**：上游按 payload 派生三档，纯文生＝零素材、图生/首尾帧＝只传 `metadata.first_frame`(+`last_frame`)、多参考＝带 `ref_image_N`/`ref_video_N`/`ref_audio_N`；`images` 是设定参考不是首帧。带参考视频/音频却无设定参考图、只给尾帧不给首帧，均在 `ValidateRequestAndSetAction` 本地 400（预扣费之前，不建单不扣费不 AutoBan），上游内部工作流 ID `minimax_h3_lightx2v*` 走全局脱敏。**纯文生/首尾帧本站未做端到端真机验证（用户选择先不跑）**。参考视频计费真机 1 条已验证：768p横 5s + 11s 参考 = 16 计费秒 / ¥0.64 / 1344×768 出片。契约细节见 `docs/agent-guides/channel-contracts.md` SeetaCloud 节。
- **部署链路**：本地提交 → `git push fork main`（远端只有 `fork`，没有 `origin`）→ 服务器 `bash /home/ubuntu/deploy-new-api.sh <short-sha>`（带 SHA 断言：docker build → 换容器 → 健康检查）。**服务器连不上 github**：push 后服务器 fetch 不到会 ABORT，须本地 `git bundle create x.bundle refs/heads/main ^<服务器HEAD>` 经 ssh 管道传入、服务器 `git fetch x.bundle main:refs/remotes/bundle/main` 后再部署。⚠️ ssh 管道写文件与后台化组合会把文件截断成 0 字节——传完必须 `wc -c` 验字节数。⚠️ `git bundle` 的正端**必须是引用名**（`main ^<sha>`），裸 SHA 当正端会报「Refusing to create empty bundle」（2026-11-14 实踩）。
- **素材直传 COS（2026-11-14 上线，端到端真机已验）**：`POST /v1/files`（TokenAuth，不挂 Distribute；路由在 `router/video-router.go` 的 filesV1Router，**不在** relay-router——那里 `/v1/files` 其余方法仍是 RelayNotImplemented 占位）。流程：客户端带文件名调 `/v1/files` → 网关按 manwu 渠道转发 ArcReel `/api/v1/remote-generation/cos-upload/presign`（service token 鉴权，与 staging 同口径）→ 返回预签名 PUT URL（30 分钟）+ `object_url`（CDN）→ 客户端**直传**腾讯云 COS，字节不过网关（老机出口仅 3Mbps；境外素材源实测 67-79KB/s，ArcReel 代下载 60s 硬预算下单图 >4MB 必 400——直传后无此限制，上限 25MB/对象）。建单时引用 `object_url`，ArcReel 从 CDN 拉取 TTFB 0.1-0.3s。实现 `controller/files.go` + `relay/channel/task/manwu/cos_presign.go`；大小上限默认 200MB（`FILES_PRESIGN_MAX_MB` 可调）。

## 新生产实例（迁移目标机，2026-11-12 起建）

| 项 | 值 |
|---|---|
| 服务器 | `ssh ubuntu@114.132.252.150`（root 同密钥可登；广州 ap-guangzhou，腾讯云轻量 2C4G/59G/5M） |
| SSH 密钥 | 本机 `C:\Users\hgx\.ssh\id_ed25519_nopass`（公钥 `auto-gp-wencai`，已装入 ubuntu/root 的 authorized_keys） |
| 基座 | Ubuntu 24.04；Docker 29.9.3 + compose v5.7.1（腾讯云/daocloud 镜像源）；swap 4G（/swap.img + /swapfile，swappiness=10）；nginx 1.24.0 + certbot（证书 2027-02-10 到期自动续期） |
| 域名 | `https://api2.heibaidao.cn`（2026-11-12 上线；A 记录在 DNSPod，域名属另一账号 UIN `100046072329`，管理需该账号密钥；轻量防火墙 22/80/443/ICMP 已放行） |
| 定位 | 已复刻老机 new-api + sub2api 全套（迁移完成记录见方案 §12），接**新业务**；老机 119.29.253.97 保持原样不动 |
| 迁移方案 | `docs/new-server-migration-plan.md`（唯一权威；含范围、COS 中转、渠道核对清单、验收项、§12 完成记录与遗留项） |
| 腾讯云子账户 | 主账号 `100005629999`（APPID `1256715892`），子用户 `hgx001`（`AdministratorAccess`；密钥源 `/root/.cos.env` 600，另经 `/root/cosdirect-provision.sh` 明文下发一份到 `options.CosDirectSetting`，两处同源须成对轮换；`TencentMPSSetting` 属另一账号密钥，轮换时勿改；不打印不入聊天） |
| COS 桶 | `hbd-video-1256715892`（ap-guangzhou，私有，生命周期 30 天）。老机 `mps-test-1397292858` 属**另一个腾讯云账号**，其 secret 不可复用 |
| 双机差异 | 新机 compose 固定 subnet `172.18.0.0/16`；sub2api 8080 只绑 `172.18.0.1`；options 三项已改 api2（备份 `bak_20261112_options_api2`）；new-api logs/ 属主须 1000:1001（compose 重建目录后需重新 chown） |
| 新机部署 | `DEPLOY_HOST=newapi DEPLOY_PORT=22 deploy/deploy-backend.sh <sha>`（ssh config 别名 `newapi`/`newapi-root`；ubuntu 已入 docker 组）。**PowerShell 里裸 `bash` 是 WSL，env 不继承会误打老机**——必须走 Git Bash 且重定向到文件才能拿到输出。新机可直接 fetch github（不需要 bundle）。⚠️ 新机 new-api 容器由 deploy 脚本 `docker run` 管理，勿对 `/home/ubuntu/new-api/docker-compose.yml` 跑 `compose up`，会把镜像回退到迁移时的旧版 |
| COS 直存 | options 键 `CosDirectSetting`（2026-11-12 已下发，密钥源 `/root/.cos.env`，下发脚本 `/root/cosdirect-provision.sh`）；wan3 非超分模型成片经 `SubmitDirectRelocate` 拉片直传桶 `hbd-video-1256715892`，占位符 `cos-proxy://` 取片时按桶匹配凭据预签名 302 |

- 迁移传输走 **COS 广州内网**中转（老机公网出口实测仅 2.6Mbps，直传不可行）；COS 中转完成后删除中转前缀。
- 新机产物链路规划：一期 COS 直存 + 302 预签名（无损，不过 MPS 转码），二期 CDN 域名；复用 `wan3/relocate.go` 的 `cos-proxy://` 协议。
- 双机并行期：老机的 DB/options/sub2api OAuth 账号状态变更不会自动同步新机；配置冻结或两边手动同步，sub2api 账号异常先报用户拍板。**代码版本必须双机一致**，见「部署与计费硬约束」。

## 按需必读索引

| 任务 | 先读 |
|---|---|
| 漫屋/ArcReel/Worker、参考图、模型名、文本/产物代理 | [remote-generation.md](docs/agent-guides/remote-generation.md) |
| MiniMax/H3、MegaAI别名、三元巡狩、Wan3规格/计费差异 | [channel-contracts.md](docs/agent-guides/channel-contracts.md) |
| 模型改名/调价、目录/能力/缓存、上下架、AutoBan恢复 | [pricing-catalog.md](docs/agent-guides/pricing-catalog.md) |
| 支付/回调、补单、额度缓存、兑换码/订阅并发安全 | [payments.md](docs/agent-guides/payments.md) |
| 发布护栏、安全探针、误建撤销、sub2api/OAuth | [production-operations.md](docs/agent-guides/production-operations.md) |
| 实际发布/回滚步骤（单机执行体） | [deploy/README.md](deploy/README.md)（唯一权威流程）；双机同版本编排见本文「部署与计费硬约束」 |
| 有赞上游余额对账、怀疑异常扣款 | `bash scripts/audit-youzan-balance.sh <人工抄的余额>`（在生产服务器执行；节点与用法见脚本头注释） |
| 表达式/动态分层计费 | [pkg/billingexpr/expr.md](pkg/billingexpr/expr.md) |

## 部署与计费硬约束

- **双机部署、严格同版本（2026-11-12 用户拍板）**：生产是老机 `api.heibaidao.cn`（`ubuntu@119.29.253.97:877`）与新机 `api2.heibaidao.cn`（ssh 别名 `newapi`）并存。**每次发版两台都要发，且必须锚定同一个 commit SHA**；只发一台＝发版未完成，任何时刻两台版本不得分叉。前后端产物同锚定该 SHA，前端用 `DEPLOY_HOST`/`DEPLOY_PORT` 覆盖老机默认值、两台各发一次。
  - 入口：老机 `bash /home/ubuntu/deploy-new-api.sh <sha>`（连不上 github，bundle 传 SHA 后 `wc -c` 验字节）；新机 `DEPLOY_HOST=newapi DEPLOY_PORT=22 deploy/deploy-backend.sh <sha>`（必须 Git Bash，输出重定向到文件）。执行细节见 [deploy/README.md](deploy/README.md) 与上文「新生产实例」。
  - 验收：逐机核对部署 SHA / 前端 release `<timestamp>-<sha>` 与 `/api/status`（`success+setup`、PostgreSQL、未回落 SQLite），**两边一致才算完成**；不一致立即补齐落后的那台，不留待下次。回滚同样双机同版本，一台回滚后另一台不得留在高版本。
  - 版本对齐 ≠ 数据对齐：两台 DB/options/COS 凭据/sub2api 账号状态不随发版同步，差异见 `docs/new-server-migration-plan.md`；不得为追版本覆盖另一台的数据。
- 后端用 `deploy/deploy-backend.sh <sha>`（老机另有 `/home/ubuntu/deploy-new-api.sh`）在生产远程构建精确提交，**两台各执行一次**；本地Docker不是前提。前后端发布路径独立，禁止临时脚本绕过canonical流程。
- env快照完整且含关键配置才换容器；健康检查需success+setup、数据库后端与SQL_DSN都正确，不能以回落空SQLite的success:true判健康。
- 模型变更**先发版、再改渠道/价表数据**；生产ModelPrice覆盖代码默认，价格与档位倍率/前端单位/最终结算一起核对。
- 下架同步channels.models与abilities，AutoBan恢复同步status与enabled；缓存/目录/卡片是不同链路，不能只改一个字段宣布完成。
- 别名对普通用户所有响应/DTO/日志/产物URL出口遮上游，数据库/管理员原文保留；不在写入层/重试分类前脱敏。
- 支付入账必须行锁/CAS/同事务幂等；金额十进制算一次，**先入账再回success**，不让合法回调重放重复加额度。

## Project Governance

- 页脚归属必须保留：`footer.newapi.projectAttributionSuffix`及翻译、`Designed and developed by New API`（或其译文）。
- 所有源码版权头 `Copyright (C) 2023-2026 QuantumNous` 与AGPL-3.0要求保留，不为品牌/文档整理删除。
- 创建PR前核对当前git身份与历史主要作者，不改git config；非历史主要作者在PR说明AI生成/辅助，使用 `.github/PULL_REQUEST_TEMPLATE.md`，不另造结构。

## 文档维护

根文件只放全局约束与索引；专题仅保留用户决定、真实协议、昂贵反证、资金/权限/交付风险及未决项。普通已修故障、静态实现清单、过期价格/账号状态、任务号、日志时序与重复复盘直接删除，不改名继续堆。历史追溯用Git；同机制只保留现行口径，配置现查、未验证明确标注。纯文档整理只做静态校验，不启动真实生成/付款/部署。
