# 设计方案：CapCut（cp-*）参考音频 / 参考视频全链路支持

- 日期：2026-11-15（v2，含方案审查修订）
- 触发：用户口径反转 —— CapCut 官网「多模态参考」支持图片 + 视频 + 音频，但 new-api 目前**音频静默丢弃、视频上限 1 个**，客户按带音视频下单却拿不到对应交付。
- 范围：new-api（网关）→ ArcReel（建单/托管）→ 燃境 Ai App Worker（执行）→ CapCut 官网。
- 关联事故：`gen-dae093a317f9948aa8a48afd`（提示词 `@image01…@image08`、`@audio01…@audio03`；音频标记悬空 + 图片未做位置绑定 → 人物串脸）。

## 0. v2 修订记录（方案审查结论已并入正文）

| # | v1 口径 | v2 修订 | 理由 |
|---|---|---|---|
| R1 | 音频时长**解析失败 → 400** | **解析失败 → 放行 + 告警日志**（复用 `taskcommon/refaudio.go` 既有宽容口径，不新增 fail-loud 路径） | 素材地址可能只对上游可达（IP 白名单 / 私有签名），我们抓不到 ≠ 超长；一刀切拒单会把本来能跑的请求打死，而音频**不参与计费**，误放的代价只是上游可能拒一次（自动退款） |
| R2 | 「提示词索引越界 → 失败」未限定条件 | 明确限定：**该类型上传数 > 0** 才判越界；某类型上传数 = 0 时该类型标记**一律字面保留** | v1 的表述会被实现成「无素材也判越界」，Worker 先发布即打爆存量带 `@audioNN` 的单（事故单正是 3 音频标记 + 0 音频素材） |
| R3 | 视频「合计 ≤15s」未说明取整口径 | **限长用未取整浮点合计**，**计费仍用逐条四舍五入后相加** | 两者混用会互相污染：3×5.4s = 16.2s 应拒，若先取整成 5+5+5=15 就误放；反之用浮点算钱会与上游账单对不上 |
| R4 | 风险 #3：「2 条 8s 的旧请求此前会被拒、现在会放行 —— 属预期放宽」 | **方向写反了**：旧口径是**逐条** ≤15s，2×8s 一直**放行**；新口径合计 16s 会**开始拒绝** —— 这是**收紧**，属破坏性变更 | 上线前必须按「收紧」处理（公告 + 灰度），不能按「放宽」直接发 |
| R5 | 发布顺序只有「先下游后上游」的软约束 | 增加**显式 feature switch**（Worker 配置项 + 客户端版本标识），三端由开关控制启用，而不是靠「无素材字面保留」这一条行为兜底 | 软约束只能保证「不失败」，保证不了「不静默丢」；有开关才能单点回滚 |
| R6 | 槽位偏移按「上传顺序 = 节点顺序」推算 | Worker 填充前**读回 DOM 素材节点顺序**校准偏移，不一致就按节点实际顺序重排后再绑定 | 上传顺序 ≠ 官网渲染顺序是有先例的失败模式（拖拽/异步解码），假设一旦不成立就是串脸 |
| R7 | 计费「音频不参与」只写在文字里 | 补**对照测试**：同一请求带/不带音频，`EstimateBilling` 结果必须相等 | 文字约束没有测试锁定，下一位维护者「顺手」把音频加进 `seconds` 就没人发现 |

## 1. 现状盘点（2026-11-15 代码事实）

| 环节 | 图片 | 视频 | 音频 |
|---|---|---|---|
| 官网 CapCut | 收（≤30 张已真机标定） | 收（`accept=image/*,video/*,audio/*`，4s mp4 真机标定 loaded） | 收（同一素材口，官方面板公开口径 2.5 系列 10 音频） |
| new-api | 透传 `reference_image`（≤30） | 透传 `reference_video`（**≤1**） | **接受但静默丢弃**，不下发 |
| ArcReel | 透传（≤30，超出 400） | 透传（≤1，超出 400） | **静默过滤**（`_filter_capcut_reference_inputs`） |
| Worker | 上传 + `@imageN` 位置绑定（2026-11-15 已实现） | 上传（单值 `referenceVideo`） | 调度器**未声明** `referenceAudio` → 适配层明确拒单；上传分组不含音频 |
| 计费 | 不参与 | **参考视频秒数计入计费秒**（`duration + refVideoSeconds`） | 不参与 |

代码位置：

- new-api：`relay/channel/task/manwu/adaptor.go`（音频记账/丢弃 L2058-L2137；上限 L1741-L1762；inputs 拼装 L1333-L1344；计费秒 L1211）
- new-api 测试：`relay/channel/task/manwu/capcut_reference_matrix_test.go`（`TestCapcutReferenceAudioSilentlyDropped` 等）
- new-api 时长探测：`relay/channel/task/taskcommon/refvideo.go`（逐条限长 + 计费）、`refaudio.go`（**合计**限长，探测失败宽容放行 —— 本次直接复用，见 R1）
- ArcReel：`server/routers/remote_generation.py::_filter_capcut_reference_inputs`（L548-L605）
- Worker：`electron/remote/bootstrap.ts`（capcut `supports.referenceVideo`，音频未声明 L435-L441）、`electron/dispatcher/domAvReference.ts`、`electron/dispatcher/submitFlow.ts::buildReferenceGroups`（L2532-L2560）、`src/platforms/capcut/mentionPlan.ts`、`src/platforms/capcut/params.ts`

## 2. 目标与非目标

目标：

1. 音频参考与视频参考**完整透传**到 CapCut 官网并参与内容生成：
   - `media[type=reference_audio]`、顶层 `audios/audio` 均识别；
   - 提示词里的 `@audioN` 绑定成真实音频素材节点（与 `@imageN` 同一机制）。
2. 视频从「单值 1 个」扩到多值（与音频同一批改造，避免二次改载荷）。
3. 上限显式、超限显式失败（400 / fail-fast），任何环节不静默截断。
4. 计费口径保持可解释：图片/音频不参与计费秒，参考视频秒数维持现状。

非目标：

- 不改 CapCut「生成音频」开关（保持出厂开，客户显式静音才关）。
- 不改按秒单价/档位倍率。
- 不为其它渠道顺带放开（各自契约独立）。

## 3. 关键设计决策

### D1 上限口径（2026-11-15 用户拍板：视频 5、音频 5）

| 素材 | 官网公开口径 | 本方案 | 依据 |
|---|---|---|---|
| 图片 | 30 | **30（不变）** | 2026-11-14 真机 30 张全收 |
| 视频 | 10（2.5 系列） | **5 条，合计 ≤15s** | 用户 2026-11-15「音视频上限都是 5，合计不超过 15 秒」 |
| 音频 | 10（2.5 系列） | **5 条，合计 ≤15s** | 同上 |

三端常量必须同源同名（`CAPCUT_MAX_REFERENCE_AUDIOS` / `capcutMaxReferenceAudios`），任何一处窄于其它两处都会变成静默丢弃。时长上限**不新增常量**：直接复用 `taskcommon.MaxReferenceVideoSeconds` / `MaxReferenceAudioSeconds`（均 15 秒，与万相同一条上游硬约束），避免第二处真值。

**「合计 15 秒」的口径澄清（v2 新增）**：用户原话可读作「同一类合计」或「音视频全部合计」。本方案取**同一类合计 ≤15s**（视频 ≤15s、音频 ≤15s），理由：

1. 视频参与计费、音频不参与 —— 合并计量会让客户收到「同类 15 秒内、合并超限」这种和账单无关的拒单理由，无法自查；
2. 上游素材口本来就分类计量（2.5 系列 10 视频 / 10 音频）。

若真机确认上游另有一条「音视频合计 ≤15s」的硬约束，则把两处校验合并为一次（实现上就是同一个 loop 累加两个桶，属一处改动，见 §4.1）。

### D2 载荷契约：顺序固定 `image → video → audio`

- new-api `inputs` 顺序：图片 → 视频 → 音频（图在前，视频解码慢放中间，音频最轻放最后）。
- Worker 上传分组顺序同序。
- **槽位基准（v2 修订，R6）**：不能假设「上传顺序 = 节点顺序」。填充前必须**读回** `data-reference-media-tag-type` 节点序列，按实际序列计算槽位：
  - 读回的序列与上传顺序一致 → 直接用 `@imageN → N-1`、`@videoN → 图片数+N-1`、`@audioN → 图片数+视频数+N-1`；
  - 不一致 → 按节点实际顺序重排映射后再绑定（映射是纯函数，重排只换输入，不改规则）。
- 该映射收敛在 `mentionPlan.ts`（纯函数，现有单测直接扩展）；读回与重排放在 `domAvReference.ts`。

### D3 Worker 载荷字段

- 音频：复用现有 `audioFiles: AudioFileData[]`。`RemoteTaskAdapter._downloadAndSplitInputs` 当前**会把多个音频全部返回**，但同时打一条「scheduler uses first only」日志（对只取首个的万相成立，对 CapCut 多音频不成立）—— 改造时同步修正该日志。
- 视频：`DomScheduledTask` 目前只有单值 `referenceVideo`；`_downloadAndSplitInputs` 当前**只取第一个视频并 warn 丢弃其余**（L2944-L2952）。需新增 `videoFiles?: string[]`，保留 `referenceVideo` 兼容（远程层取 `referenceVideo || videoFiles[0]`），并把「多视频只取首个」改为「全部落盘、保序返回」。
- 调度器能力声明：`bootstrap.ts` capcut 增加 `referenceAudio: true`；`domPlatformSupportsAvReference('capcut')` 已为 true，注释同步修正。
- **feature switch（v2 新增，R5）**：capcut 增加 `supports.referenceAudioFlag` 读配置项（默认关），Worker 只在开关打开时声明 `referenceAudio`；同时把「是否上传音频组」也挂在同一个开关上，保证「声明了能力」与「真的会上传」永远同步。

### D4 失败策略

| 场景 | 处置 | 发生点 |
|---|---|---|
| 超出条数上限（图 30 / 视频 5 / 音频 5） | 400 `invalid_input_reference`（new-api）/ 400（ArcReel） | 建单前 |
| 素材只有 path 没有 URL | 400（new-api 只下发 URL） | 建单前 |
| 视频合计 >15s | 400（浮点合计，见 R3） | 建单前 |
| 音频**实测**合计 >15s | 400 | 建单前 |
| 音频时长**测不出** | **放行 + 告警日志**（R1） | 建单前 |
| 视频时长测不出 | 400（**计费依赖**，不能按 0 秒算钱） | 建单前 |
| 下载后数量与声明不符 | Worker fail-fast，不提交 | 提交前 |
| 上传后素材未 `loaded`（≤12s） | Worker fail-fast，不提交 | 提交前 |
| 某类素材**有上传**但提示词索引越界 | Worker fail-fast（`mention-ref-out-of-range`） | 填充前 |
| 某类素材**没有上传**但提示词写了该标记 | **字面保留 + 告警**（不失败、不绑定） | 填充前 |
| DOM 节点序列与上传顺序不一致 | 按节点序列重排映射（不是失败） | 填充前 |
| 官网弹层/节点异常 | Worker fail-fast，不提交 | 填充/确认阶段 |

**「无素材 → 字面保留」是上线安全阀**：客户端提示词模板目前会写 `@audio01…` 这类标记，而音频在改造完成前不会到达 Worker。若把「无素材的标记」判为越界失败，Worker 先发布就会把现有正常单全部打死。规则（R2 的落地措辞）：

- 该类型上传数 > 0 → 标记按序号绑定；序号越界才失败；
- 该类型上传数 = 0 → 标记按原文保留并记 `unresolved`（与今天行为一致）。

图片类型另有兜底：远程适配层的 `checkRemoteReferenceCoverage` 已在下载前核对「提示词图片标记数 vs 实际参考图数」，所以「写了 @image 却没传图」轮不到填充阶段。

一律零额度消耗（预扣费与提交都在校验之后/由失败回滚）。

### D5 计费与时长上限

| 素材 | 条数上限 | 时长上限 | 计费 |
|---|---|---|---|
| 图片 | 30 | 无 | 不参与 |
| 视频 | 5 | **合计 ≤15s（未取整浮点判）** | **计入**：`seconds = 生成时长 + Σ round(每条秒数)` |
| 音频 | 5 | **合计 ≤15s（未取整浮点判）** | **不计入**（仅内容参考） |

- 视频：**新增**总量校验函数（`ProbeAndEnforceReferenceVideosTotal`），不改造现有逐条版 `ProbeAndEnforceReferenceVideos` —— 后者被 erchun / hailuo / wan3 三条链路共用，改语义会连带改它们的契约。新函数一次探测产出两个值：**浮点合计**（判 15s）与**逐条取整合计**（计费），从结构上避免 R3 的混用。
- 音频：直接调用既有 `EnforceReferenceAudioMaxSeconds`（它本来就是这个语义：累加 + 探测失败宽容 + 只在校验侧用，不参与计费），CapCut 只需把返回值 `unmeasured` 写进告警日志。
- ArcReel 侧 `_capcut_price_cents` 只按 `duration × resolution 倍率`，不改（它不向客户收参考素材费）。
- new-api 的 `EstimateBilling` 仍按 `req.Duration + capcutRefVideoSeconds`，音频**不写入该值**；用对照测试锁定（R7）。

## 4. 三端改造清单

### 4.1 new-api（Go）

1. `relay/channel/task/taskcommon/refvideo.go`
   - 新增 `ProbeAndEnforceReferenceVideosTotal(ctx, urls, totalMaxSeconds, client) (billingSeconds int, err error)`：
     - 逐条探测，任一条失败即 fail-loud（视频计费依赖）；
     - 累计 `float` 合计判 `totalMaxSeconds`（`DurationEpsilon` 容差）；
     - 计费用 `RoundSecondsToBilling` 逐条后相加；
     - 不复用/不改动 `ProbeAndEnforceReferenceVideos`（其它三条链路契约不变）。
2. `relay/channel/task/manwu/adaptor.go`
   - 新增 `InputTypeAudio = "reference_audio"`；`normalizedRequest` 增加 `Audios []string`。
   - `resolveCapcutReferenceMaterials` 返回值增加 `audioURLs`；音频**不再丢弃**：
     - 计数 + 上限 `capcutMaxReferenceAudios`（5）；
     - URL 缺失显式 400（与视频同一条判据）；
     - 返回给 validate 段 → `norm.Audios`。
   - `capcutMaxReferenceVideos` 1 → 5；新增 `capcutMaxReferenceAudios = 5`；
     `capcutReferenceAudit` 的日志从「silently dropped」改为「视频=%d 音频=%d（已保留）」。
   - 视频时长口径（D5）：改调 `ProbeAndEnforceReferenceVideosTotal(..., taskcommon.MaxReferenceVideoSeconds, ...)`。
   - 音频时长：调 `EnforceReferenceAudioMaxSeconds(..., taskcommon.MaxReferenceAudioSeconds, ...)`，`unmeasured` 非空时**打 warn 日志**（不拒单）。
   - `BuildRequestBody` `kindCapcutVideo`：`inputs = images + videos + audios`。
   - 注释同步：`kindCapcutVideo` 分支头「音频静默丢弃 / 参考视频显式 400」等过时口径全部改掉。
3. 测试（TDD，先改断言再改实现）：
   - `capcut_reference_matrix_test.go`：`TestCapcutReferenceAudioSilentlyDropped` 反转为 `TestCapcutReferenceAudioPassedThrough`（5 种请求形态都断言 `reference_audio` 出现在 inputs **末尾**）；
   - 新增：音频 5 通过 / 6 → 400；视频 5 通过 / 6 → 400；音频缺 URL → 400；
   - **视频合计 16s → 400 / 15s 边界放行 / 单条 15s 仍放行**；**音频合计 16s → 400 / 15s 边界放行 / 不可解析格式 → 放行**；
   - 图片 30 + 视频 5 + 音频 5 混合顺序断言（image→video→audio）；
   - 计费：视频合计秒数进 `EstimateBilling`；音频秒数**不**进（同一请求带/不带音频的 `seconds` 相等）。

### 4.2 ArcReel（Python）

1. `_filter_capcut_reference_inputs`：
   - 删除音频过滤，改为计数 + 上限 `CAPCUT_MAX_REFERENCE_AUDIOS = 5`（与 new-api/Worker 同口径），超限 400；
   - 返回 `(kept, image_count)` 语义不变（kept 含音频）；
   - 日志改为 `capcut reference inputs kept: images=%d videos=%d audios=%d`。
2. `CAPCUT_MAX_REFERENCE_VIDEOS` 1 → 5（与 D5 计费口径同批发布）。
3. 测试 `tests/test_remote_capcut_reference_inputs.py`：音频保留、音频 5 通过 / 6 → 400、视频 6 → 400、顺序保持。
4. 建单/Worker 下发不用改：`inputs_json` 原样透传，Worker 自己分槽。
5. 时长合计校验只在 new-api 做（唯一收费方；音频不收费、视频计费在 new-api 算账）：ArcReel 只校验条数，不做二次探测（避免重复下载解析）。

### 4.3 Worker（Electron / TS）

1. 能力声明：`bootstrap.ts` capcut `supports: { referenceVideo: true, referenceAudio: <switch> }`（v2：受 R5 的开关控制）。
2. 载荷：`DomScheduledTask` 增加 `videoFiles?: string[]`；`RemoteTaskAdapter` 下载分槽保留多视频顺序；`pickAvReferencePayload` 兼容两者。
3. 上传：`buildReferenceGroups('capcut')` 增加第三组 `{ kind: 'audio', refs: audioFiles }`；`params.ts` 去掉 `referenceAudioCount > 0 → null`，改为 `> 5 → 'too-many-reference-audios'`；视频上限 1 → 5。
4. 提示词绑定：`mentionPlan.ts` 必须是**「有素材才绑定」**（R2）：
   - 该类型上传数 > 0：按序号绑定，越界失败；
   - 该类型上传数 = 0：标记**按原文保留**并记 `unresolved`（= 今天的行为）。
   `buildMentionFillScript` 只补 `wantAudios` 计数；`domAvReference.ts` 增加**节点序列读回 + 重排**（R6）。
5. 测试：`mentionPlan.test.ts` 增加「音频槽位 / 音频越界 / 无音频时字面保留 / 节点序列与上传顺序不一致时按节点重排」；`mentionFillScript.test.ts` 增加 `wantAudios`；`params` 单测反转为接受音频（>5 拒绝）；`buildReferenceGroups` 单测（image→video→audio 顺序）。
6. 真机标定（零额度，只上传不生成）：
   - 上传 2 图 + 1 视频 + 1 音频，确认三类 `data-reference-media-tag-type`、**节点顺序是否等于上传顺序**（R6 的输入）、`@` 弹层是否列出音频项、选中后 attrs 与上传节点一致；
   - 确认音频是否与图片/视频共用同一个 file input（`buildUploadPrepareScript` 目前按 `accept` 含 `image` 挑 input；若官网分槽则要按 kind 选 input）；
   - 标定音频格式/大小/时长限制（mp3/m4a/wav、单文件上限、最短时长），写入三端常量与文档；
   - 上传 5 个视频测确认窗口耗时，决定 12s 确认是否要按素材数放宽；
   - 若 `@` 弹层不列音频（仅图片/视频），则音频绑定降级为「素材在编辑器但不做位置绑定」，方案需回退该子项并记录。

## 5. 发布顺序与回滚

**先下游、后上游**（保证任何时刻不会出现「网关收了音频但下游丢掉」），且每一步都由 R5 的开关控制，可单点回滚：

0. **前置条件**：Worker 落地 D4 的「无素材 → 字面保留」规则 + 节点序列读回（R6）；音频能力开关**默认关**。
1. Worker（`cron_video_brach`）：支持多视频 + 音频上传与绑定，开关关闭时行为与今天完全一致；打开开关单机验证。
2. ArcReel：保留音频并校验上限（此时 new-api 仍丢弃音频 → 客户侧无感知，可长期并存）。
3. new-api：开始向客户接收并下发音频；双机同 SHA 发布（遵循 AGENTS.md 双机同版本硬约束）。
4. Worker 开关按渠道/版本逐步打开；前端/文档同步模型说明与渠道契约（不涉及价格）。

回滚按相反顺序：关 Worker 音频开关 → 回 new-api（重新静默丢，客户侧回到旧口径）→ 回 ArcReel。**不允许只回上游不回下游**（会重新出现静默丢）。

⚠️ **破坏性变更公告**（R4）：视频「逐条 ≤15s」→「合计 ≤15s」会拒绝**此前能跑**的请求（典型：2×8s、3×6s）。上线前需要：① 从 tasks 表统计「参考视频 ≥2 条且合计 >15s」的历史单量；② 对仍在调用的客户提前通知；③ 首周对这类 400 打点监控，确认真实需求再决定是否放宽到 20s（上限是产品口径，不是上游硬限）。

## 6. 验收清单

- 单测：new-api 矩阵 + 总量边界（视频/音频 15s 放行、16s 拒、音频不可解析放行）+ 音频不计费对照；ArcReel 参考素材过滤；Worker mention/params/分组/节点重排。
- 契约测试：同一请求在三端日志里 `inputs` 顺序与数量一致（image→video→audio）。
- 真机（零额度）：上传标定 + 节点序列读回 + `mention-prompt:ok:text=...,refs=N`（N = 提示词引用总数，含 `unresolved` 计数）。
- 真机（需用户单独授权并确认费用）：1 条「2 图 + 1 视频 + 1 音频 + @ 绑定」生成，核对成片引用生效、账单 = 生成时长 + 参考视频合计秒（音频不额外计费）。
- 回归：不带音视频的纯图/纯文生单，`inputs` 与历史完全一致（无新增空组）；带 1 条 ≤15s 视频的存量单计费与今天一致。

## 7. 风险与未决

1. **官网音频行为未真机标定**：上限已按用户口径定为 5（官网公开 10）；`@` 弹层是否列出音频项、音频是否与图/视频共用 file input、节点顺序是否等于上传顺序，必须真机确认（决定「位置绑定」是否可实现）。
2. **音频时长解析的格式覆盖（v2 口径已改）**：mp3 / m4a / wav 之外（aac 裸流、flac）解析不出 → **放行**（R1）。风险从「误拒」变成「误放」：客户传超长 flac 时本地不拦，上游可能拒单（自动退款，不扣费）。上线后用真实素材抽样统计 `unmeasured` 比例，若长期为 0 说明覆盖够用。
3. **视频总量校验是收紧（v2 修正）**：旧口径逐条 ≤15s，`2×8s`、`3×6s` 这类请求**一直能跑**；新口径合计 16/18s 会**开始 400**。这不是放宽而是**破坏性收紧**，见 §5 的三步公告流程。测试里要显式锁定「2×8s → 400」「单条 15s → 放行」两个边界。
4. **多素材解码耗时**：5 条视频 + 5 条音频会拉长上传确认窗口，Worker 现有单次 12s 确认需按素材数动态放宽或改为逐条确认（实现时验证）。
5. **音频与「生成音频」开关的叠加语义**：参考音频是内容参考，不是输出音轨；需在交付说明中写清「成片声音仍由官网生成/客户开关决定」。
6. **客户端提示词分布未知**：存量提示词可能只写 `@audioNN` 而从不带音频素材（本次事故单即如此）。D4 的「无素材 → 字面保留」保证不误杀，但这类单的音频引用永远不会生效，应在上游提示词组装端修正（另行排期）。
7. **旧注释与文档存在「视频显式 400 / 音频静默丢」的过时口径**，实施时一并清理，避免下一位维护者按旧注释改回去。（new-api 侧已清；Worker / ArcReel 待各自发布时清。）
8. ~~**电商总兜底路由仍按旧口径挡音频**~~ —— **已处理（2026-11-15）**：`middleware/seedance_route_ecommerce.go` 的音视频两道拦截已删除，素材改为抬到顶层 `videos` / `audios` 透传（见 §8）。连带影响：`prepareCapcutRoute` 是规则六第三分支 / 规则七 / 规则八共用的唯一准入入口，所以三条规则一起放开，测试三条各有一组用例（`TestEcommerceRoutes*` / `TestTotalFallbackRoutes*`）。
