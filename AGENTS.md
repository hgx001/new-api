# new-api 项目约束

DO NOT send optional commentary

Go/Gin/GORM API网关，前端为 `web/default/`（React/TypeScript/Rsbuild）。根文件只放全局硬约束和按需索引；渠道、定价、支付、运维契约分到 `docs/agent-guides/`，命中主题后必读，不全量加载。

## 费用与生产安全

- **未经用户明确允许，禁止任何真实视频生成测试**，包括连通性/计费探针、真实e2e或重复采样。默认用mock单测、历史产物、日志/DB。
- 获准真测也须明确次数/费用，默认最多1条；重跑/扩量另确认。客户端退款、HTTP400/402不保证上游零成本；不能以“探针”名义绕过授权。
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

## 按需必读索引

| 任务 | 先读 |
|---|---|
| 漫屋/ArcReel/Worker、参考图、模型名、文本/产物代理 | [remote-generation.md](docs/agent-guides/remote-generation.md) |
| MiniMax/H3、MegaAI别名、三元巡狩、Wan3规格/计费差异 | [channel-contracts.md](docs/agent-guides/channel-contracts.md) |
| 模型改名/调价、目录/能力/缓存、上下架、AutoBan恢复 | [pricing-catalog.md](docs/agent-guides/pricing-catalog.md) |
| 支付/回调、补单、额度缓存、兑换码/订阅并发安全 | [payments.md](docs/agent-guides/payments.md) |
| 发布护栏、安全探针、误建撤销、sub2api/OAuth | [production-operations.md](docs/agent-guides/production-operations.md) |
| 实际发布/回滚步骤 | [deploy/README.md](deploy/README.md)（唯一权威流程） |
| 表达式/动态分层计费 | [pkg/billingexpr/expr.md](pkg/billingexpr/expr.md) |

## 部署与计费硬约束

- 后端用 `deploy/deploy-backend.sh <sha>` 在生产远程构建精确提交；本地Docker不是前提。前后端发布路径独立，禁止临时脚本绕过canonical流程。
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
