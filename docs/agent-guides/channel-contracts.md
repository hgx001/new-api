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
- 心笔只收参考图：带首尾帧、参考视频、参考音频、seed、>9 张图、http 图、非枚举比例、小数时长 → 一律留在原 Wan3 链路（规则三因此仍然可用）。
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
- 分辨率只有 **480p / 768p × 横竖(1:1)**，**不支持 1080p**；时长 **1–15 秒**。
  分辨率 token 是「档位+方向」写法（`480p横` / `768p竖` / `480p(1:1)`），
  裸 `480p` 虽然也返回 200，但那是服务端兜底，不是契约。
- **参考图只收 data URI**：客户给 http(s) 链接时，网关要先下载再转成 data URI
  下发（本地做 SSRF 校验、体积上限 10MB）。这一步让建单变慢，所以图片下载
  要有独立超时，不能占着建单连接无限等。
- 对外模型名 `minimax_h3-文生` / `minimax_h3-图生`（带中文后缀，直面客户）。
  上游工作流名 `minimax_h3_lightx2v_no_pic` / `minimax_h3_lightx2v` 只在适配器
  内部映射，不出现在任何对外出口。
- 计费：**按秒**，480P ¥0.03/秒（基准），768p（720P 档）¥0.05/秒（倍率 1.667）。
- 脱敏：`seetacloud` 走字面量；`seeta` 短词走 `\bseeta\b` 词边界模式 ——
  字面量匹配没有词边界，裸 `seeta` 会误伤正常英文词。
