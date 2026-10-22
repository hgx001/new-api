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
复核过上游实扣**（见文末「真机验证」）。

## 决策

| 项 | 决定 |
|---|---|
| 对外模型名 | `wan-3.0`（与官网渠道的 `wan3.0-video` / `wan3.0-video-官网` 区分开，不重名） |
| 计价 | **按次 + 分辨率档**。480p ¥2.4 / 720p ¥3.0 / 1080p ¥4.0（上游成本 ¥1.6/1.8/2.0，1.5 倍） |
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
| `duration` / `seconds` | `duration`（整数） |
| `resolution` / `metadata.resolution` / `size`(WxH) | `resolution`（`480p`/`720p`/`1080p`） |
| 原始请求体 `ratio` / `metadata.ratio` / `metadata.aspect_ratio` / `size` 推断 | `ratio` |
| `images` / `image` / `input_reference` / `media[type=reference_image\|first_frame\|last_frame]` | `referenceImages` |
| `media[type=reference_video]` | `referenceVideos` |
| `audios` / `audio` / `media[type=reference_audio]` | `referenceAudios` |
| `seed` | `seed` |

素材数量超上游上限（图 ≤10 / 视频 ≤5 / 音频 ≤5）本地 400。

### 3. 定价与白名单

- `setting/ratio_setting/model_ratio.go`：`"wan-3.0": 2.4 / USD2RMB`（480p 基准，内部 USD 等值存储）
- `relay/channel/task/megabyai/constants.go`：`wan30ResolutionSizeRatio = {480p: 1, 720p: 1.25, 1080p: 1.666667}`
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

### 5. 生产上线

1. 先发版后端：`deploy/deploy-backend.sh <sha>`（**顺序不能反**：反序会出现
   「模型可见但一调就坏」）
2. 管理 API 建渠道（type 64，base `https://newapi.megabyai.cc`，
   models `wan-3.0`）—— 走管理 API 而不是裸 SQL，因为能力走内存缓存
3. 生产 `ModelPrice` option 加 `wan-3.0`
4. `models` 表建卡片（模型广场显示名/描述/厂商）

## 风险与注意

- ⚠️ **上游实际扣费以「每次固定价」为准**。若上游改成按时长计价（当前价目表是
  `call`，不是 `second`），本站按次定价会亏。真机验证就是盯这一条。
- ⚠️ **本站这把 key 的分组是 `auto`（智能分组）**：上游可能用别的分组的折扣结算
  （上游有 0.2 折的分组），实际成本可能低于价目表。真机验证时按上游余额差值对账。
- ⚠️ **参考素材字段名未在真机上验证过**：上游文档写的是 `referenceImages`，
  本 adaptor 按文档翻译；若上游实际只认经典字段（`images`/`media`），
  参考素材会被静默丢弃。首个带参考图的任务要人工核对成片。
- `sd-2.5` 上游已无同名型号，生产渠道只挂 `wan-3.0`；代码里保留 `sd-2.5`
  是不破坏既有路径（youkou 渠道的按次计费覆盖也复用这个包）。
- ⚠️ **渠道密钥里混进 BOM 会连锁触发 AutoBan**（2026-10-22 实测）：
  用 PowerShell 管道把密钥写进服务器文件时会带 UTF-8 BOM → 渠道里存的是
  带 BOM 的 key → 上游返回 401「无效的令牌」→ **渠道被自动禁用** →
  后续请求报「分组 default 下模型 wan-3.0 的可用渠道不存在」。
  现象离根因很远，排查时先核对渠道 key 的 md5/长度；
  恢复用 `scripts/restore-channel.sh <id>`（status 与 abilities.enabled 必须一起改）。
  `scripts/create-megabyai-channel.sh` 现在会直接拒绝含非 ASCII 字符的密钥文件。
