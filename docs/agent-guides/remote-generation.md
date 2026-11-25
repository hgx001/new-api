# 漫屋远端生成与三项目契约

涉及 `relay/channel/task/manwu/`、ArcReel/Worker 联调、参考图或产物代理时必读。公开价格和当前启用渠道查实际配置；不把源码枚举、旧账号状态或某次成功率当生产现状。

## 自家三项目与请求链

| 项目 | 本机位置 | 职责 |
|---|---|---|
| 燃境 Ai App | `C:/work/即梦网站/cron_video_brach` | Electron Worker，驱动各平台官网 |
| ArcReel（漫屋） | `C:/work/即梦网站/ArcReel` | 建单、派单、钱包、托管；`manwu-agent` 是 gitee 镜像 |
| new-api（本仓库） | `C:/work/new-api-src`；维护目录 `C:/work/维护newapi` | 对外 API 网关与计费 |

漫屋渠道指向自家 `https://arcreel.heibaidao.cn`，本网关为 `https://api.heibaidao.cn`，不是两个外部服务商。

```text
客户端 POST /v1/videos 或工作台 POST /v1/video/generations
  → 本渠道校验/建单 → ArcReel /api/v1/remote-generation/jobs
  → assign_job（<platform>.dom）→ Worker WSS → 官网
  → 下载/必要后处理/托管 → 本网关代理产物
```

- 轮询 `/v1/videos/{id}`；`/v1/video/generations` 不能用于轮询。请求来源查 `logs.other.request_path`。
- 用户→new-api 用网关令牌；本渠道→ArcReel 用 `REMOTE_OPENAPI_SERVICE_TOKEN`；Worker→ArcReel 用 Worker token，互不通用。
- service token 归 ArcReel `openapi-service`，仅 create/get/cancel/产物读取；不因排查把 Worker/客户权限混用。
- 开发 BaseURL 用本机 `http://127.0.0.1:1241`、Worker 开发 WSS+开发 token；生产配生产域与生产凭据。测试 App 显式接生产同样会接真实队列。
- 联调供应商还可能经过自家 `C:/work/sub2api`；按时间、模型、request/client ID 关联各层，不以自动选渠道/降级成功证明目标渠道可用。

## 三端同口径：素材与参数

| 层 | 实现入口 |
|---|---|
| 网关 | `relay/channel/task/manwu/adaptor.go` |
| ArcReel | `server/routers/remote_generation.py` 的参数归一与 `_prompt_reference_*` |
| Worker | `electron/remote/remoteReferenceGuard.ts`；`src/platforms/doubao/params.ts` |

- 位置式引用只认 `@图片N` / `@图N`，期望取最大编号；`@imageNN` 是素材名 token，不按数字当位置，只参与“引用图但零图”的兜底。
- `@` 前为 ASCII 字母/下划线不算引用，数字前缀可用；`@图1@图2` 不漏后一个。多带图不拦；自然语言“参考图N”当前不触发位置闸，扩规则须三端同批改。
- 参考图别名必须显式覆盖 snake_case 与 camelCase：`input_reference/images/image/image_url/image_urls/reference_image/reference_images/input_images`，以及 `inputReference/imageUrl/imageUrls/referenceImage/referenceImages/inputImages`。
- Go JSON 忽略大小写但不忽略下划线，不能假设 `referenceImages` 会匹配 `reference_images`。漏字段会静默丢图，不是客户端没传图的充分证据。
- 网关不保存完整原始请求体，必须保留 `manwu 参考图解析：model/source/count` 的来源/数量日志；取证不记录素材签名 URL 或凭据全文。
- 缺图按网关解析 → ArcReel `inputs_json` → Worker 文件逐层核实，不把官网要求补图误当账号问题去换号重投。

## 模型、档位与对外能力

路由以 `modelSpecs` 为源码真相源，新增形态沿用当前表驱动，不为每个模型新开 adaptor；未知模型拒绝，不默认落 dola。

- dola 公共名是 `seedance-2.0`（点号）；豆包是 `seedance-2-0-fast`（连字符；`seedance-2-5` 已于 2026-11-12 迁出豆包链路，常驻视频生成车站），转发时翻译成 `doubao-*` 官网真名。
- `seedance-2.5` 的 dola 30s 已按既定业务下架/退役；旧代码还存在表项不等于可售能力，不据此恢复 channels/abilities 或承诺 Worker 30s。
- dola（`seedance-2.0`）时长为 5–15 秒区间：区间内任意整数秒原样透传、不吸附，区间外（含 30s）400 invalid_duration，缺省 15；按次价与时长无关。
- 豆包时长原样透传、不吸附不改写：`seedance-2-0-fast` 4–30 秒、`seedance-2-5-flash` 4–30 秒（2026-11-23 由 4–15 放宽，同时改为按条 ¥4.0）；未指定比例不编造，豆包不接受 resolution。`seedance-2-5` 自 2026-11-12 起走视频生成车站（ch27，按次 ¥3.50）：时长 2–30 整数、比例七档（`auto` 作为 `adaptive` 别名）、默认 720P。
- `gemini-web-video` 故意不叫官方 `veo-*`，避免渠道重名；Veo 不接受自定 seconds/duration/resolution，不能静默丢弃后假装执行。
- `Nano Banana Pro` 对应 ArcReel 抽象 `manwu-image`，内部实际站点由服务端路由；图片不接受视频参数。
- `jimeng-video-reverse` 恰好一段公网参考视频，文本产物、prompt 可选；不接受图片/时长/比例/分辨率或 multipart 本地文件直传。
- 万相图生/编辑分别由 `wan2.7-图生` / `wan2.7-编辑` 表项按素材形态区分，不能因同 platformId 就混用协议。详细限值查当前表和验证分支，不复制固定模型数量。
- CapCut（剪映国际版）另算一个站，**五档独立商品**：公开名 `cp-seedance-2.0mini` / `cp-seedance-2.0fast` / `cp-seedance-2.0` / `cp-seedance-2.5flash` / `cp-seedance-2.5`，上游真名 `seedance_2.0_mini` / `seedance_2.0_fast` / `seedance_2.0` / `seedance_2.5_flash` / `seedance_2.5`，platformId `capcut`。
  - **五档统一按秒**：EstimateBilling 对**全部五档**返回 `seconds` 倍率，720p 再乘 `size` 倍率（`capcut720PRatios`：mini/fast ×1.5，2.0/2.5/2.5flash ×2）。⚠️ 漏给任何一档 `seconds` 就会按「1 秒」收费。
  - ModelPrice 配 **480p 基准单价（元/秒）**，改价时 ModelPrice 与 `capcut720PRatios` 必须同时改。
  - 时长：2.5 / 2.5 Flash 4–30 秒，其余 4–15 秒。⚠️ 上界用**表驱动**（`capcutMaxDurations`）—— `cp-seedance-2.5flash` 含子串 `cp-seedance-2.5`，子串匹配会把它误判成 2.5 档（时长与分辨率倍率一起错）。
  - 分辨率只卖 480p / 720p；1080p / 4K 未定价 → 显式 400，**不静默降级**（客户按 1080p 下单却收到 480p = 错交付）。
  - 比例六档（不含官网 "Fit" 自适应）。
  - **参考素材（2026-11-15 口径）**：参考图 ≤30 张；参考视频 ≤5 条**且合计 ≤15s**（计入计费秒：`seconds = 生成时长 + Σround(每条)`）；参考音频 ≤5 条**且合计 ≤15s**（**不计费**）。载荷顺序固定 `image → video → audio`（Worker 按顺序算 `@imageN/@videoN/@audioN` 槽位）。⚠️ 视频限长是**合计**口径（旧版是逐条 ≤15s）：`2×8s` 这类旧请求会开始 400，属破坏性收紧。音频时长测不出时**放行**并告警（不参与计费，宁可放），视频测不出则 400（计费依赖）。超限一律显式 400，绝不静默截断。
  - `cp-` 前缀与豆包 `seedance-2-0-fast` 区分，**不做互为别名**。
  - **上架**：`models` 卡片 / `abilities` / `ModelPrice` 三处必须齐（直改 `channels.models` **不会**自动同步 `abilities`，漏了就表现为「模型不存在」）。⚠️ 双机生产库各自独立（`api.heibaidao.cn` / `api2.heibaidao.cn`），改库要**两台各执行一次**幂等脚本：`scripts/prod-upsert-capcut-models-20261114.sql`。
- 拒绝无法诚实实现的显式参数，本地400；不悄悄改规格、丢图或换档交付。
- 改公开名同步 ArcReel `_MANWU_MODEL_PATTERN` 与 Worker `heibaidaoModelCapabilities` / `modelProviderPresets`。正则不得误吃其它供应商相近名字；旧 db-* 不重新对外上架。

## 计费、结果与交付

- 计费细则见 [定价与目录](pricing-catalog.md)。按次返回 nil 倍率，Fast 按 seconds，图片按 n；倍率型模型不能放入按次跳过倍率的 `TASK_PRICE_PATCH`。
- 豆包 `seedance-2-5-flash` 是**按条一口价**（¥4.0/条，4–30 秒同价，固定 720p）：`EstimateBilling` 对本档必须返回 nil，按次认定同时要在 `constant.TaskPerCallModels` 登记 —— 两处缺一就会出现「名单说按次、adaptor 给了 seconds」的静默超收（¥4.0 × 30）。
- 托管相对 sourceUrl 需服务端鉴权，走本站 `/v1/videos/{task_id}/content`；公网 CDN 直链不得携带渠道/service key。
- 文本结果沿 `TaskInfo.ResultText → TaskPrivateData.ResultText → TaskDto.ResultText`，OpenAI 形态放 `metadata.prompt` 与 `media_type=text`，不能给空 URL 诱导客户端下载不存在的文件。
- 反解素材由 ArcReel 下载，提交可能耗时1–2分钟；不能为“快”压低网关超时导致“客户端已失败、上游却已建单”。
- 验交付用历史 ready 产物和正确用户鉴权；不因一次官网 something-went-wrong 就认定网关错误，也不能把过去验过的链路当永久不会出故障。
- 模型广场与调用能力分离，反解价格未核定前不因占位价对外展示。上限/参数变动同步边界测试与三仓实现，不只改常量。

## 素材直传 COS（`POST /v1/files`，2026-11-14 上线，端到端真机已验）

大素材走这条，不要走「网关代下载」：

```text
客户端带文件名 POST /v1/files
  → 网关按 manwu 渠道转发 ArcReel /api/v1/remote-generation/cos-upload/presign（service token）
  → 返回预签名 PUT URL（30 分钟）+ object_url（CDN）
  → 客户端直传腾讯云 COS（字节不过网关）
  → 建单时引用 object_url，ArcReel 从 CDN 拉取（TTFB 0.1–0.3s）
```

- 为什么不走网关中转：老机出口仅 3Mbps，境外素材源实测 67–79KB/s，而 ArcReel 代下载有
  **60s 硬总预算** —— 单图 >4MB 必 400。直传后无此限制（上限 25MB/对象，`FILES_PRESIGN_MAX_MB`）。
- 路由在 `router/video-router.go` 的 `filesV1Router`，**不在** relay-router
  （那里 `/v1/files` 其余方法仍是 `RelayNotImplemented` 占位）。TokenAuth，不挂 Distribute。
- 扩展名白名单两侧同源：`controller/files.go` 与 ArcReel `remote_cos_upload.py` 必须同步放宽/收紧。
- 实现：`controller/files.go` + `relay/channel/task/manwu/cos_presign.go`。密钥只在 ArcReel 侧，网关不持有。

## 两档 sd- 视频模型（2026-11-22 上架）

| 公开名 | 上游名 | 价格 | 参数 |
|---|---|---|---|
| `sd-seedance-2.0fast` | `tiktok-seedance-2.0-fast` | ¥0.10/秒 | 4–15s、720p、9:16、≤4 图 |
| `sd-seedance-2.0` | `tiktok-seedance-2.0` | ¥0.22/秒 | 同上 |

**两档参数完全一致**（2026-11-22 老板口径）：时长 4–15 秒逐秒可选、
分辨率固定 720p、画面比例固定 9:16、≤9 张参考图、按秒计费。
广场卡片描述按此口径写死，且**不出现渠道词与别家品牌词**。

⚠️ 时长**逐模型分档**（2026-11-22 老板口径）：2.0 系列 4–15 秒逐秒；
1.5 Pro（`tk-symphony-1.5pro`）仍是官网原生 5/10/12 秒。
new-api 侧校验在 `relay/channel/task/manwu/adaptor.go::tiktokDurationWhitelistFor`，
Worker 侧在 `src/platforms/tiktok/params.ts`（两处同源，改一处必须改另一处）。

- **公开名用 `sd-` 中性前缀**：模型 id 对用户可见，前缀不透露渠道（`cp-`=CapCut、`el-`=ElevenLabs）。
- **分辨率与比例服务端定死**（720p / 9:16），不对用户暴露选择；用户传别的值不采纳。
- **不设 size 倍率**（与 CapCut 的「480p 基准 × 720p 倍率」不同）：`ModelPrice` 直接就是最终单价，改价只改这一处。
- 能力组唯一来源 `constant/tiktok_family.go`；广场白名单查 `constant.IsTiktokFamily`，不要手写模型名。
- 上架脚本：`scripts/prod-upsert-sd-models-20261122.sql`（双机各跑一次，先发版再改库）。
- Worker 侧靠 `TIKTOK_MODEL_MATCHERS` 选官网下拉档位。⚠️ 官网同时有 `Seedance 2.0` 与 `Seedance 2.0 Fast`，标准档必须用负向断言排除 `fast`/`mini`，否则价差 2.2 倍静默错档。
