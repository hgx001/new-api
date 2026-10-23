# MegaAI 渠道接入 wan-3.0（2026-10-22）

## Context

把第三方 new-api 实例 **MegaAI**（`https://newapi.megabyai.cc`）接进本站网关，
在已有渠道类型 `ChannelTypeMegaAI = 64`（原先只挂了 `sd-2.5`，且生产从未建过该渠道）
上新增对外模型名 **`wan-3.0`**（通义万相 Wan3.0 满参版）。

上游接入文档：`https://newapi.megabyai.cc/docs`（文档正文在 `/assets/guide-E0rvbq2n.md`，
前端 bundle 里 `/docs` 路由懒加载 `assets/index-C_cWUtVR.js`）。

## 上游事实（2026-10-22 实测 / 读价目表确认）

| 事实 | 依据 |
|---|---|
| 上游是 new-api 实例，视频走 openai-video 协议 | `POST /v1/videos` 提交、`GET /v1/videos/{id}` 轮询、`GET /v1/videos/{id}/content` 下载 |
| `wan-3.0` 确实在售 | `GET /v1/models` 返回 `wan-3.0` / `wan-3.0-480p` / `wan-3.0-720p` / `wan-3.0-1080p` |
| `POST /v1/videos {"model":"wan-3.0","prompt":""}` → 400 `invalid_request` | 证明该模型走的就是 `/v1/videos`，不是聊天接口 |
| 上游价目：**480p ¥1.6 / 720p ¥1.8 / 1080p ¥2.0，按「次」计** | `GET /api/pricing` 的 `wan-3.0` 条目：`resolution_price_units = {480p: call, 720p: call, 1080p: call}`，`quota_type=1` |
| 价目表单位是**元**不是美元 | `GET /api/status`：`usd_exchange_rate=1`、`quota_display_type=CNY` |
| 时长 5/10/15/30 秒，参考图 ≤10、参考视频 ≤5、参考音频 ≤5，全比例，提示词 ≤5000 字符 | 上游价目表 `description` |
| 上游已不再提供无后缀的 `sd-2.5` | `GET /v1/models` 只剩 `sd-2.5-720p` 等带档位后缀的型号 |
| 上游任务回执带计费字段：`billing_amount` / `billing_currency` / `billing_quota` / `billing_status`，以及 `metadata.upstream_task_id` | `GET /v1/videos/{task_id}` 实测回执 |
| 上游任务 id 形如 `task_<32位>`，且**只有带 `task_` 前缀才查得到** | 直查无前缀会返回 `task_not_exist` |

⚠️ **「按次」与时长的关系**：上游价目表的单位是 `call`（不是 `second`），
所以 30 秒和 5 秒同价。这一条是定价的地基，**2026-10-22 用 1 个 30 秒 720p 真机任务
复核过上游实扣**（30 秒 720p 实扣 ¥1.8，与价目表 720p 单价分毫不差 → 见文末「真机验证」）。

## 决策

| 项 | 决定 |
|---|---|
| 对外模型名 | `wan-3.0`（与官网渠道的 `wan3.0-video` / `wan3.0-video-官网` 区分开，不重名） |
| 计价 | **按次 + 分辨率档**。480p ¥3.0 / 720p ¥3.5 / 1080p ¥4.0（上游成本 ¥1.6/1.8/2.0） |
| 时长 | **不参与计价**（上游按次）。客户端给多少就转发多少，不吸附、不拒绝 |
| 分辨率 | 必须归一到 480p/720p/1080p 三档；**认不出来一律 400**，不猜档位 |
| 默认分辨率 | 480p，且**显式下发**给上游（保证「我们收的价」=「上游收的价」） |
| 参考素材 | 翻译成上游统一参数 `referenceImages` / `referenceVideos` / `referenceAudios` |
| 渠道 | 复用 `ChannelTypeMegaAI = 64`，生产新建渠道（此前生产没有该渠道） |

### 为什么默认 480p 而不是 720p

上游按次计费、时长不参与。若客户端不指定分辨率而我们不显式下发，
上游可能按自己的默认档（未知）计价，而我们按 480p 收钱 → **少收**。
显式下发最低档就把这个风险消掉了。

## 实施步骤

### 1. 适配器包 `relay/channel/task/megabyai/`

既有结构是「facade 包 Sora 适配器」，本次覆盖三处：

- `EstimateBilling`（`adaptor.go`）
  - `sd-2.5` → `nil`（固定按次价，禁止带入 Sora 的 seconds/size 倍率）
  - `wan-3.0` → `{"size": <分辨率档倍率>}`，**没有 seconds**（上游按次计费）
  - 档位在 `ValidateRequestAndSetAction` 阶段解析好写进 context，
    计费复用同一个值 → 不会出现「计费档位 ≠ 下发档位」
- `BuildRequestBody`（`wan30.go`）
  - `wan-3.0` 走上游统一参数契约，**不透传原始 JSON**（上游文档明确不要与
    `seconds`/`size`/`image`/`input_reference` 混用）
  - 其余模型沿用 Sora 的透传行为
- `ParseTaskResult`（`adaptor.go`）
  - 成片地址兼容 `video_url` / `url` / `result_url` / `data.url` / `data.video_url` /
    `output.video_url` 等多种位置
  - 错误字段兼容对象 / 字符串 / `fail_reason`
  - 状态兼容 `succeeded` 等非标准终态
  - **认不出的非空状态按「还在跑」处理**：误判失败会让用户白等一次生成，
    误判成功会把没出的片子当成功交付；轮询超时有兜底退款
  - 不用 Sora 的严格解析当唯一来源：严格解析一失败，任务就永远停在非终态
    （既看不到失败原因，也永远退不了款）

### 2. 入参映射（本站通用入参 → 上游统一参数）

| 本站 | 上游 |
|---|---|
| `prompt` | `prompt` |
| `duration` / `seconds` | `duration`（整数；`seconds` 数字与字符串两种写法都收） |
| `resolution` / `metadata.resolution` / `size`(WxH) | `resolution`（`480p`/`720p`/`1080p`） |
| 原始请求体 `ratio` / `metadata.ratio` / `metadata.aspect_ratio` / `size` 推断 | `ratio` |
| `images` / `image` / `input_reference` / `media[type=reference_image\|first_frame\|last_frame]` | `referenceImages` |
| `media[type=reference_video]` | `referenceVideos` |
| `audios` / `audio` / `media[type=reference_audio]` | `referenceAudios` |
| `seed` | `seed`（省略时不下发，上游自己随机） |

素材数量超上游上限（图 ≤10 / 视频 ≤5 / 音频 ≤5）本地 400；
素材地址必须是**公网 http(s) URL**（上游要自己去抓），`data:` / `blob:` / 本地路径本地 400。

### 3. 定价与白名单

- `setting/ratio_setting/model_ratio.go`：`"wan-3.0": 3.0 / USD2RMB`（480p 基准，内部 USD 等值存储）
- `relay/channel/task/megabyai/constants.go`：`wan30ResolutionSizeRatio = {480p: 1, 720p: 3.5/3.0, 1080p: 4.0/3.0}`
  （倍率写成「档位价 ÷ 基准价」而不是魔数，改基准价时一眼能看出该改成多少）
- `model/pricing.go`：`isAllowedPricingModel` 放行 `wan-3.0`（上模型广场）
- `web/default/src/features/pricing/lib/model-helpers.test.ts`：`wan-3.0` 登记进**按次**穷举表
  （这张表漏登记过一次就会把「按次」显示成「/秒」，属于静默错价）
- ⚠️ **生产 `ModelPrice` option 会覆盖代码默认值**：改价必须同时改 option

### 4. 交付链路（`controller/video_proxy.go`）

新增 `ChannelTypeMegaAI` 分支：

- 轮询响应里带成片直链 → 直接透传，**不带渠道密钥**（避免把凭证发给第三方 CDN）
- 没有直链（轮询拿不到地址时 ResultURL 落成站内代理地址）→ 回源
  `{base}/v1/videos/{upstream_task_id}/content`，带渠道密钥
- ⚠️ 必须用 `isSelfProxyPath` 排除站内代理地址，否则代理会自我递归
- ⚠️ 上游给的直链是 Cloudflare R2 **签名 URL（24 小时过期）**，本站不缓存字节 →
  过期后 `/content` 取不到片，见文末「真机验证」的交付有效期说明

### 5. 生产上线

1. 先发版后端：`deploy/deploy-backend.sh <sha>`（**顺序不能反**：反序会出现
   「模型可见但一调就坏」）
2. 管理 API 建渠道（type 64，base `https://newapi.megabyai.cc`，
   models `wan-3.0`）—— 走管理 API 而不是裸 SQL，因为能力走内存缓存
3. 生产 `ModelPrice` option 加 `wan-3.0`
4. `models` 表建卡片（模型广场显示名/描述/厂商）

## 别名模型 `seedance2.5-特惠`（2026-10-22 新增）

同一渠道（`ChannelTypeMegaAI = 64`）下的第二个对外模型名，
**上游仍然是 `wan-3.0`**，参数传递与 wan-3.0 **完全一致**，只有对外价格口径不同。

| 项 | wan-3.0 | seedance2.5-特惠 |
|---|---|---|
| 上游模型 | `wan-3.0` | `wan-3.0`（渠道 `model_mapping` + 代码兜底表） |
| 参数传递 | 统一参数契约 | **完全一致**（同一套翻译代码） |
| 对外价 | 480p ¥3.0 / 720p ¥3.5 / 1080p ¥4.0 | **固定 ¥3.0/条** |
| 计费倍率 | `{"size": 档位倍率}` | **无**（`EstimateBilling` 返回 nil） |
| 分辨率 | 三档都支持（默认 480p） | **只支持 720p**（默认 720p，别的档位 400） |
| 时长 | 5/10/15/30 秒都支持 | **只支持 30 秒**（默认 30 秒，别的时长 400） |

⚠️ **锁定档位必须 fail-loud**：客户端要 1080p 或 5 秒时**明确 400**
（`invalid_resolution` / `invalid_duration`），不能静默改档 ——
静默把 1080p 降成 720p = 用户付了钱拿不到想要的清晰度；
静默把 5 秒拉成 30 秒 = 客户端以为省了钱，结果账单按次一样。
与 MiniMax 通道「能力门控一律 fail-loud」口径一致。

实现：`megabyaiLockedTierModels`（模型 → 锁定档位表），
`resolveLockedResolution` / `wan30LockedDuration` 做门控，
`ValidateRequestAndSetAction` 与 `BuildRequestBody` 走同一套判定（不会两处不一致）。
**只作用于别名模型**，wan-3.0 的三档分辨率与多档时长不受影响。

实现要点：

- `megabyaiUnifiedContractModels`：走统一参数契约的**精确集合**
  （⚠️ 不能用前缀匹配：站内还有 `seedance-2-5` / `seedance-2.5` / `seedance2.5特惠版`
  等一堆相近名字，前缀会误命中别家渠道的枚举表）。
- `megabyaiResolutionTierModels`：按分辨率档计价的模型集合，目前只有 `wan-3.0`。
- `megabyaiUpstreamModels`：对外名 → 上游名的**兜底表**。渠道 `model_mapping` 生效时以
  渠道为准；渠道漏配时靠这张表兜底，**别名绝不会原样发给上游**（上游不认识中文别名，会 400）。
- 计价：`defaultModelPrice["seedance2.5-特惠"] = 3.0 / USD2RMB`（USD 等值存储）。

成本口径：上游按**实际下发的分辨率档**结算（480p ¥1.6 / 720p ¥1.8 / 1080p ¥2.0）；
别名模型锁死 720p → 成本 ¥1.8/条，我们固定收 ¥3.0 → 毛利 40%，不会亏。
（2026-10-22 上线时对外价是 ¥5.0/条，同日 wan-3.0 档位价上调后于 2026-10-23 下调到 ¥3.0/条。）

上线步骤（与 AGENTS.md「模型改名/调价」口径一致）：**先发版**（代码里要有这个模型）→
再改渠道（管理 API 加 `models` + `model_mapping`，自动重建能力行）→ 改 `ModelPrice` option
→ 建 `models` 广场卡片。顺序反了会出现「模型可见但一调就坏」。

### 生产上线记录（2026-10-22）

- 后端 commit：`f5699fdd`（别名模型上线）+ `b93a7986`（档位锁定：只出 720p / 只出 30 秒）
  （部署镜像内已验证含 `seedance2.5-特惠` 字面量）
- 渠道 24：`models = wan-3.0,seedance2.5-特惠`、
  `model_mapping = {"seedance2.5-特惠":"wan-3.0"}`、`status=1`、密钥未变
  （len=51 / md5=634b4ecc8476f75d516bd0204116c065）
- 能力：`seedance2.5-特惠` 与 `wan-3.0` 均 `enabled=true`（default 组）
- `ModelPrice` option（2026-10-22 上线时）：`"seedance2.5-特惠": 0.6849315068493151`（= ¥5.00/条）；
  2026-10-23 调价后为 `0.41095890410958904`（= ¥3.00/条），见下「调价记录」
- 广场卡片已建；`/api/pricing` 与 `/v1/models` 均已列出（endpoint=openai-video）
- ⚠️ **未跑真实任务**（用户未授权）：功能未端到端验证。首单需人工核对
  「下游报 seedance2.5-特惠、上游实际是 wan-3.0、成片正常」。
- 前端改动（渠道类型白名单 + 按次穷举表）**无需单独发版**：
  `supportedModels` 在前端没有任何消费方（死配置），且线上旧前端的按秒判定
  对本模型名不匹配 → 广场本就显示为按次。

### 调价记录（2026-10-23）

- `seedance2.5-特惠` 对外价 **¥5.0/条 → ¥3.0/条**（按次，不变分辨率/时长口径）
- 改价三处同步：`setting/ratio_setting/model_ratio.go` 的 `defaultModelPrice`
  （`5.0 / USD2RMB` → `3.0 / USD2RMB`）、`relay/channel/task/megabyai/seedance25_tehui_test.go`
  的价格期望值、生产 `ModelPrice` option
  （`0.6849315068493151` → `0.41095890410958904` = ¥3.00/条）
- 生产脚本：`scripts/prod-reprice-megabyai-seedance25-tehui-20261023.sql`
  （同事务改 option + 广场卡片文案）
- 实收：205479 quota（¥3.00/条，QuotaPerUnit=500000，USD2RMB=7.3）
- 上游成本不变：别名锁 720p → ¥1.8/条，毛利 40%
- ⚠️ 生产 option 覆盖代码默认值，只改代码线上不变（反之亦然）

## 别名模型的「遮上游」交付链路（2026-10-23）

别名模型 `seedance2.5-特惠` 的存在意义是「下游看不出真实上游是 `wan-3.0`」。
上线后逐面核对，发现**成片直链是最后一个残留面**，本轮用 TDD 做掉。

上游回执（`task.Data`，实测自任务 619）里同时带着三样东西：

```json
{"model":"wan-3.0","url":"https://d4c280ea2c37b44805c584296cb271a0.r2.cloudflarestorage.com/hailuo-video/aggregate/videos/vid_xxx.m4v?X-Amz-Signature=...",
 "metadata":{"final_video_url":"https://...r2.../hailuo-video/...","video_url":"https://...r2.../hailuo-video/..."},
 "billing_amount":1.8,"billing_currency":"CNY","billing_status":"settled"}
```

- R2 域名 + `hailuo-video` 货源目录 → 直接暴露上游存储与货源链；
- `"model":"wan-3.0"` → 直接点名真实上游；
- `billing_amount` → 暴露上游成本。

本轮的处理（**只在出口层做，数据库保留原文**）：

| 下游可见面 | 处理 |
|---|---|
| 任务 DTO `result_url` | 改指向本站 `/v1/videos/{task_id}/content` 代理；未完成/失败任务置空（失败文案会回退进 `result_url`，那也是上游原文） |
| OpenAI Video 取任务响应体 `GET /v1/videos/{task_id}` | 别名模型按对外口径**重建**（`relay/relay_task.go` 的 `redactAliasOpenAIVideo`）：对外模型名 + 本站代理地址，无上游直链/成本/任务号 |
| 任务 DTO `fail_reason` / 提交失败错误体 / 错误日志 `content` | `RedactUpstreamText`：上游字样换中性词 + **上游地址整体摘掉**（换 `[地址已隐藏]`） |

**代价**：成片改走本站代理 = 本站多一跳出网带宽（32MB/条量级，¥0.02/条量级），
且 `/content` 需要鉴权（会话或 token 都行，`middleware.TokenOrUserAuth`）。
`wan-3.0` 本尊（对外原名）**不受影响**，仍然直链透传。

代理行为本身没变：`controller/video_proxy.go` 的 `ChannelTypeMegaAI` 分支读的是
**库里的原始直链**（管理员在 `/api/task/` 仍看得到），回源逻辑照旧。

## 风险与注意

- ⚠️ **上游实际扣费以「每次固定价」为准**。若上游改成按时长计价（当前价目表是
  `call`，不是 `second`），本站按次定价会亏。真机验证就是盯这一条。
  （2026-10-22 真机：30 秒 720p 实扣 ¥1.8 = 价目表 720p 单价，**当前口径成立**；
  但上游改价时这条要重新验。）
- ⚠️ **本站这把 key 的分组是 `auto`（智能分组）**：上游可能用别的分组的折扣结算
  （上游有 0.2 折的分组），实际成本可能低于价目表。真机验证时按上游余额差值对账。
  （2026-10-22 真机回执 `billing_amount=1.8` = 价目表原价，**这次没吃到折扣**。）
- ⚠️ **参考素材字段名未在真机上验证过**：上游文档写的是 `referenceImages`，
  本 adaptor 按文档翻译；若上游实际只认经典字段（`images`/`media`），
  参考素材会被静默丢弃。首个带参考图的任务要人工核对成片
  （本次真机是纯文生视频，**没覆盖到这条**）。
- `sd-2.5` 上游已无同名型号，生产渠道只挂 `wan-3.0`；代码里保留 `sd-2.5`
  是不破坏既有路径（youkou 渠道的按次计费覆盖也复用这个包）。
- ⚠️ **渠道密钥里混进 BOM 会连锁触发 AutoBan**（2026-10-22 实测）：
  用 PowerShell 管道把密钥写进服务器文件时会带 UTF-8 BOM → 渠道里存的是
  带 BOM 的 key → 上游返回 401「无效的令牌」→ **渠道被自动禁用** →
  后续请求报「分组 default 下模型 wan-3.0 的可用渠道不存在」。
  现象离根因很远，排查时先核对渠道 key 的 md5/长度；
  恢复用 `scripts/restore-channel.sh <id>`（status 与 abilities.enabled 必须一起改）。
  `scripts/create-megabyai-channel.sh` 现在会直接拒绝含非 ASCII 字符的密钥文件。

## 真机验证（2026-10-22，1 个授权任务）

用户明确授权「跑 1 个 30 秒 720p」，端到端跑通并完成账目核对。

| 项 | 实测值 |
|---|---|
| 提交 | `POST /v1/videos`：`{model: wan-3.0, duration: 30, ratio: 16:9, resolution: 720p}` |
| 本站任务 | `task_W6qbKbmn4LuExWRTapnwnvS08ukY9uDx`（tasks id=619，channel 24，platform 64） |
| 上游任务 | `task_36be6a9c56a14eebbeb1573c43abbf7e`（上游内部 vid_28c43b66c6495219c5bd01） |
| 上游计费回执 | `billing_status=settled`、`billing_amount=1.8 CNY`、`billing_quota=900000` |
| 本站计费 | quota=205478（= 164383 × 1.25）；`per_call_billing=true`、`other_ratios={"size":1.25}`、**没有 seconds** |
| 用户余额 | 500000 → 294522，差值正好 205478 → 预扣=实收，无退款、无补扣 |
| 交付 | `GET /v1/videos/{task}/content` → 200 / 32,773,068 bytes / `video/mp4`（magic `ftypisom`） |
| 耗时 | 985 秒（submit_time → finish_time） |

三条结论：

1. **「按次、时长不参与」被证实**。30 秒 720p 的上游实扣 ¥1.8 与价目表 720p 单价
   完全一致，没有任何按时长加价的成分 → 本站「按次 + 分辨率档」定价不会因时长亏钱。
2. **计费档位与下发档位一致**。`billing_context.other_ratios` 只有 `size:1.25`（720p 档）、
   没有 `seconds`，与 `EstimateBilling` 的设计（wan-3.0 只给 size）一致，
   不会出现「收 720p 的钱、下发 480p」。
3. **交付链路通**。`/content` 代理能拿到合法 MP4（magic `ftypisom`）。

ℹ️ 上面表格里的本站实扣（205478 quota = ¥3.00/次、`size:1.25`）是**调价前**的历史快照：
2026-10-22 晚间把 wan-3.0 的档位价由 ¥2.4/3.0/4.0 上调到 **¥3.0/3.5/4.0**，
倍率同步改为 3.5/3.0 与 4.0/3.0。上游回执（billing_amount 1.8 / settled）不受影响，
仍是上游成本口径的凭据。

⚠️ **交付有效期（新增注意事项）**：上游成片是 Cloudflare R2 的**签名直链**
（`X-Amz-Expires=86400`，即签发后 24 小时过期）。本站把该直链存进
`private_data.result_url` 并直接透传（不带渠道密钥），**本站不缓存视频字节** →
用户要在 24 小时内下载，超期再取会 403；要长期留存得另做「转存到本站对象存储」。

台账：生产 `logs` type=6 / id=8033（`OPERATOR-COST-ACCOUNTING`），
记录客户侧 205478 quota（¥3.00）与上游侧 ¥1.8 settled 两个口径。

## 参数传递审查（2026-10-22，纯静态 + 单元测试，未生成任何视频）

真机只覆盖了文生视频（prompt / duration / ratio / resolution / seed 五个字段），
所以另外按「客户端原始 JSON → 本站解析 → 上游提交体」整条链做了一次代码级审查
（`relay/channel/task/megabyai/wan30_params_test.go`，22 个参数用例，全程本地计算、
不发上游请求、不产生计费）。

**结论：主链路正常。** 18 个参数映射用例全绿，且真机实测与之一致：

| 客户端写法 | 上游收到 | 证据 |
|---|---|---|
| `duration: 30` / `duration: "30"` / `seconds: "15"` | `duration: 30 / 15` | 单测 + 真机成片 30.02s |
| `resolution: "720p"` | `resolution: "720p"` | 单测 + 真机成片 1280×720 |
| `size: "1920x1080"` | `resolution: 1080p` + `ratio: 16:9` | 单测 |
| 顶层 `ratio: "16:9"` | `ratio: "16:9"` | 单测 + 真机 16:9 |
| `metadata.resolution` / `metadata.ratio` | 同名字段 | 单测 |
| `images` / `media[...]` / `audios` | `referenceImages` / `referenceVideos` / `referenceAudios` | 单测 |
| `seed` 数字与字符串 | `seed: 42` | 单测 |

审查中查出并修掉 3 个问题：

1. **数字型 `seconds` 直接 400**（全局，影响所有任务渠道）。`TaskSubmitReq.Seconds`
   是 `string`，客户端传 `{"seconds": 15}` 会让整包反序列化失败 →
   客户端收到 `400 invalid_json`（"cannot unmarshal number into ... of type string"），
   表现为「参数根本没传进去」而不是「参数写错了」，极难定位。
   修法：`relay/common/relay_info.go` 的 `UnmarshalJSON` 按 `duration`/`seed` 的同款口径
   兼容数字与字符串；字符串写法行为完全不变。
2. **`input_reference` 会覆盖整个 `images` 数组**（静默丢参考图）。平台是 Sora 语义
   （`input_reference` 是单图字段，存在就替换 `images`，见
   `relay/common/relay_utils.go` 的 `validateMultipartDirect`）。
   wan-3.0 支持 10 张参考图，两者并存时丢图 = 用户付了 ¥3 却没带参考图。
   修法：`megabyai` 适配器在 `ValidateRequestAndSetAction` 里从原始 body 把 `images`
   找回来与 `input_reference` 合并（只在本渠道生效，不动平台语义）。
3. **非 http(s) 素材地址直接透传给上游**。上游文档写的是「参考图 URL」，
   它要自己去抓；`data:` / `blob:` / 本地路径对它不可达 →
   任务跑完才失败（用户付了钱还等了一轮生成）。
   修法：素材地址必须是公网 http(s) URL，否则本地 400
   （与 manwu / wan3 通道的既有约定一致）。

⚠️ **仍未被任何验证覆盖的**：`referenceImages` / `referenceVideos` / `referenceAudios`
这三个字段名是否被上游真正识别（上游文档如此写，但没跑过带参考素材的真机任务），
以及 `seed` 是否原样生效。首个带参考图的任务仍需人工核对成片。
