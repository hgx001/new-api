# 无限画布（infinite-canvas）接入

上游：`basketikun/infinite-canvas`（MIT）。本地 fork：`C:/work/infinite-canvas`；新机构建目录 `/home/ubuntu/infinite-canvas`。

定位：new-api 首页的一个标签页，用户用**自己的平台令牌**在画布里创作，额度/分组/限流全部沿用平台规则；画布本身**没有登录页**，身份完全由 new-api 会话决定。

## 部署形态

| 项 | 值 |
|---|---|
| 画布容器 | `infinite-canvas:newapi`，`127.0.0.1:3102 -> 3000`，`--restart always` |
| 访问路径 | 两台 nginx 的 **443 server 块**内挂 `/canvas/` → `127.0.0.1:3102/`（`location = /canvas` 301 到 `/canvas/`） |
| 前端构建 | `ENV VITE_BASE=/canvas/`，路由 `basename: import.meta.env.BASE_URL` |
| 源码 | 纯静态站点，无数据库、无登录；作品与渠道凭据都存在**浏览器**里 |

两台都跑同一个镜像，老机只 `docker load`，不在老机构建。

## 凭据注入（关键契约）

画布原生只有「地址栏 `?baseUrl=&apiKey=`」一条导入通道，密钥会进浏览器历史和访问日志。fork 里改成同源 iframe 调用：

```js
// 画布启动时挂到 window 上
window.__infiniteCanvasApplyChannel({
  baseUrl: 'https://api2.heibaidao.cn/v1',
  apiKey: 'sk-…',                     // 当前登录用户的令牌明文
  models: [{ name, capability, scriptId }],
  channelId: 'newapi',
  channelName: 'new-api',
  groupName: '默认分组',
})
```

- 宿主侧实现：`web/default/src/features/home/components/canvas-panel.tsx`（取令牌 → `/api/token/` 列表 → `fetchTokenKey` 取明文 → `GET /v1/models` 拿模型清单 → 轮询注入，直到画布挂上入口）。
- 画布侧实现：`packages/workbench/src/services/channels/injected.ts` + `startup/browser.ts`。
- iframe 必须 `sandbox='allow-scripts allow-same-origin'`（同源注入的前提，oxlint 会对这条规则报错，已就地禁用并注释原因）。
- `injected.ts` 里对配置 store 用**动态 import**：静态导入会形成 store → channels runtime → 启动流程的循环，运行时拿到 `undefined`（实测白屏，`Cannot read properties of undefined (reading 'state')`）。

## 协议脚本（fork 内置）

`packages/model-runtime/src/call-scripts/newapi.ts`，在 `builtin.ts` 里注册：

| 脚本 id | 用途 | 调用 |
|---|---|---|
| `newapi-video` | 视频 | `POST /v1/videos`（JSON：`model/prompt/duration/resolution/ratio/images/media[]`）→ `GET /v1/videos/{id}` 轮询 → 完成后取 `metadata.url`，缺失才 `GET /v1/videos/{id}/content` |
| `newapi-image` | 图片（任务式） | 同提交/轮询，脚本内 `utils.poll` 等到完成返回图片地址 |

为什么不能直接用内置 `openai-video`：它按 OpenAI 官方约定发 **multipart**（`input_reference` 传文件），而 new-api 任务接口按 **JSON** 解析，提交阶段就会失败。状态值 `queued/in_progress/completed/failed` 两边一致，可直接映射。

参考图/参考音视频必须**公网 http(s) URL**（走 `utils.toPublicUrl`）：new-api 的渠道拒收 data URI，返回 400 `invalid_input_reference`。

## 未决 / 注意

- 画布默认模型按 `guessCapability` 猜，可能把视频模型（如 `wan-3.0-yz`）选成默认；不影响功能，真要固定得改 `pickDefaultModel`。
- 图片走任务协议时 `gpt-image-2.5-官方` 不可用（ch13 已禁、OAuth 未恢复），只能用 Seedream / Nano Banana 系列。
- 画布数据在**浏览器本地**：换浏览器/清缓存会丢作品和渠道配置，注入会在下次进入时重新做。
- 演示站 `canvas.best` 会在 2026-12-01 升 1.0 且数据结构不兼容，不要拿演示站当长期入口。
