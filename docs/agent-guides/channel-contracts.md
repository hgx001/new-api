# 视频渠道的高风险协议差异

涉及渠道适配、规格、计费、结果解析或别名脱敏时必读；漫屋独立见 [远端生成](remote-generation.md)，价格操作见 [定价与目录](pricing-catalog.md)。档位以当前代码/官方协议核对，本文不复制现价和生产账号清单。

## 通用边界

- 同名家族不等于同协议：按 endpoint、payload、返回形状和计费维度分流，不能只按“视频模型”复用一份载荷。
- 参数先验证并持久到 context，再由构造/计费共用；未知模型/无法表达的规格默认 fail-loud，不静默换档。既有明确兼容例外不能在归档中偷偷收紧。
- 公网 http(s) 素材与 data/blob/本地路径不是一回事，上游要自己取资源的接口不能收到不可达地址。
- 新渠道核对 StreamOptions 支持并更新 streamSupportedChannels；请求可选 scalar 用指针+omitempty 保留显式0/false。
- 明确上游余额不足仍是上游错误，保留触发换渠道/AutoBan的原始语义；对外脱敏不污染内部重试分类。

## MiniMax / Hailuo：v1 与 v2

| | v1 | H3 v2 |
|---|---|---|
| 提交 | `/v1/video_generation`，扁平字段 | `/v2/video_generation`，`content[]` 多模态数组与 role |
| 轮询 | query `task_id` | `/v2/query/video_generation/{task_id}` 路径参数 |
| 成片 | 再 `/v1/files/retrieve?file_id=...` | 直接 `task.content.url` |
| 失败 | v1 base_resp | `type:error` + error 的 type/message/http_code，不含 base_resp |

- 轮询阶段可能没有 RelayInfo/model，按报文特征分流，不靠猜模型名。
- H3 / H3-Max 的分辨率和时长能力不同；首/尾帧与参考素材互斥，数量上限由 V2ModelSpec 校验，不静默降档。
- 纯文生须显式 ratio，不能传 adaptive；素材只收可被上游访问的公网 URL。
- Context-IR 端点 `/v2/h3_context_ir`，载荷 model 仍为 `MiniMax-H3`；返回 `content.prompt`，按文本结果交付，移除预置空 url。
- v2按有效时长与分辨率倍率计费，基础价是该模型最低档；Context-IR按次。不同模型基准档不能混用。
- Validate 阶段必须 `StoreTaskRequest`，否则构造拿不到参数，计费也可能静默只收1秒。
- `insufficient_balance_error/http_code=402` 保持上游失败身份，不包装成本端参数错误。

## MegaAI / wan-3.0 与别名

⚠️ **2026-10-26：wan-3.0 与别名 seedance2.5-特惠 已停售**（渠道 24「MegaAI-Wan3」整条删除）；`wan-3.0-特惠` 也已从渠道 20 摘除。三者 adaptor 常量与本页契约仍保留，但没有 ability 就没有路由 —— 本节仅作历史契约记录。

- 复用 `ChannelTypeMegaAI` 的 Sora facade，协议为 `/v1/videos` 提交、`/{id}` 轮询和 `/{id}/content` 下载。
- `wan-3.0` 与 `wan3.0-video` / `wan3.0-video-官网` 是不同公开名，不互相覆盖。别名 `seedance2.5-特惠` 实际上游仍是 wan-3.0，必须经 model_mapping 或兜底表翻译，不把中文别名原样发上游。
- 参数契约集合、分辨率分层计价集合、锁档集合使用**精确模型名**，不前缀匹配相近 Seedance 名。
- wan-3.0 **按次+分辨率档**，不是按秒；EstimateBilling只有size，没有seconds。归一失败400，未指定时用明确480p并显式下发，避免账实档位不同。
- 别名固定按次、无倍率，锁720p/30s；请求其它档位明确400，不静默降清晰度/拉时长。锁档只作用别名，不能带偏 wan-3.0。
- 参数翻译成上游 duration/ratio/resolution/referenceImages/referenceVideos/referenceAudios，不与 seconds/size/image/input_reference 混用，不透传原始JSON。
- 同时有 input_reference 和 images 时须按该渠道多图语义合并；不能让通用Sora单图解析覆盖整组参考图。素材上限与数字/字符串seconds兼容看当前验证代码，校验与下发同源。
- 结果兼容多个URL/失败字段及 succeeded 等状态；未知非空状态保守继续轮询，不误报成功/失败，也不能解析失败后永远卡非终态。
- 取片先排除本站代理路径防自递归；外部CDN直链不带渠道密钥，需回源上游/content时才带key。
- 上游R2签名直链曾为24h有效，本站不缓存字节；代理地址不等于永久托管。需要长期留存须另做转存，不能承诺永久可下。
- 上游实际回执的 billing_amount/currency/status 是实扣凭据，不用价表独证；查上游任务保留 task_ 等真实前缀，不裁剪ID。
- 详细接入证据见 [历史接入记录](../channel/megabyai-wan3-integration.md)；其中价格/上线状态是带日期记录，当前业务以代码、已授权配置和本指南为准。

### 别名的所有普通用户出口都要遮上游

数据库及管理员保留原文，只在HTTP/DTO/用户日志出口处理：

- 提交 model 改对外名；提交失败、fail_reason、消费/错误日志不漏真实上游名、渠道名或地址。
- Task DTO 的 upstream_model_name 改别名，data 回执不暴露上游任务号/成本/地址。
- result_url 和 OpenAI `GET /v1/videos/{id}` 的 URL/metadata 重建为本站 `/content`，失败/未完成不造产物地址；不能只改DTO漏掉原始OpenAI回执。
- 上游URL应整体摘除，不留半遮半露域名，也不把URL区段当受保护区域跳过脱敏；短词如 ali 要词边界，避免误伤 invalid/validate。
- 不在 TaskErrorWrapper、写入层或重试分类前改原文；否则余额错误换渠道失效且管理员无证可查。
- 只对明确别名集合生效，其它模型原样；验收用合成任务/日志比对普通用户与管理员出口，不跑付费生成。

## 三元巡狩：实时与拼车工作流不同

- 工作流名需与上游精确匹配（全角括号/空格等也有意义）；按模型选择工作流，不默认回落到拼车档。
- 实时 prime 不换成次日发车的 offline 工作流；慢速档是另一公开契约。不能为便宜上游把实时货交成次日货。
- 开放API不含参考视频/首尾帧字段时，卡片/教程不能继续承诺支持；存在于别家协议不等于此API能表达。
- prime历史契约固定30s/1080p、方向由ratio/size确定；其越界时长/分辨率原有兼容行为为忽略，不在归档中擅自改成400。其它模型分别按各自profile处理。
- 上游可售目录与本地更严的合并素材限制都要核对；搬渠道保留 group/priority/weight，自动同步不保证替你重建 abilities。

## Wan3 官网：参考视频参与计费

- 有参考视频时计费秒数 = 输出秒数 + 每段参考视频时长分别四舍五入后的和，不向上取整，音频不按这条相加。
- 视频时长探测在 validate 阶段，失败明确400；不能拖到无错误返回值的 EstimateBilling 后默默只收输出秒数。
- 参考视频最多5段，单段1–15s、总长≤15s；输入视频总时长+输出≤30s。参考音频数量/大小有上限，但本地目前不探音频时长，不把“上游会校验”写成“本地已覆盖”。

## wan3.0-720p-特惠 按能力分流到心笔（2026-11-06）

对外仍是同一个模型名、同一个价（¥0.20/秒）、同一个 720P 承诺，只换更便宜的上游。

- 心笔（channel 25，type 68）生成 480P + 交付前提升到 720P；官网 Wan3（channel 12，type 60）保留给心笔吃不住的请求。
- 分流在 `middleware.Wan3TehuiXinbiRoute`（挂在 Seedance25TeHuiRoute 之后、Distribute 之前）：改写 body.model 为内部名 `wan3.0-720p-特惠-内链`，**不覆盖**已有的 RouteOriginModel —— 规则二/三先改派时计费原点仍是客户最初请求的促销族。
- 特惠分流只改写参考图：带首尾帧、参考视频、参考音频、seed、>10 张图、http 图、非枚举比例、小数时长 → 一律留在原 Wan3 链路（规则三因此仍然可用）。心笔兼容口**本身**已支持 10 图 / 5 视频 / 5 音频（2026-11-11 `/docs/compat` + `GET /v1/media/models` 实核；同日真机 10 图 + 2 秒受理通过，约 ¥0.27），但首尾帧在兼容口没有字段、分流也不做音视频改写，故这些请求继续走官网。
- 参考图上游硬约束（2026-11-11 真机）：单边像素须在 240–8000，小图在提交阶段 422 `REFERENCE_IMAGE_RESOLUTION_OUT_OF_RANGE`；同一张图用 `?query` 变体凑数量曾触发 `INVALID_INPUT_FILES`（是查询串还是重复内容导致尚未隔离）。两者都在建单前返回、不扣费，本地只校 https 与张数。
- 分辨率**不参与**分流判断：特惠只有一个交付档位，原 Wan3 链路对它的 `resolveGenerationResolution` 无条件返回 480P，客户端传 480P/1080P 同样被忽略、成片统一提升到 720P。心笔侧 `generationResolution` 也是 480P，所以两条链路产物一致；顶层 `resolution`/`size` 在改写时剥掉（心笔会把像素尺寸当比例误读），`size` 里能推断出的画幅仍写进 `metadata.ratio`。
- 提交回执的 `resolution` 报**实际交付档位** 720P（Wan3 与心笔两侧都改），不回显客户端请求值 —— 否则等于把「参数被忽略」包装成承诺。
- 时长口径与 Wan3 一致：2–30 秒透传，缺省 5，越界夹取；内部名必须保持按秒，不能进 `IsSeedance25PerCall` 能力组。
- 内部名**不进模型广场**、不配 ModelPrice（取价走 BillingModelName = 特惠）；生产需把该名字加进 channel 25 的 models 与 abilities。
- 不同公开模型分别维护基础价与分辨率倍率；调价一起核对代码默认、生产option、各模型回归与前端展示，不把官网档倍率套给标准版。
- 通用TaskSubmitReq不支持的顶层ratio会丢失，需要metadata.ratio；网关接不同适配器时不能假设字段位置相同。

## SeetaCloud：自建 ComfyUI 实例（2026-11-06）

新渠道 type=70「SeetaCloud」。上游是自建的 ArcReel-H3 ComfyUI 实例，
协议与 autodl.art 接近但**有三处不能照抄**，本节记录差异。

| | SeetaCloud | autodl.art |
|---|---|---|
| 提交响应 | `{"data":{"task_id":"..."}}`，**无 code/msg 外层** | `{"code":"Success","data":{...}}` |
| 失败响应 | `{"error":{"message":"...","type":"..."}}` | `code != "Success"` |
| 参考图 | **只收 data URI**，传 URL 直接 400 | 收 URL |

- 鉴权用**裸 token**（`Authorization: <token>`），与 autodl.art 一致；带 Bearer 也能过，
  但裸 token 是原生写法。
- 提交 `POST /api/v1/comfyui/comfyui_workflow/{workflow}`，
  轮询 `GET /api/v1/comfyui/comfyui_workflow/result/{id}`。
  轮询返回 `data.status` / `data.results[].url`，成片直链**下载不需要鉴权**。
- ⚠️ **上游不校验工作流名**：传一个根本不存在的 workflow 也照样建单、照样出片。
  所以模型→工作流的映射必须由网关本地白名单兜底，不能依赖上游报错。
- 分辨率 **480p / 768p × 横竖(1:1)** 全模型可用；**1080p 档仅 Omni 链支持**
  （768p 采样 + 潜空间放大）；时长 **1–15 秒**。
  分辨率 token 是「档位+方向」写法（`480p横` / `768p竖` / `1080p(1:1)`），
  裸 `480p` 虽然也返回 200，但那是服务端兜底，不是契约。
- **参考素材只收 data URI**：客户给 http(s) 链接时，网关要先下载再转成 data URI
  下发（本地做 SSRF 校验；图 10MB / 视频 50MB / 音频 30MB 上限）。这一步让建单变慢，
  所以素材下载要有独立超时，不能占着建单连接无限等。
- **对外模型（2026-11-07 改版）**：
  - `minimax_h3_v2` —— FastH3 V2 8 步蒸馏，文生/图生/首尾帧**三合一**：
    不带图=纯文生，1 张=首帧图生，2 张=首尾帧驱动（关键帧上限 2，传 3 本地 400）；
  - `minimax_h3_omni` —— FastH3-Omni ref2va 多模态参考：设定参考最多
    **9 图 + 3 视频 + 3 音频**（`ref_image_N` / `ref_video_N` / `ref_audio_N`），
    首尾帧走请求 `metadata.first_frame` / `metadata.last_frame`，可与设定参考同请求共存；
    prompt 里用 `<Picture i>` / `<Video k>` / `<Audio j>` 位置引用（按类型独立计数，1 基）；
  - 旧名 `minimax_h3-文生` / `minimax_h3-图生` 退役：不再出现在 ModelList 与模型广场，
    适配器保留**隐藏别名**继续路由（文生别名=V2 全语义；图生别名保留必填图契约）。
- 计费：**按秒**，两条链**基准价不同**，倍率也各自一套（2026-11-08 调价）：
  - `minimax_h3_v2`：480P **¥0.03**/秒（倍率 1.0）、768p（720P 档）**¥0.05**/秒（倍率 1.667）；
  - `minimax_h3_omni`：480P **¥0.02**/秒（1.0）、768p **¥0.04**/秒（2.0）、1080p **¥0.06**/秒（3.0）。
  - 基准价在 `setting/ratio_setting/model_ratio.go`（`seetaVideoBasePrice` /
    `seetaOmniBasePrice`）与生产 `ModelPrice`（优先）；倍率在
    `relay/channel/task/seeta/constants.go`（`v2ResolutionRatios` / `omniResolutionRatios`）。
    **改价必须两边同时改** —— 只改基准会按旧倍率少收（768p ¥0.0333、1080p ¥0.04），
    只改倍率会按旧基准多收（¥0.06 / ¥0.09）。
  - **Omni 参考视频并入计费秒数**（2026-11-08，与 wan3 同口径）：
    `seconds = 生成时长 + 参考视频总秒数`（逐条解析 `mvhd` 实测时长、四舍五入到整秒后相加），
    再乘档位倍率，即参考视频与生成同价；参考图片/音频不额外计费，V2 不收参考视频。
    例：768p 生成 5 秒 + 11 秒参考视频 = ¥0.02 × 2.0 × 16 = **¥0.64**。
    探测在 `ValidateRequestAndSetAction`（预扣费之前）完成，值经 `gin.Context` 传给
    `EstimateBilling`；解析不出时长即 400 `invalid_reference_video`（LocalError，
    不建单不扣费、不 AutoBan）—— 计费依赖实测值，按 0 秒放行会少收。
    因此参考视频必须是能读出 `mvhd` 的 **MP4/MOV**；webm 等无 `mvhd` 容器会被拒。
    `http(s)` 素材会被下载两次（探测一次、`BuildRequestBody` 转 data URI 一次），
    这是为了保证计费与下发用的是同一份解析口径，勿擅自合并以免算错钱。
  - Omni 出片较快（768p 约 1–2 分钟），对客可按「3 分钟内交付」表述。
- **Omni 三种创作模式与素材硬约束**（2026-11-08 真机验证 + 本地收口）：
  上游 H3 网关 `derive_video_kind()` 按 payload 派生语义，网关下发的字段名决定走哪档：

  | 客户写法 | 网关下发 | 上游档 | 约束 |
  |---|---|---|---|
  | 只传 prompt | 无素材字段 | `text` | 0 张图，纯文生视频 |
  | `metadata.first_frame`（+`last_frame`） | `first_frame` / `last_frame` | `first_last` | 图生 / 首尾帧；不接参考视频/音频 |
  | `images`、media 里的视频音频 | `ref_image_N` / `ref_video_N` / `ref_audio_N` | `multi_reference` | **必须至少一张设定参考图** |

  - `images` 是**设定参考**（ref_blocks），不是首帧；做图生/首尾帧必须走 `metadata.first_frame`。
  - 带参考视频/音频却没有设定参考图 → 上游 400「多参考档需要至少一张参考图」，且报错里点名
    内部工作流 ID。真机：只带 11 秒参考视频 → 400（未建单未扣费）；补 1 张 `ref_image_0`
    → 200 出片（`task_VZmt1axVYojijSxZWNCzHLn4CXZaeTOj`，16 计费秒 ¥0.64）。
  - 只有 `metadata.last_frame` 没有 `first_frame` → 上游 400「首尾帧锚定要求首帧先给」。
  - 上述两类已由 `relay/channel/task/seeta/reference_inputs.go` 在
    `ValidateRequestAndSetAction` 本地 400（`invalid_reference_media` / `invalid_first_frame`），
    位于预扣费与参考视频探测之前：不建单、不扣费、不 AutoBan、不白下载素材。
  - 上游内部工作流 ID `minimax_h3_lightx2v*` 在 `model/upstream_redact.go` 的
    **全局**供应商词表遮蔽；不能放进 `upstreamModelLiterals`（那份只对别名模型生效，
    omni 不是别名模型），也不能遮 `minimax_h3_omni` / `_v2`（客户自己买的模型名）。
  - ⚠️ 未决：纯文生与首尾帧目前只有上游代码契约与 V2 历史实测支撑，
    **本站 omni 未做端到端真机出片验证**（用户 2026-11-08 选择先不跑）。
- 脱敏：`seetacloud` 走字面量；`seeta` 短词走 `\bseeta\b` 词边界模式 ——
  字面量匹配没有词边界，裸 `seeta` 会误伤正常英文词。

### 实测口径（2026-11-06 生产直连 + 网关双路验证）

**上游参数行为**

| 输入 | 上游响应 |
|---|---|
| `resolution` = `480p横/竖/(1:1)`、`768p横/竖/(1:1)` | 200，出片；实际交付 832×480 / 1344×768 / 480×832 / 640×640 |
| `resolution` = `1080p横` | 400「不支持的分辨率 token」 |
| `resolution` = 裸 `480p` | 200（服务端兜底，非契约，**网关不放行**） |
| `duration` = 1 / 2 / 15 | 200，出片（15 秒约 63 秒算完） |
| `duration` = 16 | 400「时长必须是 1-15 秒」 |
| 缺 `prompt` 或空串 | 400「prompt 不能为空」 |
| `seed` | 200，接受 |
| 文生档带 `ref_image_*` | 400「文生视频档不接受 ref_image_*」 |
| 图生档缺图 | 400「图生视频/首尾帧工作流需要一张图」 |
| 参考图传 http(s) URL | 400「只接受 data URI」 |
| 传 3 张图 | 200 **且出片**，但第 3 张完全没生效（上游只认首帧+尾帧两个槽位） |

⚠️ **上游是「静默忽略」型**：不存在的 workflow 名、多余的尾帧字段名（`last_frame` /
`tail_frame` / `end_frame` 全都收）、超额的第 3 张图，都返回 200 并照常出片。
所以**「上游没报错」不等于「参数生效了」** —— 模型白名单、分辨率档位、参考图张数
都必须由网关本地兜住。

**网关侧必须自己拦的（本地 400，不触上游）**

- 分辨率不在白名单内（v2 6 档；omni 9 档含 1080p；v2 传 1080p 直接拒）；
- 图生别名缺参考图（v2 正名缺图=文生，不拦）；
- 参考图超限（v2 关键帧 **2 张**上限；omni 设定参考 **9 张**上限）；
- 参考视频 > 3 个 / 参考音频 > 3 个（omni）；v2 传音频/视频参考直接拒；
- 参考素材格式非法（非 data URI / 非 http(s) / MIME 前缀不符 / 超体积）；
- ⚠️ 这些错误必须用 `taskcommon.NewUserError` 包起来 → relay_task 才降级成 400
  LocalError。裸 error 会变成 500，触发重试与 AutoBan —— 客户传错参数就能把渠道打掉。

**对客口径**

- 提交回执的 `size` 必须回显**实际解析出的**档位（不是工作流默认值）；
- 查询态 `status` 的基准是**本站任务状态**（`task.Status`），不是 `task.Data` 里的
  上游 status —— `task.Data` 在轮询前存的是提交回执（没有 status 字段），
  只看它会退化成 `status=unknown`，客户会以为任务挂了；
- `metadata.url` 一律给**站内代理路径** `/v1/videos/{task_id}/content`，
  不吐上游实例直链（直链里带 `u113...seetacloud.com:8443` 实例地址，
  且直链本身不鉴权，拿到就能下）。

