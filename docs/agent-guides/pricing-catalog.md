# 定价、模型目录与渠道能力

涉及价格、倍率、公开名、上/下架、渠道恢复或模型映射时必读。本文保留计费不变量，不维护会过期的现价表；改价需重新核实官方区域/币种/型号/促销与生产配置。

## 价格真相源与授权范围

- 名称/描述/徽标等显示变化与真实扣费分开；影响预扣、结算、`/api/pricing` 的价格必须在后端定义并验证，前端不能成为实际单价唯一来源。
- 官方价要核对官方来源、部署地域、币种、版本与促销；历史 commit/代码注释、转售价、搜索摘要不是当前官方价。刻意采用经销/优惠价就明确标注。
- 授权只及用户点名的模型，不能顺手改同上游、同渠道或同 adaptor 的其它公开名。
- `defaultModelPrice` 与生产持久化 `ModelPrice` 都要核对：option 会覆盖代码默认，改代码不自动改变线上现价。
- 内部存 USD 等值时显式用 CNY/当前汇率，不沿用历史固定汇率假装实时配置。
- 表达式/动态分层计费必须先读 [billingexpr 设计](../../pkg/billingexpr/expr.md)，不另发明一套 token/额度转换。

## 按次、按秒、按张不可混

- 按次模型 `EstimateBilling` 不再给 seconds 倍率；按秒模型返回有效时长倍率，图片张数走 n。前端“/次、/秒”展示与后台实际结算须一致。
- `TASK_PRICE_PATCH` 的含义是“这些模型按次跳过倍率”，不是修复所有价格的兜底。Nano Banana Pro、Seedance Fast 等依赖 n/seconds 的模型不能放进去，否则静默少收。
- 校验阶段算好的时长/分辨率档位写入 context，`BuildRequestBody` 与 `EstimateBilling` 复用；不要“收480p的钱、上游发720p”。
- 按秒档同时核对基础价、duration/size 倍率、quota_type/endpoint、前端币种、最终结算；不能只改倍率或只改数据库基础价。
- 分辨率倍率按模型名分表；相邻公开名可能属于不同价表，不能用一张全局倍率顺带带偏标准版/官网版。
- 别名模型按**客户端对外名**计价，只有 UpstreamModelName 用于上游映射；不能按实际上游名误收另一档价。
- 保留显式0/false与“未传”的区别，optional scalar 用指针+omitempty；计费/下发不能因零值被 JSON 丢掉而分裂。

## 发布、改数据与模型可见性

顺序：**先后端发版认识新模型/倍率，再修改渠道/价格/卡片数据**。反序会出现“目录可见、调用却 invalid_model”或新价配旧倍率。

三条独立链路：

| 用户看到/使用的东西 | 来源 |
|---|---|
| 模型广场 `/api/pricing` | abilities.enabled × pricing 白名单及缓存 |
| 客户端 `/v1/models` | 启用能力 × 是否存在计费配置 |
| 生产内存路由 | channels.models 及重建的渠道缓存；无缓存部署口径另核对 |

- 下架至少同步移除 `channels.models` 源头与该模型 abilities；只删 abilities，下一次编辑渠道会由 UpdateAbilities 重建复活。
- pricing 白名单本身不等于有可用渠道；保留待恢复白名单也不自动恢复售卖。卡片 models 的描述、标签、厂商与公开价同步，不让旧文案冒充当前能力。
- 恢复 AutoBan 渠道需同时恢复 `channels.status` 与 `abilities.enabled`，优先现有 `scripts/restore-channel.sh`；只恢复 status 会“看着正常但全503”。
- 新建/修改渠道优先管理 API，让能力/缓存按正常路径刷新。裸SQL新建渠道曾不能进入既有路由缓存，必要时经授权重启；不要把旧渠道/价格的周期刷新规律套给新渠道。
- 既有模型上下架/价格 option 的正常周期刷新不要求为每次变更重启。生效时延按当前 SYNC_FREQUENCY/pricing TTL 核对，过早读旧缓存不是失败证据。
- SQL涉及 channels/abilities/models/ModelPrice 时用事务、备份及回滚方案；跨渠道搬模型保留 group/priority/weight，不只搬 model 字符串。
- 退役 `seedance-2.5`、停用 `wan3.0-video-官网` 等不能因代码/旧卡片仍存在而擅自复活。是否在售/有渠道以当次授权和当前配置为准。

## 同源模型的“手动换货”开关

`wan3.0-video-官网`（ch12 DashScope）与 `wan3.0-video-tj`（ch32 客美）是同一商品的两条供货链路。

- 开关：后台 渠道 → 编辑任一承载这两个模型的渠道 → 高级设置 → 路由与覆盖 → 「万相官网 → 客美 tj 换货」（卡片最上面第一张）。接口 `POST /api/video-route/policy/wan3-guanwang-tj`，策略存 `Seedance25TeHuiRoute30s` 的 `wan3_guanwang_tj_enabled`。
- 打开后中间件把 body 的 model 改名 tj（计费原点仍是 **官网**，客户账单不变）；同时写死 `duration`（官网钳制/认 `seconds`，客美 400/不认）并用官网档位倍率覆盖目标倍率（客美 1080P 是 4 倍，官网 3.148 倍，不覆盖等于多收 27%）。
- 两类请求**留在官网**：目标模型当前分组无可用渠道；画幅为自适应（客美无此档，会回落 16:9）。
- 它与促销族规则（`enabled` 及各规则开关）**互不连坐**，换货不等于改价：改价仍要成对改 `ModelPrice` 与档位倍率。

### 带参考视频的万相请求 → hk-wan3（默认开启）

三条万相链路对参考视频的处置不同：官网适配器把音视频素材**整段丢弃**、`wan3.0-video-tj` 自 11-20 起把参考视频秒数**并入计费**、只有 `hk-wan3` 不收参考视频的钱（上游只按生成秒数计价）。

- 开关：同一个「路由与覆盖」区，紧跟换货卡下面；接口 `POST /api/video-route/policy/wan3-ref-video-hk`，策略存 `wan3_ref_video_hk_enabled`（**三态，缺省开启**）。关掉即退回现状。
- 触发：源模型是官网 / tj / prime-tj **且**带 `reference_video`（`media` 数组或顶层 `videos`/`video`），生成时长 **4–15 秒**。计费仍按客户请求的源模型，目标 adaptor 对 hk-wan3 不并入参考视频秒数，档位倍率按源模型覆盖。
- 不路由（即使开关开着）：时长不在 4–15 秒、画幅 adaptive、带首尾帧、素材超 hk-wan3 上限（图 10/视频 5/音频 5/合计 20）、目标无可用渠道。参考视频**总时长 ≤15 秒**由 hk-wan3 的 validate 在预扣前 400（不扣费不建单）。
- 它排在换货卡**之前**：带参考视频的请求先定出口为 hk-wan3，不再被换成 tj。

## 分层验价，不用重复付费采样

1. 确定性单测覆盖所有档位、模型变体、倍率、显式零值、预扣/结算及退款不变量。
2. 对比 `ModelPrice` option、`/api/pricing`、实际可用渠道、模型广场和客户端目录；卡片可能只显示基准价，与最终分层价不同。
3. 如需确认指定渠道，不用自动路由/降级后的成功代替目标渠道证明。真实结算验证仅在明确授权与次数预算内进行。
4. map遍历/浮点截断/并发概率问题在单测确定性复现或 `go test -count` 验，不靠多建真实视频撞随机性。
5. 真任务即使本端退款，上游扣费也未必退；必须记录授权探针的上游实扣与客户端退款（既有 `OPERATOR-COST-ACCOUNTING` 台账），不能以“客户差值0”宣称无成本。

渠道各自计费维度和协议见 [渠道契约](channel-contracts.md)；安全探针和误建撤销见 [生产运维](production-operations.md)。
