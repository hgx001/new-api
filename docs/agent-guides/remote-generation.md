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

- dola 公共名是 `seedance-2.0`（点号）；豆包是 `seedance-2-0-fast` / `seedance-2-5`（连字符），转发后者时翻译成 `doubao-*` 官网真名。
- `seedance-2.5` 的 dola 30s 已按既定业务下架/退役；旧代码还存在表项不等于可售能力，不据此恢复 channels/abilities 或承诺 Worker 30s。
- dola/Fast 合法 5–15s 任意整数原样透传、不吸附；豆包2.5为5/10/15/30档，未指定比例不编造，豆包不接受 resolution。
- `gemini-web-video` 故意不叫官方 `veo-*`，避免渠道重名；Veo 不接受自定 seconds/duration/resolution，不能静默丢弃后假装执行。
- `Nano Banana Pro` 对应 ArcReel 抽象 `manwu-image`，内部实际站点由服务端路由；图片不接受视频参数。
- `jimeng-video-reverse` 恰好一段公网参考视频，文本产物、prompt 可选；不接受图片/时长/比例/分辨率或 multipart 本地文件直传。
- 万相图生/编辑分别由 `wan2.7-图生` / `wan2.7-编辑` 表项按素材形态区分，不能因同 platformId 就混用协议。详细限值查当前表和验证分支，不复制固定模型数量。
- 拒绝无法诚实实现的显式参数，本地400；不悄悄改规格、丢图或换档交付。
- 改公开名同步 ArcReel `_MANWU_MODEL_PATTERN` 与 Worker `heibaidaoModelCapabilities` / `modelProviderPresets`。正则不得误吃其它供应商相近名字；旧 db-* 不重新对外上架。

## 计费、结果与交付

- 计费细则见 [定价与目录](pricing-catalog.md)。按次返回 nil 倍率，Fast 按 seconds，图片按 n；倍率型模型不能放入按次跳过倍率的 `TASK_PRICE_PATCH`。
- 托管相对 sourceUrl 需服务端鉴权，走本站 `/v1/videos/{task_id}/content`；公网 CDN 直链不得携带渠道/service key。
- 文本结果沿 `TaskInfo.ResultText → TaskPrivateData.ResultText → TaskDto.ResultText`，OpenAI 形态放 `metadata.prompt` 与 `media_type=text`，不能给空 URL 诱导客户端下载不存在的文件。
- 反解素材由 ArcReel 下载，提交可能耗时1–2分钟；不能为“快”压低网关超时导致“客户端已失败、上游却已建单”。
- 验交付用历史 ready 产物和正确用户鉴权；不因一次官网 something-went-wrong 就认定网关错误，也不能把过去验过的链路当永久不会出故障。
- 模型广场与调用能力分离，反解价格未核定前不因占位价对外展示。上限/参数变动同步边界测试与三仓实现，不只改常量。
