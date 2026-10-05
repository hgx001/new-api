# 下游 API 对接文档

适用对象：持有本网关 Key（`sk-...`）的下游客户
协议：OpenAI 兼容（对话 / 图片 / 视频）
文档版本：2026-09-29

---

## 1. 基本信息

| 项目 | 值 |
|---|---|
| Base URL | `https://api.heibaidao.cn` |
| 鉴权 | `Authorization: Bearer <你的Key>` |
| 请求格式 | `Content-Type: application/json`（视频接口也支持 `multipart/form-data`） |
| 查询可用模型 | `GET /v1/models`（返回**你这把 Key 有权限**的模型，以它为准） |
| 货币口径 | 本文价格均为人民币 ¥，按网关实时配置展示 |

- 每把 Key 可单独配置可见模型白名单。若调用未授权的模型，返回 `403` + `This token has no access to model <model>`；模型完全不存在时返回 `503` + `model_not_found`。
- 计费在**提交时预扣**、按实际用量结算；**任务失败自动全额退还**。

---

## 2. 模型总览

| 模型 | 类型 | 端点 | 计费方式 | 价格 |
|---|---|---|---|---|
| `MiniMax-M3` | 文本 | `/v1/chat/completions` | 按 token | 输入 ¥0.22 / 1M，输出 ¥0.09 / 1M |
| `MiniMax-M3.1` | 文本 | `/v1/chat/completions` | 按 token | 同 `MiniMax-M3` |
| `gpt-6-luna` | 文本 | `/v1/chat/completions` | 按 token | 输入 ¥0.94 / 1M，输出 ¥7.50 / 1M |
| `gpt-6-sol` | 文本 | `/v1/chat/completions` | 按 token | 输入 ¥18.75 / 1M，输出 ¥150.00 / 1M |
| `gpt-image-2.5-官方` | 图片 | `/v1/images/generations` | 按张 | ¥0.15 / 张 |
| `wan3.0-smart` | 视频 | `/v1/videos` | 按秒 | 480P ¥0.28/s、720P ¥0.32/s、1080P ¥0.40/s |
| `wan3.0-video-官网` | 视频 | `/v1/videos` | 按秒 | 480P ¥0.27/s、720P ¥0.53/s、1080P ¥0.85/s |
| `wan3.0-video-prime-1080p` | 视频 | `/v1/videos` | **按次** | ¥8.00 / 次（固定 1080P、30 秒） |
| `wan2.7-r2v` | 视频 | `/v1/videos` | 按秒 | ¥0.10/s（720p / 1080p 同价） |
| `autodl:minimax-h3-u24` | 视频 | `/v1/videos` | 按秒 | 480p ¥0.10/s、768p ¥0.12/s |
| `seedance-2.0` | 视频 | `/v1/videos` | **按次** | ¥1.50 / 次（5/10/15 秒） |
| `seedance-2.5` | 视频 | `/v1/videos` | **按次** | ¥1.00 / 次（固定 30 秒、720P） |
| `gemini-web-video` | 视频 | `/v1/videos` | **按次** | ¥1.00 / 次 |
| `Nano Banana Pro` | 图片 | `/v1/videos` | 按张 | ¥0.30 / 张 |
| `jimeng-video-reverse` | 文本（提示词） | `/v1/videos` | **按次** | ¥1.00 / 次 |

**兼容/隐藏模型**（可调用，但不在模型广场展示）：

| 模型 | 说明 |
|---|---|
| `gpt-5.6-luna` | 等价 `gpt-6-luna`，同价（老 Key 无需改代码） |
| `gpt-5.6-sol` | 等价 `gpt-6-sol`，同价 |
| `MiniMax-M2.7` | 与 `MiniMax-M3` 同价 |

---

## 3. 文本对话

`POST /v1/chat/completions`，与 OpenAI 完全一致（`temperature`、`max_tokens`、`tools`、`stream` 等参数原样透传）。

```bash
curl https://api.heibaidao.cn/v1/chat/completions \
  -H "Authorization: Bearer $NEW_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-6-luna",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": false
  }'
```

计费示例：输入 5,000 token + 输出 800 token（`gpt-6-luna`）= 5000/1e6×0.94 + 800/1e6×7.50 ≈ **¥0.011**。

---

## 4. 图片生成

`POST /v1/images/generations`

| 字段 | 必填 | 说明 |
|---|---|---|
| `model` | 是 | `gpt-image-2.5-官方` |
| `prompt` | 是 | 图片描述 |
| `size` | 否 | 如 `1024x1024`；不支持时自动回落该模型默认尺寸，不报错 |
| `n` | 否 | 张数，默认 1 |
| `response_format` | 否 | `url`（默认）或 `b64_json` |

```bash
curl https://api.heibaidao.cn/v1/images/generations \
  -H "Authorization: Bearer $NEW_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-image-2.5-官方", "prompt": "一只橘猫，写实摄影风格", "size": "1024x1024", "n": 1}'
```

响应：`{"created": 1760000000, "data": [{"url": "..."}]}`

计费：**¥0.15 / 张**（与尺寸无关，`n=2` 扣两次）。生成耗时约 20–60 秒，请把客户端超时设到 120 秒以上。

---

## 5. 视频生成

### 5.1 通用流程（三步）

```text
① POST /v1/videos               提交，立即返回任务 id
② GET  /v1/videos/{id}          轮询状态（建议 5–10 秒一次）
③ GET  /v1/videos/{id}/content  任务完成后下载成片
```

**① 提交**

```bash
curl https://api.heibaidao.cn/v1/videos \
  -H "Authorization: Bearer $NEW_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "wan3.0-smart",
    "prompt": "一只橘猫在窗台晒太阳，午后暖光",
    "duration": 5,
    "resolution": "720P",
    "ratio": "16:9"
  }'
```

响应：

```json
{"created_at": 1790475484, "id": "task_xxx", "object": "video", "model": "wan3.0-smart", "progress": 0, "status": "queued"}
```

**② 轮询**

```bash
curl https://api.heibaidao.cn/v1/videos/task_xxx -H "Authorization: Bearer $NEW_API_KEY"
```

| `status` | 含义 |
|---|---|
| `queued` | 排队中 |
| `in_progress` | 生成中（`progress` 0–100） |
| `completed` | 完成，取 `metadata.url` |
| `failed` | 失败，读 `error.message`；费用已自动退还 |

```json
{
  "id": "task_xxx",
  "object": "video",
  "model": "wan3.0-smart",
  "status": "completed",
  "progress": 100,
  "created_at": 1790475484,
  "completed_at": 1790475800,
  "metadata": {"url": "https://..."}
}
```

> `metadata.url` 是上游成片直链，**可能带签名且有时效**，请尽快转存。

**③ 下载成片（推荐）**

```bash
curl -L https://api.heibaidao.cn/v1/videos/task_xxx/content \
  -H "Authorization: Bearer $NEW_API_KEY" -o out.mp4
```

返回成片字节流（`video/mp4`）。任务未完成时返回 `400`；任务不属于该 Key 的账号时返回 `404`。

### 5.2 请求字段说明

| 字段 | 说明 |
|---|---|
| `model` | 必填，见下表 |
| `prompt` | 必填，文本描述；引用素材用 `@图片1` / `@视频1` / `@音频1`（也可写 `Image 1` 等英文形式） |
| `seconds` / `duration` | 时长（秒）。`duration` 传整数（推荐）；`seconds` 为 **字符串**（OpenAI 风格，如 `"5"`），传数字会报 `invalid_json`。两者都传时以 `duration` 为准 |
| `resolution` | 分辨率档位，见各模型表 |
| `ratio` / `aspect_ratio` | 画幅比例（官网渠道请放在 `metadata.ratio`，或用 `size`） |
| `images` / `input_reference` / `image` | 参考图，字符串或数组（公网 HTTPS 直链） |
| `media` | 结构化素材数组：`[{"type":"reference_image","url":"..."}]`，`type` 可取 `reference_image` / `reference_video` / `reference_audio` / `first_frame` / `last_frame` |
| `metadata` | 兜底传参：`metadata.resolution`、`metadata.ratio`、`metadata.media`、`metadata.audio` |

### 5.3 各模型参数与限制

#### `wan3.0-smart`（有赞智能调度，性价比首选）

| 参数 | 取值 |
|---|---|
| `resolution` | `480P`（默认）/ `720P` / `1080P` |
| `seconds` | 2–30（默认 5） |
| `ratio` | `adaptive`（默认）/ `16:9` / `9:16` / `1:1` / `4:3` / `3:4` |
| 参考素材 | 图 ≤10 张、视频 ≤5 个、音频 ≤5 个、首尾帧各 1 张 |
| 计费 | 按秒：¥0.28 / ¥0.32 / ¥0.40（480P / 720P / 1080P） |

- 首尾帧**不能**和普通参考图/视频/音频混用（会直接报错，不消耗费用）。
- 支持文生视频（不传素材）、图生视频、多模态参考。

#### `wan3.0-video-官网`（官方渠道标准版）

| 参数 | 取值 |
|---|---|
| `resolution` | `480P`（默认）/ `720P` / `1080P` |
| `seconds` | 2–30（默认 5） |
| `ratio` | `metadata.ratio`：`adaptive` / `16:9` / `4:3` / `1:1` / `3:4` / `9:16`；也可用 `size`（`960x540` / `1280x720` / `1920x1080`） |
| 计费 | 按秒：¥0.27 / ¥0.53 / ¥0.85（480P / 720P / 1080P） |

- 该渠道目前**仅支持文生视频**：传入参考图/视频/音频会被忽略（不会报错）。需要图生视频或多模态参考请改用 `wan3.0-smart`。

#### `wan3.0-video-prime-1080p`（满血 30 秒档）

| 参数 | 取值 |
|---|---|
| `resolution` | 固定 `1080P`（传其它值会被忽略） |
| `seconds` | 固定 30（传其它值会被忽略） |
| `ratio` | 同 `wan3.0-smart` |
| 参考素材 | 图 ≤8 张、视频 ≤5 个、音频 ≤5 个、首尾帧各 1 张 |
| 计费 | **按次 ¥8.00**，与时长/分辨率无关 |

```bash
curl https://api.heibaidao.cn/v1/videos \
  -H "Authorization: Bearer $NEW_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"wan3.0-video-prime-1080p","prompt":"海边的日落延时"}'
```

#### `wan2.7-r2v`（参考生视频，必须带图）

| 参数 | 取值 |
|---|---|
| `images` | **必填，1–3 张**参考图 |
| `resolution` | `720p` / `1080p`（默认 `1080p`） |
| `seconds` | 仅 `5` 或 `10`（传其它值就近取档） |
| `ratio` | `16:9`（默认）/ `9:16` / `1:1` |
| 计费 | 按秒 ¥0.10（两档同价） |

```bash
curl https://api.heibaidao.cn/v1/videos \
  -H "Authorization: Bearer $NEW_API_KEY" -H "Content-Type: application/json" \
  -d '{"model":"wan2.7-r2v","prompt":"@图片1 中的人物转身走向镜头","images":["https://your-cdn.com/girl.png"],"duration":5,"resolution":"720P","ratio":"16:9"}'
```

- 不支持首尾帧、参考视频、参考音频（传了会直接报错，不消耗费用）；不传参考图同样会报错。

#### `autodl:minimax-h3-u24`（多图 + 多音频生视频，画质优先）

| 参数 | 取值 |
|---|---|
| `prompt` | 必填，≤10000 字符 |
| `images` | **必填，1–9 张** |
| `audios` | 可选，≤3 条 |
| `seconds` | 1–15（默认 5） |
| `resolution` | `480p竖` / `768p竖` / `480p横` / `768p横` / `480p(1:1)` / `768p(1:1)`（默认 `768p竖`） |
| `seed` | 可选，0 ~ 999999999999999 |
| 计费 | 按秒：480p ¥0.10、768p ¥0.12 |

- 本模型的 `768p` 即上游 720P 档；`竖/横/(1:1)` 决定画幅方向。

#### `seedance-2.0`（漫屋）

| 参数 | 取值 |
|---|---|
| `seconds` | `5` / `10` / `15`（默认 15） |
| `ratio` | `16:9`（默认）/ `9:16` / `1:1` / `4:3` / `3:4` / `21:9`（也可用 `size` 传比例字符串） |
| `input_reference` / `images` | 可选，≤10 张，必须是 http/https URL |
| 计费 | **按次 ¥1.50**，与时长无关 |

#### `seedance-2.5`（漫屋）

| 参数 | 取值 |
|---|---|
| `seconds` / `duration` | 固定 `30`；不传时自动使用 30，传其它值直接 400 |
| `resolution` | 固定 `720p`；不传时自动使用 720p，传其它值直接 400 |
| `ratio` | `16:9`（默认）/ `9:16` / `1:1` / `4:3` / `3:4` / `21:9` |
| `input_reference` / `images` | 可选，≤10 张，必须是 http/https URL |
| 计费 | **按次 ¥1.00**，固定 30 秒、720P |

#### `gemini-web-video`（漫屋 Gemini 官网 Veo）

与 `seedance-2.0` 同渠道（漫屋 → ArcReel → 浏览器 Worker → Gemini 官网），差异：

| 参数 | 取值 |
|---|---|
| `prompt` | **必填** |
| `ratio` | 同 dola 六档（默认 `16:9`） |
| `input_reference` / `images` | 可选，≤10 张 http/https URL |
| `seconds` / `duration` / `resolution` | **不支持**，传入直接 400 |
| 计费 | **按次 ¥1.00**（官网固定时长，不随时长变化） |

> 不叫 `veo-*`：官方 Gemini 渠道已占用那些模型名，重名会被路由到错渠道。

> ⚠️ **可用性非 100%，接入前请先探活**。本模型走 Google 官网的浏览器自动化，官网会间歇性
> 拒单。2026-10-04 三次真机验证：1 成 2 败——失败形态有两种，都是**任务失败并全额退款**，
> 不是网关报错：
> - 轮询 10 分钟仍未出片（Worker 侧硬上限，失败原因 `生成轮询超过 10 分钟，官网始终未给出结果`）
> - 建单后数秒被官网拒（失败原因 `Gemini官网任务失败: something went wrong`）
>
> 实测出片耗时约 2.5 分钟（1280x720 / 10 秒 / 24fps）。建议客户端在 `failed` 时提示
> 「上游繁忙，请稍后重试」并允许重投，不要当成参数错误去改请求体。

#### `Nano Banana Pro`（漫屋远端图片）

| 参数 | 取值 |
|---|---|
| `prompt` | **必填** |
| `n` / `count` | 出图张数，1–10（默认 1） |
| `ratio` | 同 dola 六档（默认 `16:9`） |
| `input_reference` / `images` | 可选，≤10 张 http/https URL |
| `seconds` / `duration` / `resolution` | **不支持**，传入直接 400 |
| 计费 | 按张 ¥0.30 × 张数 |

产物在 `metadata.url`，`metadata.media_type = "image"`。因为走异步任务端点，响应是
OpenAI video 对象（`object: "video"`），取图请读 `metadata.url`。

#### `jimeng-video-reverse`（漫屋即梦视频反解）

输入一个视频，输出**提示词文本**（不是媒体）。

| 参数 | 取值 |
|---|---|
| `input_reference`（或 `video` / `videos`） | **必填**，恰好 1 个公网 http/https 视频 URL（服务端代下载，mp4/mov/webm，≤100MB） |
| `prompt` | 可选，作为给即梦助手的附加指令 |
| `ratio` / `seconds` / `images` | **不支持**，传入直接 400 |
| 计费 | **按次 ¥1.00** |

产物在 `metadata.prompt`（文本），`metadata.media_type = "text"`；**没有媒体产物**，
`metadata.url` 不存在（去拉 content 代理会返回 502）。实测 30 秒视频约 90 秒出结果，
上限 5 分钟。

**暂不支持文件直传**：请先把视频传到公网可访问的地址再传 URL；`multipart/form-data`
直传会返回 400。

> 提交反解任务时服务端会先**代下载**该视频（≤100MB），因此 `POST /v1/videos` 的响应
> 可能慢到 1–2 分钟；请把提交超时设为 ≥180s。视频越大越慢，超过 100MB 直接 400。

### 5.4 参考素材要求

- 必须是**公网可访问的 HTTPS 直链**（不接受内网地址、`http://`、本地路径、base64）。
- 图片：PNG / JPG / BMP / WEBP，单张 ≤20MB，单边 240–8000 像素；带透明通道会自动压白底。
- 视频：时长 1–15 秒；音频：单个 ≤15MB。
- 素材 URL 需在提交后至少 90 秒内保持可访问。
- 若图片源不稳定（防盗链、签名过期），建议改用可公开访问的稳定图床。

---

## 6. 计费与退款

| 场景 | 规则 |
|---|---|
| 文本 | 输入/输出 token 分别计价（见模型表） |
| 图片 | 按张：`gpt-image-2.5-官方` ¥0.15/张（`/v1/images/generations` 同步出图）、`Nano Banana Pro` ¥0.30/张（走 `/v1/videos` 异步任务，成图读 `metadata.url`） |
| 视频（按秒） | 单价 × 秒数 × 分辨率档位倍率 |
| 视频（按次） | 固定价：`wan3.0-video-prime-1080p` ¥8/次、`seedance-2.0` ¥1.50/次、`seedance-2.5` ¥1.00/次、`gemini-web-video` ¥1.00/次、`jimeng-video-reverse` ¥1.00/次 |
| 任务失败 | 自动全额退还，无需申请 |
| 内容审核 | 提示词或素材触发上游审核 → 任务失败并退款，失败原因为 `内容审核不通过` |

**配额单位**：网关内部按配额（quota）计账，`¥1 = 500000 / 7.3 ≈ 68493` 配额；对外只需按上表价格核对。

---

## 7. 错误码

对话/图片接口返回 OpenAI 风格：

```json
{"error": {"message": "错误说明", "type": "error_type"}}
```

视频接口返回：

```json
{"code": "invalid_request", "message": "错误说明"}
```

| HTTP | 含义 | 处理建议 |
|---|---|---|
| 400 | 参数错误（时长/比例/分辨率非法、缺少必填素材、首尾帧与参考素材混用等） | 按 `message` 修正参数后重试 |
| 401 | Key 缺失/无效/过期 | 检查 `Authorization` 头 |
| 403 | 配额不足（`insufficient_user_quota`），或该 Key 无此模型权限 | 充值 / 找管理员开通 |
| 404 | 任务不存在（或不属于该账号） | 检查 task id |
| 408 | 提交结果未知（超时） | 不要重复提交，先查询任务状态 |
| 429 | 触发限流/并发上限 | 退避重试（建议指数退避） |
| 500 | 请求构建失败（`build_request_failed`）或内部错误 | 检查参数后重试；反复出现请附 task id 反馈 |
| 502 / 503 | 上游暂时不可用 / 模型不可用（`model_not_found`） | 稍后重试或改用其它模型 |

**提交超时（HTTP 408）**：表示上游是否受理未知，**不要立即重复提交**，先 `GET /v1/videos/{id}` 查状态或联系管理员核对。

---

## 8. 注意事项

1. **轮询与超时**：建议 5–10 秒轮询一次；客户端超时建议 ≥ 300 秒。各模型实测/预计耗时（含排队）：

| 模型 | 耗时 |
|---|---|
| `wan3.0-smart`、`wan2.7-r2v` | 约 2–5 分钟 |
| `wan3.0-video-官网` | 约 6 分钟 |
| `wan3.0-video-prime-1080p` | 尚无实测样本，预计 5–10 分钟 |
| `autodl:minimax-h3-u24` | 约 5–20 分钟 |
| `seedance-2.0` | 约 4–40 分钟（波动大） |
| `seedance-2.5` | 约 4–40 分钟（波动大） |
| `gemini-web-video` | 实测约 2.5 分钟（10 秒 / 720P；官网会间歇性拒单，见该模型小节） |
| `jimeng-video-reverse` | 实测 30 秒视频约 90 秒 |
| `Nano Banana Pro` | 尚无稳定实测样本 |
2. **成片转存**：`metadata.url` 与 `/content` 都可能有时效，成功后请立即下载转存到自己的存储。
3. **不要重复提交**：提交报超时/网络错误时先查任务状态，重复提交会产生两次费用。
4. **提示词审核**：含敏感内容会被上游审核拦截，失败会退款，改写提示词即可。
5. **参数容错**：分辨率大小写均可（`720p` / `720P`）；时长超出范围会被就近取档或截断（除 `wan2.7-r2v`、`wan3.0-video-prime-1080p` 这类固定档位模型）。
6. **模型清单以 `GET /v1/models` 为准**，请勿在代码里写死；模型上下架会通过该接口体现。
   ⚠️ 但该接口会带出**没有可用渠道**的模型：2026-10-04 实测列表里有 28 个，其中 `gpt-5`、
   `gpt-5.1`、`gpt-5.4`、`gpt-5.5`、`gpt-5-mini`、`gpt-5-nano`、`gpt-5.4-mini`、
   `gpt-5-chat-latest`、`gpt-5.6`、`gpt-5.6-terra` 这 10 个实际调用全部返回
   `503 model_not_found`。**首次接入请对每个模型发一次最小请求探活**，不要只看列表就写进代码。
7. 需要新增模型、调整配额或排查具体任务，请提供 **task id + 请求时间 + 错误信息**。

---

## 9. 快速自检清单

- [ ] `GET /v1/models` 能看到目标模型
- [ ] 文本/图片/视频三类端点的 Base URL 都是 `https://api.heibaidao.cn`
- [ ] 视频客户端超时 ≥ 300 秒，轮询间隔 5–10 秒
- [ ] 参考图使用公网 HTTPS 直链且可公开访问
- [ ] 任务成功后立即下载成片转存
- [ ] 提交超时不自动重试，改为查询任务状态
