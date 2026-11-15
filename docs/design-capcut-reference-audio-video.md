# 设计方案：CapCut（cp-*）参考音频 / 参考视频全链路支持

- 日期：2026-11-15
- 触发：用户口径反转 —— CapCut 官网「多模态参考」支持图片 + 视频 + 音频，但 new-api 目前**音频静默丢弃、视频上限 1 个**，客户按带音视频下单却拿不到对应交付。
- 范围：new-api（网关）→ ArcReel（建单/托管）→ 燃境 Ai App Worker（执行）→ CapCut 官网。
- 关联事故：`gen-dae093a317f9948aa8a48afd`（提示词 `@image01…@image08`、`@audio01…@audio03`；音频标记悬空 + 图片未做位置绑定 → 人物串脸）。

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
| 视频 | 10（2.5 系列） | **5（合计 ≤15s）** | 用户 2026-11-15 明确「音视频上限都是 5，合计不超过 15 秒」 |
| 音频 | 10（2.5 系列） | **5（合计 ≤15s）** | 同上 |

三端常量必须同源同名（`CAPCUT_MAX_REFERENCE_AUDIOS` / `capcutMaxReferenceAudios`），任何一处窄于其它两处都会变成静默丢弃。**时长合计上限统一 15 秒**，计费口径见 D5。

### D2 载荷契约：顺序固定 `image → video → audio`

- new-api `inputs` 顺序：图片 → 视频 → 音频（现有注释已规定「图在前」；视频解码慢放中间，音频最轻放最后也不影响绑定，因为绑定靠序号而非时序）。
- Worker 上传分组顺序同序；编辑器素材节点顺序 = 上传顺序 = `@imageN/@videoN/@audioN` 的槽位基准：
  - `@imageN` → 槽位 `N-1`
  - `@videoN` → `图片数 + N-1`
  - `@audioN` → `图片数 + 视频数 + N-1`
- 该映射已收敛在 `mentionPlan.ts`（纯函数，现有单测直接扩展）。

### D3 Worker 载荷字段

- 音频：复用现有 `audioFiles: AudioFileData[]`。`RemoteTaskAdapter._downloadAndSplitInputs` 当前**会把多个音频全部返回**，但同时打一条「scheduler uses first only」日志（对只取首个的万相成立，对 CapCut 多音频不成立）—— 改造时同步修正该日志。
- 视频：`DomScheduledTask` 目前只有单值 `referenceVideo`；`_downloadAndSplitInputs` 当前**只取第一个视频并 warn 丢弃其余**（L2944-L2952）。需新增 `videoFiles?: string[]`，保留 `referenceVideo` 兼容（远程层取 `referenceVideo || videoFiles[0]`），并把「多视频只取首个」改为「全部落盘、保序返回」。
- 调度器能力声明：`bootstrap.ts` capcut 增加 `referenceAudio: true`；`domPlatformSupportsAvReference('capcut')` 已为 true，注释同步修正。

### D4 失败策略

| 场景 | 处置 | 发生点 |
|---|---|---|
| 超出上限 | 400 `invalid_input_reference`（new-api）/ 400（ArcReel） | 建单前 |
| 素材只有 path 没有 URL | 400（new-api 只下发 URL） | 建单前 |
| 下载后数量与声明不符 | Worker fail-fast，不提交 | 提交前 |
| 上传后素材未 `loaded`（≤12s） | Worker fail-fast，不提交 | 提交前 |
| 某类素材**有上传**但提示词索引越界 | Worker fail-fast（`mention-ref-out-of-range`） | 填充前 |
| 某类素材**没有上传**但提示词写了该标记 | **字面保留 + 告警**（不失败、不绑定） | 填充前 |
| 官网弹层/节点异常 | Worker fail-fast，不提交 | 填充/确认阶段 |

**「无素材 → 字面保留」是上线安全阀**（本次审查补）：客户端提示词模板目前会写 `@audio01…` 这类标记，而音频在改造完成前不会到达 Worker（本次事故 `gen-dae093a317f9948aa8a48afd` 就是 3 个音频标记 + 0 个音频素材）。若把「无素材的标记」判为越界失败，Worker 先发布就会把现有正常单全部打死。规则定为：

- 该类型上传数 > 0 → 标记按序号绑定；序号越界才失败；
- 该类型上传数 = 0 → 标记按原文保留并记 `unresolved`（与今天行为一致）。

图片类型另有兜底：远程适配层的 `checkRemoteReferenceCoverage` 已在下载前核对「提示词图片标记数 vs 实际参考图数」，所以「写了 @image 却没传图」轮不到填充阶段。

一律零额度消耗（预扣费与提交都在校验之后/由失败回滚）。

### D5 计费与时长上限（2026-11-15 用户拍板）

| 素材 | 条数上限 | 时长上限 | 计费 |
|---|---|---|---|
| 图片 | 30 | 无 | 不参与 |
| 视频 | 5 | **5 条合计 ≤15 秒** | **计入**：`seconds = 生成时长 + 参考视频合计秒数`（合计 ≤15，故最多 +15） |
| 音频 | 5 | **5 条合计 ≤15 秒** | **不计入**（仅内容参考） |

- 视频：沿用 `ProbeAndEnforceReferenceVideos` 的探测能力，但**口径从「单条 ≤15s」改为「合计 ≤15s」**——现有 `taskcommon` 的 maxSeconds 是逐条校验（L21-L34），需要新增/改造为总量校验，否则 5×15s 会被放行。
- 音频：**必须做时长探测**（只校验、不计费）：新增 `taskcommon` 音频时长解析（mp3 / m4a / wav；m4a 可复用 mvhd 解析），合计 >15s 或解析失败 → 400。与 SeetaCloud 参考视频「解析不出时长即 400 拒单」同一口径，不静默放行。
- ArcReel 侧 `_capcut_price_cents` 只按 `duration × resolution 倍率`，不改（它不向客户收参考素材费）。
- new-api 的 `EstimateBilling` 仍按 `req.Duration + capcutRefVideoSeconds`，音频不写入该值。

## 4. 三端改造清单

### 4.1 new-api（Go）

1. `adaptor.go`
   - 新增 `InputTypeAudio = "reference_audio"`。
   - `normalizedRequest` 增加 `Audios []string`。
   - `resolveCapcutReferenceMaterials` 返回值增加 `audioURLs`；音频**不再计入 audit 后丢弃**，而是：
     - 计数 + 校验上限 `capcutMaxReferenceAudios`（5）；
     - URL 缺失显式 400；
     - 返回给 validate 段 → `norm.Audios`。
   - `capcutMaxReferenceVideos` 1 → 5；`capcutReferenceAudit` 增加 `AudioKept`（保留数），日志从「silently dropped」改为「kept audio=N」。
   - 时长口径（D5）：视频改为**合计 ≤15s**（新增总量校验，不复用逐条上限）；音频新增时长探测（mp3/m4a/wav，合计 ≤15s，解析失败 400），**不计费**。
   - `BuildRequestBody` `kindCapcutVideo`：`inputs = images + videos + audios`。
   - 注释与 `docs/agent-guides/channel-contracts.md` 的 CapCut 节同步（另一处还有「参考视频显式 400」的过时注释 L926-L934，一并修正）。
2. 测试（TDD，先改断言再改实现）：
   - `capcut_reference_matrix_test.go`：`TestCapcutReferenceAudioSilentlyDropped` 反转为 `TestCapcutReferenceAudioPassedThrough`（三种请求形态都断言 `reference_audio` 出现在 inputs 且顺序在最后）；
   - 新增：音频 >5 → 400；视频 >5 → 400；音频 URL 缺失 → 400；**视频合计 16s → 400 / 15s 边界放行**；**音频合计 16s → 400 / 解析失败 → 400 / 15s 边界放行**；图片 30 + 视频 5 + 音频 5 混合顺序断言；
   - 计费：视频合计秒数进 `EstimateBilling`；音频秒数**不**进（同一请求对比有无音频的计费值相等）。

### 4.2 ArcReel（Python）

1. `_filter_capcut_reference_inputs`：
   - 删除音频过滤，改为计数 + 上限 `CAPCUT_MAX_REFERENCE_AUDIOS = 5`（与 new-api/Worker 同口径），超限 400；
   - 返回 `(kept, image_count)` 语义不变（kept 含音频）；
   - 日志改为 `capcut reference inputs kept: images=%d videos=%d audios=%d`。
2. `CAPCUT_MAX_REFERENCE_VIDEOS` 1 → 5（与 D5 计费口径同批发布）。
3. 测试 `tests/test_remote_capcut_reference_inputs.py`：音频保留、音频 >5 → 400、视频 >5 → 400、顺序保持。
4. 建单/Worker 下发不用改：`inputs_json` 原样透传，Worker 自己分槽。
5. 时长合计校验放在 new-api 建单前（唯一收费方）；ArcReel 只校验条数，不做二次探测（避免重复下载解析、且它不按素材收费）。

### 4.3 Worker（Electron / TS）

1. 能力声明：`bootstrap.ts` capcut `supports: { referenceVideo: true, referenceAudio: true }`。
2. 载荷：`DomScheduledTask` 增加 `videoFiles?: string[]`；`RemoteTaskAdapter` 下载分槽保留多视频顺序；`pickAvReferencePayload` 兼容两者。
3. 上传：`buildReferenceGroups('capcut')` 增加第三组 `{ kind: 'audio', refs: audioFiles }`；`params.ts` 去掉 `referenceAudioCount > 0 → null`，改为 `> 5 → 'too-many-reference-audios'`；视频上限 1 → 5。
4. 提示词绑定（本次审查修正）：`mentionPlan.ts` 的规则必须是**「有素材才绑定」**：
   - 音频上传数 > 0：`@audioN` → `{kind:'audio', slot: 图片数+视频数+N-1}`，越界失败；
   - 音频上传数 = 0：`@audioN` **按原文保留**并记 `unresolved`（= 今天的行为）。
   图片/视频同理（图片另有 `checkRemoteReferenceCoverage` 前置兜底）。若改成「无素材也判越界」，Worker 先发布就会打爆现有带 `@audioNN` 提示词的单（事故单正是 3 个音频标记 + 0 个音频素材）。
   `buildMentionFillScript` 只补 `wantAudios` 计数。
5. 测试：`mentionPlan.test.ts` 增加「音频槽位/音频越界/无音频时字面保留」；`mentionFillScript.test.ts` 增加 `wantAudios`；`params` 单测反转为接受音频（>5 拒绝）；`buildReferenceGroups` 单测（image→video→audio 顺序）。
6. 真机标定（零额度，只上传不生成）：
   - 上传 2 图 + 1 视频 + 1 音频，确认三类 `data-reference-media-tag-type`、节点顺序、`@` 弹层是否列出音频项、选中后 attrs 与上传节点一致；
   - 确认音频是否与图片/视频共用同一个 file input（`buildUploadPrepareScript` 目前按 `accept` 含 `image` 挑 input；若官网分槽则要按 kind 选 input）；
   - 标定音频格式/大小/时长限制（mp3/m4a/wav、单文件上限、最短时长），写入三端常量与文档；
   - 上传 5 个视频测确认窗口耗时，决定 12s 确认是否要按素材数放宽；
   - 若 `@` 弹层不列音频（仅图片/视频），则音频绑定降级为「素材在编辑器但不做位置绑定」，方案需回退该子项并记录。

## 5. 发布顺序与回滚

**先下游、后上游**（保证任何时刻不会出现「网关收了音频但下游丢掉」）：

0. **前置条件**：Worker 必须先落地 D4 的「无素材 → 字面保留」规则（否则 Worker 先发布时，存量带 `@audioNN` 的单会在填充阶段失败）。
1. Worker（`cron_video_brach`）：支持多视频 + 音频上传与绑定；此时 ArcReel 仍过滤音频，行为不变。
2. ArcReel：保留音频并校验上限；此时 new-api 仍丢弃音频，行为不变。
3. new-api：开始向客户接收并下发音频；双机同 SHA 发布（遵循 AGENTS.md 双机同版本硬约束）。
4. 前端/文档：模型说明与渠道契约同步（不涉及价格）。

回滚按相反顺序：先回 new-api（重新静默丢，客户侧回到旧口径），再回 ArcReel，最后回 Worker。**不允许只回上游不回下游**（会重新出现静默丢）。

## 6. 验收清单

- 单测：new-api 矩阵 + 时长合计边界（视频/音频 15s 放行、16s 拒）+ 音频不计费；ArcReel 参考素材过滤；Worker mention/params/分组。
- 契约测试：同一请求在三端日志里 `inputs` 顺序与数量一致（image→video→audio）。
- 真机（零额度）：上传标定 + `mention-prompt:ok:text=...,refs=N`（N = 提示词引用总数）。
- 真机（需用户单独授权并确认费用）：1 条「2 图 + 1 视频 + 1 音频 + @ 绑定」生成，核对成片引用生效、账单 = 生成时长 + 参考视频合计秒（音频不额外计费）。
- 回归：不带音视频的纯图/纯文生单，`inputs` 与历史完全一致（无新增空组）；带 1 个视频的存量单计费与今天一致（合计 ≤15s 时口径等价）。

## 7. 风险与未决

1. **官网音频行为未真机标定**：上限已按用户口径定为 5（官网公开 10）；但 `@` 弹层是否列出音频项、音频是否与图/视频共用 file input，必须真机确认（决定「位置绑定」是否可实现）。
2. **音频时长解析的格式覆盖**：mp3 / m4a / wav 之外（如 aac 裸流、flac）解析失败会 400 拒单。上线前用客户实际会传的格式抽样验证，必要时先放宽为「解析失败只警告」——但这与「合计 ≤15s」口径冲突，需用户拍板。
3. **视频总量校验改造有回归面**：现有 `ProbeAndEnforceReferenceVideos` 是逐条上限，改成合计上限后，单条 15s 的存量单行为不变，但 2 条 8s 的旧请求此前会被拒、现在会放行 —— 属预期放宽，测试里要显式锁定。
4. **多素材解码耗时**：5 条视频 + 5 条音频会拉长上传确认窗口，Worker 现有单次 12s 确认需按素材数动态放宽或改为逐条确认（实现时验证）。
5. **音频与「生成音频」开关的叠加语义**：参考音频是内容参考，不是输出音轨；需在交付说明中写清「成片声音仍由官网生成/客户开关决定」。
6. **客户端提示词分布未知**：存量提示词可能只写 `@audioNN` 而从不带音频素材（本次事故单即如此）。D4 的「无素材 → 字面保留」保证不误杀，但这类单的音频引用永远不会生效，应在上游提示词组装端修正（另行排期）。
7. 旧注释与文档存在「视频显式 400 / 音频静默丢」的过时口径，实施时一并清理，避免下一位维护者按旧注释改回去。
