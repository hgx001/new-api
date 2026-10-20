# AGENTS.md — Project Conventions for new-api

DO NOT send optional commentary

## 三项目关系链路（2026-10-08）

本业务由三个紧密耦合的项目组成，排查问题前先分清归属（三份 AGENTS.md 各有一份相同章节，改链路时三处同步更新）：

| 项目 | 位置 | 角色 |
|---|---|---|
| 燃境 Ai App | `C:/work/即梦网站/cron_video_brach`（Electron） | **Worker 执行体**：DOM 自动化跑各平台官网，以 Worker 身份连 ArcReel 的 WSS 接单执行 |
| ArcReel（漫屋） | `C:/work/即梦网站/ArcReel`（`manwu-agent` 是其 gitee 镜像） | **服务端 + 前端**：建单、`assign_job` 派单（按 `<platform>.dom` 能力匹配）、钱包计费、产物托管 |
| new-api（本仓库） | `C:/work/new-api-src`（维护目录 `C:/work/维护newapi`） | **对外 API 网关**：漫屋渠道把任务转发给 ArcReel，对终端用户卖 OpenAI 兼容 API |

### 端到端调用链

```
终端用户（new-api 令牌）→ POST /v1/video/generations
  → 本仓库漫屋渠道（relay/channel/task/manwu/adaptor.go，参数白名单校验）
  → ArcReel POST /api/v1/remote-generation/jobs（REMOTE_OPENAPI_SERVICE_TOKEN 鉴权）
  → ArcReel assign_job（按 <platform>.dom 能力匹配在线 Worker）
  → Worker App（WSS 长连接）DOM 自动化执行
  → Worker 下载→delogo→上传产物 → ArcReel 托管（projects/remote_outputs/）
  → 本仓库轮询 GET /api/v1/remote-generation/jobs/{id} → 代理产物字节回给用户
```

### 模型 → 平台映射（漫屋渠道，adaptor.go modelSpecs 路由表）

| 对外模型名 | ArcReel platformId | 执行站点 | 备注 |
|---|---|---|---|
| `seedance-2.0` | dola | dola 官网 | 5–15s 区间任意值真跑（透传不吸附），¥1/次 |
| `seedance-2.5` | dola | dola 官网 | ~~固定 30s/720p，¥0.8/次~~（2026-10-10 下架；**2026-10-18 硬删除，调用 503**） |
| `seedance-2-5`（上游真名 doubao-seedance-2-5-260628） | doubao | **豆包官网**（www.doubao.com） | 5/10/15/30s 四档，**¥3/次** |
| `seedance-2-0-fast`（上游真名 doubao-seedance-2-0-fast-260128） | doubao | **豆包官网** | 5–15s **逐秒**（每秒都支持），**¥0.1/秒** |
| `gemini-web-video` | gemini | Gemini 官网 Veo | 固定 10s，¥1/次 |
| `Nano Banana Pro` | manwu-image | gemini/jimeng（服务端路由） | 图片 |
| `jimeng-video-reverse` | jimeng | 即梦 | 视频反解，文本出参 |

⚠️ **两个 Seedance 站的名字只差分隔符**：`seedance-2.0`/`seedance-2.5`（点号）走 **dola**，`seedance-2-0-fast`/`seedance-2-5`（连字符）走**豆包官网**。2026-10-12 起豆包公开名去掉了 `db-` 前缀（旧名 `db-seedance-2-0`/`db-seedance-2-5` 已从路由表彻底删除，调用返回 400 `invalid_model`）。豆包模型名与 Worker 侧白名单（`cron_video_brach/src/platforms/doubao/params.ts`）同口径；渠道侧校验在 adaptor.go 的 `kindDoubaoVideo` 分支（2.0 Fast 时长 5–15 逐秒透传、2.5 时长 5/10/15/30 四档、比例七档含 auto、未指定比例不编造、拒 resolution）。

### 环境配对（一一对应，禁止交叉）

| 环境 | App ↔ ArcReel | new-api ↔ ArcReel |
|---|---|---|
| 开发 | Worker 用 `start-dev.bat`（注入 `ws://127.0.0.1:1241` + `worker-token-dev.txt`） | 渠道 BaseURL 填 `http://127.0.0.1:1241` |
| 生产 | 打包版默认 `wss://arcreel.heibaidao.cn` + `worker-token.txt` / UI 配置 | 渠道 BaseURL 不填（默认 `https://arcreel.heibaidao.cn`） |

鉴权三层：Worker↔ArcReel 用 worker token（仅启动器注入）；new-api↔ArcReel 用 `REMOTE_OPENAPI_SERVICE_TOKEN`（ArcReel 环境变量，Bearer，只认 create/get/cancel 三端点 + 产物读取，落系统账号 `openapi-service`）；终端用户↔本仓库用 new-api 令牌。

### 排查要点

- **任务卡 queued** = 没有在线 Worker 上报对应能力（查 ArcReel 库 `remote_worker_nodes` 表）；Worker 离线接不到单 ≠ 生成失败。
- 本渠道建单 400（invalid_model / invalid_duration / invalid_ratio）= adaptor 白名单拦截，对照上表；ArcReel 侧归一见其 create_job 各平台分支。
- **Seedance 2.0 系时长口径（2026-10-10 定稿，2026-10-12 两端统一为逐秒透传）**：`seedance-2.0`（dola）与 `seedance-2-0-fast`（豆包）对外均按 **5–15 秒区间**受理，**区间内每一秒都支持**（5,6,…,15），区间外（含 30s）一律 400，且**原样透传、不吸附**——依据是 Worker durationHack 真机实证官网后端接受任意值（7 秒提交成片实测 7.10 秒；dola 不点时长 UI、豆包非档位值跳过 UI，均靠请求体改写下发，并带硬闸门拒单防静默回落）。`seedance-2-5`（豆包）仍是 5/10/15/30 四档白名单，`seedance-2.5`（dola）固定 30s/720p。
- 漫屋前端报「没有漫屋授权桥」= 在纯浏览器里点了万相桥链路的按钮（须桌面端 App），与远端任务体系无关，两套通道别混。

## Overview

This is an AI API gateway/proxy built with Go. It aggregates 40+ upstream AI providers (OpenAI, Claude, Gemini, Azure, AWS Bedrock, etc.) behind a unified API, with user management, billing, rate limiting, and an admin dashboard.

## Tech Stack

- **Backend**: Go 1.22+, Gin web framework, GORM v2 ORM
- **Frontend**: React 19, TypeScript, Rsbuild, Base UI, Tailwind CSS
- **Databases**: SQLite, MySQL, PostgreSQL (all three must be supported)
- **Cache**: Redis (go-redis) + in-memory cache
- **Auth**: JWT, WebAuthn/Passkeys, OAuth (GitHub, Discord, OIDC, etc.)
- **Frontend package manager**: Bun (preferred over npm/yarn/pnpm)

## Architecture

Layered architecture: Router -> Controller -> Service -> Model

```
router/        — HTTP routing (API, relay, dashboard, web)
controller/    — Request handlers
service/       — Business logic
model/         — Data models and DB access (GORM)
relay/         — AI API relay/proxy with provider adapters
  relay/channel/ — Provider-specific adapters (openai/, claude/, gemini/, aws/, etc.)
middleware/    — Auth, rate limiting, CORS, logging, distribution
setting/       — Configuration management (ratio, model, operation, system, performance)
common/        — Shared utilities (JSON, crypto, Redis, env, rate-limit, etc.)
dto/           — Data transfer objects (request/response structs)
constant/      — Constants (API types, channel types, context keys)
types/         — Type definitions (relay formats, file sources, errors)
i18n/          — Backend internationalization (go-i18n, en/zh)
oauth/         — OAuth provider implementations
pkg/           — Internal packages (cachex, ionet)
web/             — Frontend themes container
 web/default/   — Default frontend (React 19, Rsbuild, Base UI, Tailwind)
  web/classic/   — Classic frontend (React 18, Vite, Semi Design)
  web/default/src/i18n/ — Frontend internationalization (i18next, zh/en/fr/ru/ja/vi)
```

## Internationalization (i18n)

### Backend (`i18n/`)
- Library: `nicksnyder/go-i18n/v2`
- Languages: en, zh

### Frontend (`web/default/src/i18n/`)
- Library: `i18next` + `react-i18next` + `i18next-browser-languagedetector`
- Languages: en (base), zh (fallback), fr, ru, ja, vi
- Translation files: `web/default/src/i18n/locales/{lang}.json` — flat JSON, keys are English source strings
- Usage: `useTranslation()` hook, call `t('English key')` in components
- CLI tools: `bun run i18n:sync` (from `web/default/`)

## Rules

### Common Code Quality

- New code should stay direct and readable. Prefer early returns, clear branches, and well-named local variables to deep nesting or layered control flow.
- Minimize nested function definitions. Use them only when required by a callback API or when keeping the closure local is clearly simpler than adding another symbol.
- Avoid adding package-level or module-level helper functions that have only one caller and do not express a stable business concept. Inline that logic at the call site instead.
- A separate function is appropriate when it represents reusable behavior, a required interface/framework callback, an exported API, a test fixture, or complex business logic that deserves direct tests.
- If a single-use helper is kept, its name must describe a durable domain concept rather than a mechanical step extracted only to shorten the caller.

### Backend Rules

**JSON package:** All JSON marshal/unmarshal operations MUST use the wrapper functions in `common/json.go`:

- `common.Marshal(v any) ([]byte, error)`
- `common.Unmarshal(data []byte, v any) error`
- `common.UnmarshalJsonStr(data string, v any) error`
- `common.DecodeJson(reader io.Reader, v any) error`
- `common.GetJsonType(data json.RawMessage) string`

Do NOT directly import or call `encoding/json` in business code. `json.RawMessage`, `json.Number`, and other type definitions from `encoding/json` may still be referenced as types, but actual marshal/unmarshal calls must go through `common.*`.

**Database compatibility:** All database code MUST work with SQLite, MySQL >= 5.7.8, and PostgreSQL >= 9.6 simultaneously.

- Prefer GORM methods (`Create`, `Find`, `Where`, `Updates`, etc.) over raw SQL.
- Let GORM handle primary key generation; do not use `AUTO_INCREMENT` or `SERIAL` directly.
- When raw SQL is unavoidable, account for dialect differences:
  - PostgreSQL uses `"column"` quoting, while MySQL/SQLite use `` `column` ``.
  - Use `commonGroupCol`, `commonKeyCol` from `model/main.go` for reserved-word columns like `group` and `key`.
  - Use `commonTrueVal`/`commonFalseVal` for boolean values.
  - Use `common.UsingMainDatabase(...)` for primary database branches and `common.UsingLogDatabase(...)` for log database branches.
- Do not use database-specific features without cross-DB fallback, including MySQL-only functions, PostgreSQL-only operators, SQLite-unsupported `ALTER COLUMN`, or database-specific JSON column types without a `TEXT` fallback.
- Migrations must work on all three databases. For SQLite, use `ALTER TABLE ... ADD COLUMN` instead of `ALTER COLUMN` (see `model/main.go` for patterns).
- Avoid GORM boolean default tags such as `gorm:"default:true"` when the default is a business rule already enforced by code. MySQL and PostgreSQL can normalize boolean defaults differently, causing GORM `AutoMigrate` to repeatedly issue `ALTER TABLE` on restart. Prefer setting these defaults in request/model normalization, hooks, constructors, or service logic; do not replace `default:true` with `default:1` unless the behavior is verified across SQLite, MySQL, and PostgreSQL.

**Relay and provider behavior:**

- When implementing a new channel, confirm whether the provider supports `StreamOptions`; if supported, add the channel to `streamSupportedChannels`.
- For request structs parsed from client JSON and re-marshaled to upstream providers, optional scalar fields MUST use pointer types with `omitempty` (for example, `*int`, `*uint`, `*float64`, `*bool`).
- Preserve explicit zero values in upstream relay request DTOs: absent client JSON fields must become `nil` and be omitted, while explicit `0`, `0.0`, or `false` values must remain non-`nil` and be sent upstream.
- Avoid non-pointer scalars with `omitempty` for optional request parameters, because zero values will be silently dropped during marshal.

**Billing expression system:** When working on tiered/dynamic billing (expression-based pricing), MUST read `pkg/billingexpr/expr.md` first. It documents the design philosophy, expression language, full architecture, token normalization rules, quota conversion, and expression versioning. All billing expression changes must follow that document.

**Backend test quality:** Backend tests must protect real behavior, API contracts, billing/accounting invariants, data compatibility, or regression paths.

- Do not add tests that only improve coverage numbers, prove that code happens to run, or lock in implementation details without a user-visible or cross-module contract.
- Avoid fake fuzz/stress/smoke/performance tests built from random inputs, large loop counts, sleeps, timing comparisons, or log-only assertions.
- Avoid duplicate tests that exercise the same branch with different names but no new invariant.
- Avoid tests that force incorrect provider/protocol semantics into production code.
- Avoid tests that assert private constants, select-field lists, helper internals, or file layout when observable behavior is already covered elsewhere.
- Prefer deterministic table tests with explicit inputs and exact expected outputs.
- When tests need database, request context, user group, settings, or cache state, initialize that state explicitly inside the test fixture.
- New or substantially rewritten Go backend tests MUST use `github.com/stretchr/testify/require` for setup and fatal assertions, and `github.com/stretchr/testify/assert` for non-fatal value checks.
- Avoid hand-written assertion helpers unless they encode a reusable project-specific invariant.
- When cleaning tests, preserve meaningful regression coverage. If a deleted test covered a real contract indirectly, replace it with a smaller test that asserts that contract directly.

### Frontend Rules

- Use `bun` as the preferred package manager and script runner for the frontend (`web/default/`):
  - `bun install` for dependency installation
  - `bun run dev` for development server
  - `bun run build` for production build
  - `bun run i18n:*` for i18n tooling
- Frontend UI text must support i18n with `i18next`/`react-i18next`. Use flat JSON locale files in `web/default/src/i18n/locales/{lang}.json`, with English source strings as keys.
- In React components, use `useTranslation()` and call `t('English key')` for user-facing text.
- Follow `web/default/AGENTS.md` for detailed frontend conventions, including TypeScript, component structure, styling, accessibility, testing, and build checks.

### Deployment Rules

- Production Go backend releases MUST use the remote-build flow: SSH to the production server, check out the exact committed SHA there, build `Dockerfile.deploy` on the server, then recreate the backend container and run the health checks. 入口：本地 `deploy/deploy-backend.sh <sha>`（服务器侧执行体 `deploy/backend-remote.sh`）；完整流程与回滚见 `deploy/README.md`。
- A local Docker daemon MUST NOT be a prerequisite for backend deployment. Do not ask the user to start Docker Desktop or build the production image locally when the production server can build it remotely.
- The frontend and backend have separate deployment paths. `deploy/deploy-frontend.sh` is for the frontend static release only; backend changes must use the backend remote-build deployment flow and must not be treated as frontend-only changes.
- Before deployment, keep the source checkout pinned to the exact commit being released, preserve unrelated local files, use the smallest necessary production change, and verify the container status, application health endpoint, and relevant business API after release.
- **发版必须用仓库脚本，不得用临时脚本绕过**（2026-10-14 定）。生产机连不上 github:443，`deploy/deploy-frontend.sh` L161-165 的 fetch fallback 正是为此存在：fetch 失败 → 查本地 checkout 是否已有该 commit → 继续，所以 bundle 直传后直接跑脚本即可，不需要手工复刻发布逻辑。手工绕过会让脚本的该分支永远得不到验证：`c2f63c6b` 修完 P0-A/P0-B 后，截至 2026-10-14 两个分支在生产**均未执行过**，而当日两次前端发布（`18cf13b2`、`d337494`）走的都是临时脚本 `/tmp/publish-frontend.sh`，因此**不构成验证**。P0-A 还需一次后端发版（服务器 `.git/info/exclude:7` 的 `Dockerfile.deploy` 是 scp 时代残留，应顺手删除）。详见 `docs/superpowers/specs/2026-10-13-deploy-restructure-spec.md` 第 3.x 节。

### Model Pricing and Catalog Rules

- Separate display-only changes from billing changes. Model names, descriptions, badges, and card layout may be frontend-only; any price that affects quota deduction, task settlement, or `/api/pricing` MUST be defined and validated in the backend.
- Before changing a provider price, check the provider's latest official pricing page and the exact deployment region, currency, model variant, and promotion status. Historical commits, old code comments, reseller prices, and search snippets are not authoritative. If a discounted or reseller price is intentional, document that explicitly instead of calling it the official price.
- For per-second video models, verify the complete chain: backend `defaultModelPrice`/persisted `ModelPrice` base price, provider adaptor duration and resolution multipliers, `quota_type` and supported endpoint, frontend currency conversion, and the final settlement calculation. The frontend must never be the only source of truth for an actual price.
- Validate model pricing at multiple layers: deterministic unit tests for every tier and model variant, `/api/pricing`, production database `ModelPrice`, and a forced-channel or real upstream billing/settlement check when applicable. Recheck the model-square card separately because it can cache API data and may show only the base price while tiered rates are applied at request time.
- Keep internal currency conversion explicit. When the project stores a CNY price as an internal USD value, use `CNY price / usd_exchange_rate`, and verify the configured exchange rate before comparing displayed values.
- Dated pricing notes from the 2026-09-12 audit: the official Alibaba Cloud North China prices are `wan3.0-video` = ¥0.30/0.60/1.20 per second for 480P/720P/1080P and `wan3.0-video-prime` = ¥0.45/0.90/1.80; AutoDL's requested public tiers are ¥0.10/0.12/0.20 per second, where the upstream `768p` parameter represents the 720P tier. These values must be revalidated before future price changes.

### ArcReel / sub2api 联调约束

- 本机 `C:\\work\\new-api-src` 与 `C:\\work\\sub2api`、`C:\\work\\即梦网站\\ArcReel` 都是生产链路源码，相关服务部署在同一台服务器上。
- 遇到模型或媒体请求异常时，必须联合检查 ArcReel 请求、new-api 路由/适配器、sub2api 账号池/凭证及上游响应；不要仅依据 new-api 自动路由后的成功判断某个指定渠道成功。
- 指定渠道验证应使用 new-api 强制渠道测试或该渠道真实上游直连，并用 request ID、模型、时间关联三侧日志。生产操作遵循备份、最小变更和凭证脱敏原则。

### Manwu（漫屋）通道维护约束

- 通道定位：`relay/channel/task/manwu/` 经 OpenAI 兼容 `POST /v1/videos` 承接七个远端模型，再转 ArcReel 远端建单（默认基址 `https://arcreel.heibaidao.cn`，提交路径 `/api/v1/remote-generation/jobs`）。完整链路：ArcReel 本地任务 → 本中转（heibaidao）→ ArcReel 远端建单 → worker（燃境 App）→ 目标官网。排查必须按此链路逐段确认，不要跳段。
- 模型路由表（`adaptor.go::modelSpecs`，七个模型共用一条建单链路，差异只在 platformId/outputMode/白名单；新增模型先加表项，禁止新开 adaptor）：
  - `seedance-2.0` → `platformId: dola` + `outputMode: video`（按次 ¥1.0，5–15 秒逐秒透传同价）
  - `seedance-2.5` → `platformId: dola` + `outputMode: video`（按次 ¥0.8，固定 30 秒、720p）
  - `seedance-2-0-fast` → `platformId: doubao` + `outputMode: video`（**按秒 ¥0.1/秒**，5–15 秒每一秒都支持；上游真名 `doubao-seedance-2-0-fast-260128`，转发前在 `modelSpec.UpstreamModel` 翻译）
  - `seedance-2-5` → `platformId: doubao` + `outputMode: video`（按次 ¥3.0，5/10/15/30 四档；上游真名 `doubao-seedance-2-5-260628`）
  - `gemini-web-video` → `platformId: gemini` + `outputMode: video`。**故意不叫 `veo-*`**：官方 Gemini 渠道已占那些名字，重名会被路由到错渠道
  - `Nano Banana Pro` → `platformId: manwu-image`（对外模型名已改为 Nano Banana Pro；ArcReel 内部仍用抽象 platformId，服务端固定走 Gemini Pro 上游）+ `outputMode: image`
  - `jimeng-video-reverse` → `platformId: jimeng` + `outputMode: prompt`，**文本出参**（`prompt` 可选；视频入参恰好 1 个 http(s) URL）
- 拒绝口径统一：官网没有可实现控件的参数**一律 400，不静默丢弃**（Worker 侧 `RemoteTaskAdapter` 对 Gemini/Veo 的 unsupported params 同样 fail-fast）。Veo 不接受 `seconds`/`duration`/`resolution`；图片与反解不接受时长/分辨率/比例。反解的 multipart 文件直传报 `invalid_input_reference`（提示改传公网 URL）。
- 结果收敛分两类（`ParseTaskResult` / `VideoProxy::resolveManwuResultURL`）：
  - ArcReel 托管产物（Gemini 图片 blob、Veo 的 data: mp4）的 `sourceUrl` 是**相对路径**且需渠道密钥 → adaptor **不吐** URL，让 new-api 落成 `/v1/videos/{task_id}/content` 代理，由 `controller/video_proxy.go` 的 `ChannelTypeManwu` 分支回查 job 取 sourceUrl 再带密钥下载。
  - dola/tiktok 这类公网 CDN 直链照旧透传，**绝不带渠道密钥**（否则凭证泄露给第三方 CDN）。禁止去掉这个区分。
- 文本结果链路：`relaycommon.TaskInfo.ResultText` → `model.TaskPrivateData.ResultText`（JSON 列，免迁移）→ `dto.TaskDto.ResultText`；OpenAI video 响应里落在 `metadata.prompt`（`metadata.media_type = "text"`）。新增文本型任务沿用这条链路。
- 计费：`setting/ratio_setting/model_ratio.go` 定义单价；`seedance-2.0`（¥1.0/次）/ `seedance-2-5`（¥3.0/次）/ `gemini-web-video`（¥1.0/次）/ `jimeng-video-reverse` 按次（`EstimateBilling` 返回 nil）；`seedance-2-0-fast` **按秒**（¥0.1/秒，`EstimateBilling` 返回 `{"seconds": D}`）；`Nano Banana Pro` 按张，张数走 `EstimateBilling` 的 `{"n": N}` 倍率。**注意生产 `ModelPrice` option 会覆盖代码默认值**——只改 `model_ratio.go` 线上不会变，改价必须同时改 option（见 2026-10-05 调价 commit）。
  - ⚠️ **`Nano Banana Pro` 绝不能进 `TASK_PRICE_PATCH` 环境变量**：进了会被 `relay_task.go` 当按次计费跳过倍率相乘，n 张只扣 1 张的钱（静默少收费）。
  - ⚠️ **同理 `seedance-2-0-fast`（按秒）也不能进**：它依赖 `seconds` 倍率，跳过相乘就会把 9 秒的单按 ¥0.1 收（少收 9 倍）。把该变量当作「这些模型都按次」的白名单来读。**2026-10-12 核对：生产容器实际未设置 `TASK_PRICE_PATCH`（`docker inspect new-api` 无此变量）**，即当前没有任何模型跳过倍率相乘；文档里「seedance-2.0/2.5 在该变量里」是旧状态，别据此推断线上行为。
- 提交耗时：反解的参考视频由 ArcReel 代下载（≤100MB，上限对齐 Worker 硬限制），`POST /v1/videos` 可能阻塞 1–2 分钟。本中转 `RELAY_TIMEOUT` 默认为 0（不超时），不要为了「快」把它调到 60s 以下，否则大视频会出现「网关报错但上游已建单」。
- ⚠️ **`gemini-web-video` 链路本身已验证可用，但上游 Google 官网会间歇性拒单（2026-10-04 三次真机验证）**。task 246 轮询 20 分钟无果（Worker 重试两次 ×10 分钟上限）；task 247 `provider_running` 后 **5 秒**被官网拒，报 `Gemini官网任务失败: something went wrong`；task 262 一次成功（2.5 分钟出片，1280x720 / 10s / 24fps / h264+aac / 4.5MB）。同期 ArcReel 历史 15 ready / 7 failed（约 68%）。脆弱点在 Worker `mac-mini-001` 只有 1 个可提交 gemini 账号（`1789263965686-jkhdkq`，`max_concurrent=1`），另一个 `1789010295191-v4p8en` 自 2026-09-12 起 `needs_relogin`。
  - 排查口径：先看 ArcReel `remote_worker_accounts` 有几个 `can_submit=1`、再对比 `remote_generation_events` 的 `job.failed` payload。`something went wrong` = 官网侧拒单（额度/风控/会话），**不是**建单参数问题；`生成轮询超过 10 分钟` = Worker 侧 `DomPlatformScheduler.PLATFORM_POLL_TIMEOUT_MS.gemini` 硬上限（第四仓 `cron_video_brach`）。
  - **不要因为这两种报错去改本中转**：路由、请求构造（只传 `videoParams.ratio`）、计费（两侧同为 ¥1.0/次 = 68493 quota）、状态映射、失败退款（精确退回，差值 0）、交付链路都已逐项验证正确。改本中转只会掩盖上游问题。
  - **判定交付链路是否坏，不要靠新建单去试**：既烧钱又受上游波动干扰。直接拿历史 ready job 的 mp4 验（`task_id=task_n1Lka9MuISXORM5jo3sRrh2Vv5SRmRPn` → `gen-6682493b9ae987d87365ed97`，带渠道密钥得 6.2MB 合法 mp4，不带密钥 401）。
- 模型广场可见性：`seedance-2.0` / `seedance-2-5` / `seedance-2-0-fast` / `gemini-web-video` / `Nano Banana Pro` 已在 `model/pricing.go isAllowedPricingModel` 放行；`jimeng-video-reverse` **故意未放行**——¥1.0/次是占位价，商务核定前不得对外展示价格（白名单只管广场，不影响调用）。卡片描述/供应商/标签存在 `models` 表（`description`/`icon`/`tags`/`vendor_id`），改名或调价时必须同步 `UPDATE models`，否则卡片会继续写旧名与旧价。
- 本地错误码均为 400：`invalid_model` / `invalid_request` / `invalid_duration` / `invalid_ratio` / `invalid_count` / `invalid_input_reference`，改动错误语义时同步更新 `adaptor_test.go` 的拒绝用例表。
- 测试：`go test ./relay/channel/task/manwu/`。改上限必须同步维护边界用例（dola 10 张放行见 `TestValidateAcceptsTenReferenceImages`，11 张拒绝见 `TestValidateRejectsIllegalRequests` 的 `too many images`；图片张数上界、反解单视频约束各有独立用例），禁止只改常量不改测试。
- 双仓库同口径：ArcReel 侧 `lib/video_backends/openai.py` 的 manwu 分支（`_MANWU_MODEL_PATTERN` / `_MANWU_MAX_REFERENCE_IMAGES` / `_manwu_ratio`）必须与本通道同口径。**改对外模型名必须同步改正则**（2026-10-04：`dola-*` 改 `seedance-2.0/2.5` 后，ArcReel 侧正则不改就会退回 Sora 单参考图 + WxH size）。正则在 `seedance-2.0` 前禁止 `._:-`，否则会误吃 `doubao-seedance-2-0-260128`、`huixin:seedance-2.5-burst-15s`。任一侧改上限、比例档或字段语义，必须双侧同步改、同步发版。注意 ArcReel 的部署脚本只更新 ArcReel 两台服务器，不会更新本中转；本中转发版走上面的 Deployment Rules 远程构建流程。
- 第四仓 Worker（`C:\work\即梦网站\cron_video_brach`）对 dola **无需改**：它按 `duration` 收敛官网模型（5/10/15 → `dola-seedance-2-0-fast`，30 → `dola-seedance-2-5`），两个公开模型靠 ArcReel 归一后的时长自动落到正确官网档位。但它的 `shared/constants/heibaidaoModelCapabilities.ts` / `modelProviderPresets.ts` 里**存着黑白道公开模型名**（本中转改名后必须同步改，否则工作台下拉里选中改名后的模型会查不到能力、时长/比例白名单全丢）。本中转**不认识** Worker 侧的 `doubao-*` 官网真名以外的别名，故改名无需 Worker 改 DOM 白名单。

### MiniMax / hailuo 通道维护约束

- 端点按模型分两代，**字段形状完全不同**，不要试图用同一套 payload：
  - v1（Hailuo-2.3 / -02 / T2V-01 / S2V-01…）：`POST /v1/video_generation` 扁平字段（`prompt` / `first_frame_image` / `subject_reference`），轮询 `?task_id=`，成功后再 `/v1/files/retrieve?file_id=` 换直链。渠道 `type=35`（`constant.ChannelTypeMiniMax`）→ `relay/channel/task/hailuo`。
  - v2（`MiniMax-H3` / `MiniMax-H3-Max` / `MiniMax-H3-Context-IR`）：`POST /v2/video_generation` + `content[]` 多模态数组（`type=text|image_url|video_url|audio_url` + `role`），轮询 `/v2/query/video_generation/{task_id}`（**路径参数**），成功即 `task.content.url` 单步取流。实现见 `hailuo/v2.go` + `hailuo/v2_response.go`。
- v2 失败形状是 `{"type":"error","error":{type,message,http_code}}`，**没有 v1 的 `base_resp`**。`ParseTaskResult` 靠报文特征（`looksLikeV2Query`）分流，不能靠 model 名——轮询阶段拿不到 `RelayInfo`。
- `insufficient_balance_error`（`http_code=402`）必须**原样**透传成本端看到的「上游错误」，不能降级成本端错误：它真是上游账户没钱，应当触发换渠道与 AutoBan。
- 能力门控一律 fail-loud（400，不静默改档）：H3 = 768P/2K + 4–15s；H3-Max = 480P/768P + 5–15s（不支持 2K）；首帧≤1、尾帧≤1、参考图≤9、参考视频≤3、参考音频≤3；**首尾帧与参考素材互斥**（上游直接拒绝混用）；纯文生视频必须显式 `ratio`（上游不接受 `adaptive`）。
- 素材只收公网 `http(s)` URL：`data:` URI 对远端上游不可达（它要自己去取），提前 400。
- `MiniMax-H3-Context-IR` 端点是 `/v2/h3_context_ir`，但载荷里的 `model` 必须是 `MiniMax-H3`（端点名 ≠ 模型名），产物是 `content.prompt` → 走 `TaskInfo.ResultText` 链路，`metadata.media_type=text` 且**不返回 url**（要 `delete(metadata,"url")`，`ToOpenAIVideo()` 会预置空串 url，留着会让客户端当有产物去拉不存在的文件）。
- 计费：v2 按时长计价，`EstimateBilling` 返回 `{"seconds": D, "size": 分辨率倍率}`（与 wan3 同范式）；基础价定义在**最低分辨率档**（H3=768P、H3-Max=480P），倍率表在 `V2ModelSpec.ResolutionRatios`。`Context-IR` 不按时长计费（回落按次）。单价放 `defaultModelPrice`（本部署所有视频模型的约定，见「Model Pricing and Catalog Rules」）。
- ⚠️ v2 校验**必须**调 `relaycommon.StoreTaskRequest` 把请求写回 context：`BuildRequestBody` 与 `EstimateBilling` 都从该 key 读取。漏掉的第二个症状是**静默**的——计费拿不到 duration 就按 1 秒收。

### 生产运维硬约束（血泪教训）

- **🔥 未经用户明确允许，禁止跑任何真实视频任务（2026-10-06 用户明令，凌驾于下文一切「真实任务」规则之上）**。视频任务上游按次/按秒真金白银扣费（有赞 prime ≈450 积分/单、smart ≈90 积分/单、seedance ¥0.8–1.0/单），且部分上游 402 预检拒绝后任务**仍可能照跑扣费**（2026-10-06 实测：prime 提交被 402 拒，450 积分照扣、成片照出）。诊断类需求一律走替代手段，按优先级：① 单元测试（`go test` 可确定性复现的绝不真打）；② 历史任务产物验证（不新建单）；③ 必然被本地 400 拦下的非法入参探针（断言 quota 差值为 0、`tasks` 无新增）；④ 通道被禁用等纯路由问题查日志/DB 即可。**只有用户明确说「可以跑一单」之后才允许提交真实任务**；「真实任务上限 1 个」等旧规则均以本条为前提。
- **恢复被 AutoBan 的渠道必须同时改 `channels.status` 与 `abilities.enabled`**。2026-10-02 的真实事故：AutoBan 禁用渠道时连带把能力表置 `enabled=false`，事后只把 `status` 改回 1，导致渠道「看起来正常」但该渠道**所有模型 503 `model_not_found`**，持续数小时无人察觉。恢复用 `scripts/restore-channel.sh <id>`，它两边一起改并打印复核。
- **模型改名/调价的生产步骤（2026-10-12 定稿口径）**：① **先发版**：`deploy/deploy-backend.sh <sha>`（按秒逻辑与白名单在代码里）；② **再跑 SQL**：`scripts/prod-rename-doubao-seedance-20261012.sql`（**单事务**，channels.models → abilities → models 卡片描述 → ModelPrice option 四步原子完成）。SQL 跑完后路由缓存 ≤60s 重建，`/v1/models` 立即，广场 TTL 1min，**无需重启**。

  **顺序不能反，理由是「可见性」而不是「会不会错价」**：反序（SQL 先、后端旧）时 abilities 已有新名，`/v1/models` 会**对外列出** `seedance-2-0-fast`，但旧二进制的 adaptor 表里没有它 → 调用即 400 `invalid_model`。即「模型可见但一调就坏」。正序（先发版）时 abilities 未更新，该模型既不在 `/v1/models` 也不在广场，完全隐形，没人能调到它，SQL 一跑即刻整体生效——没有任何半可用窗口。两种顺序都不会错价（前者 400、后者 503 `model_not_found`，都进不了计费），但正序没有坏体验窗口。

  ⚠️ **绝不能只跑 SQL 不发版**：单价 option 已是新值而代码仍是旧的 `EstimateBilling`，一旦有人绕过 abilities 直接命中渠道，就会用新价配旧倍率扣费。
- **裸 SQL 建/改渠道后必须重启 new-api**：能力走内存缓存（`CacheGetRandomSatisfiedChannel`），SQL 改 `abilities` 对**新建**渠道不生效（对已缓存的旧渠道反而会立刻生效，所以容易误判成「缓存已刷新」）。走管理 API 建渠道不会有这个问题。
- **上下架（隐藏/恢复）一个模型必须「两处同改」**（2026-10-10 定稿）。一个模型的可见性由**三条独立链路**决定，少改一处就会出现「藏了还在」或「改完自己复活」：
  - **广场 `/api/pricing`** ← `abilities.enabled=true` × `model/pricing.go isAllowedPricingModel` 白名单；
  - **下游 `/v1/models`** ← `abilities.enabled=true` × `relay/helper.HasModelBillingConfig`（`model/ability.go GetGroupEnabledModels` 直查能力表）；
  - **路由**（线上 `MEMORY_CACHE_ENABLED=true`）← `channels.models` 字段本身（`model/channel_cache.go InitChannelCache` 按该字段重建 `group2model2channels`，每 `SYNC_FREQUENCY`=60s 一次；未开缓存时才回落到 `abilities`）。
  - 🔥 **只删 `abilities` 行不够**：`model.UpdateAbilities` 是「按 `channels.models` 删光重建」，渠道一旦被编辑（后台保存 / 管理 API），被删的能力行会**原样复活** → 广场与列表又出现。故必须 ① 从 `channels.models` 摘除模型名，② 删除该 `abilities` 行。白名单 `isAllowedPricingModel` **不用改**：abilities 不在时它不参与展示判定，保留反而便于日后一键恢复。
  - 下架即**不可调用**（无渠道 → 503 `model_not_found … 无可用渠道`）。此探针不建单、不扣费，属安全的零成本验证手段。
  - 生效时延：`/v1/models` 立即；广场 pricing TTL 1min；路由缓存 ≤60s。**无需重启、无需重新部署后端**。
  - 参考脚本 `scripts/prod-hide-seedance25-20261010.sql`（单事务 + 备份表 + 回滚 SQL 注释）。
- **验证脚本不得用能通过校验的入参**。`seedance-2.0` 的 `n` 字段不参与校验，拿它当「探针」会**真建单并扣费**（本项目已因此误建 2 次 dola 任务）。**更硬的坑：`seedance-2.0` / `seedance-2-5` / `seedance-2-0-fast` 的最小入参 `{"model":"<模型名>","prompt":"x"}` 都是合法且会真建单**（2026-10-04 又误建 1 次；2026-10-12 两个豆包模型改名后同样成立）。规则：① 只用必然被本地校验拦下的输入（越界 seconds / 越界 ratio / 非 http 参考图 / 越界 n）；② 每次跑完断言 `sum(tokens.used_quota)` 差值为 0、`tasks` 无非终态任务、ArcReel `arcreel.log` 里 `remote-generation/jobs` 计数差值为 0；③ 一旦误建，必须**同时**在 new-api（退款 + 任务置 FAILURE + 标记日志；new-api **没有** `/v1/videos/{id}/cancel` 端点）与 ArcReel（`POST /remote-generation/jobs/{id}/cancel`）两侧撤销。照抄 `scripts/probe-manwu-dola-dual.sh`，它已同时做三项断言。
- **验证「计费 bug」不得用付费真实任务（2026-10-06 立规，代价 810 上游积分）**。上一条讲的是「别误建单」，这一条更进一步：**有些单就是必须建的，也不该多建**。
  - **实例**：修 `relay_task.go` 的 `OtherRatios` 逐项 `int()` 截断（同一请求扣费在 102739/102735 间随机）时，为了「多跑几次看抖动」跑了 **9 个付费任务**，上游有赞共扣 **810 积分**（9 × 90 分）。客户侧 952039 quota 全额退了，**上游那 810 分撤不回来**，净损由运营方承担（台账见 `logs.content LIKE '%OPERATOR-COST-ACCOUNTING%'`，脚本 `scripts/prod-log-probe-upstream-cost-20261005.sql`）。
  - **方法错误不在疏忽，在选错验证手段**：同一提交里已写了 `relay/relay_task_billing_test.go`，它用三个硬编码数值（单次累乘 102739；两种遍历顺序 102739 / 102735；并断言两者不同）就把顺序依赖**确定性复现**了。观察随机性根本不需要真金白银去撞。
  - **三条硬规矩**：① **确定性 bug 一律先上单元测试**，能用 `go test` 复现的不许用真实任务复现（map 顺序、浮点截断、时序、并发类问题尤其如此 —— 它们在测试里是确定的，在生产里是概率的）；② **观察随机性用 `go test -count=50`，不用重复建单**（一次真实任务只采样一次，`-count=50` 采样五十次且不花钱）；③ **真实任务上限 1 个**，只用于确认「端到端通、单价对得上」。
  - **什么值得跑真实任务**：验证**调价后单价** → 1 个（真实账目是唯一事实源，见「Model Pricing and Catalog Rules」）；验证**交付链路** → 0 个（拿历史 ready 任务的产物验）；验证**上游拒单/审核** → 1 个（只能真打，但一次足以定性）。
  - **先查上游计费维度再选档**：有赞 `wan3.0-smart` **不按分辨率区分扣点**（720p 与 1080p 同为 90 分/任务）。我默认跑 1080p 属于**运气**（恰好没多花），不是判断。
  - **每个付费探针跑完必须写 `OPERATOR-COST-ACCOUNTING` 台账**，把上游实扣与客户侧退款并列留痕，否则这笔成本会在对账时凭空消失。
- 部署走 `deploy/deploy-backend.sh <sha>`（服务器侧执行体 `deploy/backend-remote.sh`）：它带 **SHA 前缀断言**（曾出现「构建了旧 commit 却以为成功」）、fetch 失败时校验本地是否已有该 commit、旧容器只保留最近 3 个。生产容器不是 compose 管理，必须按旧容器原参数 `docker run` 重建。

- **🔧 GPT 通道（sub2api）掉线排查：先跑自动恢复脚本，别手工瞎试**。sub2api 上游（`119.29.253.97:8080`，账号 id=4，Codex OAuth）是渠道 13 GPT-Pool 的唯一来源，它一掉线**文本和生图一起挂**，但 new-api 侧看不出任何异常迹象。
  - **脚本**：`deploy-vpn/sub2api_autocheck_task.py`（不在 git 里，`deploy-vpn/` 已列入 `.git/info/exclude`）。**Windows 计划任务「sub2api GPT OAuth 自动恢复」每小时 :05 自动跑它**；手工跑：`<托管python> deploy-vpn/sub2api_autocheck_task.py`。
  - **脚本行为**：先 GET 查账号状态，异常才重导；healthy 时按 6 小时间隔做一次**真实探活**（`gpt-5.6-sol` `max_tokens=1`，纯文本不花钱）。退出码 `0`=健康/恢复成功、`1`=需人工、`2`=环境错误（如登录不上）。
  - **看结果的两个地方**：`deploy-vpn/logs/sub2api_autocheck.log`（每行结论，脚本自己写盘）+ `.workbuddy/memory/YYYY-MM-DD.md`（每日一行摘要）。探活时间戳在 `logs/sub2api_probe_state.json`。
  - **恢复动作**（脚本自动完成，勿手工替）：读本机 `C:\Users\hgx\.codex\auth.json` → `POST /api/v1/admin/accounts/4/apply-oauth-credentials` + `POST .../4/schedulable`，且**必须用本机 JWT `exp` 覆盖 `expires_at`**（沿用旧值会导致「刚恢复又掉」）。
  - **⚠️ 头号判因陷阱：账号 `status=active` ≠ 能用**。2026-10-06 实测踩坑：脚本连续报 healthy，实际账号早已 `Token revoked` 掉线，GPT 文本主通道全挂，是靠一次真实调用才暴露的。所以**只查状态字段的监控一律不可信，必须以真实调用/探活为准**。
  - **症状 → 判因对照表**（真实原因在 `docker logs sub2api`，客户端文案常指向错误的模型名）：
    | 现象 | 真因 | 处置 |
    |---|---|---|
    | 503 `No available compatible accounts` | 调度层无账号，往上找 reason | 看日志 `account_disabled_auth_error` |
    | `Token revoked (401)` / `invalidated oauth token` | 令牌被轮换作废 | 跑恢复脚本重导 |
    | `codex_plan_gated_model` | 计划封锁 | 重导无用，需换账号/套餐 |
    | `model_rate_limited` | 30 分钟冷却 | 等冷却，别重导 |
    | `filtered: not_schedulable` | 已被前一次 401 打挂 | 跑恢复脚本 |
  - **严禁**用 `/test`、`/v1/models`（返 200 假象）或图片/视频请求做健康验证——`/test` 与真实调用会触发 refresh 把账号打回 error；验证只能用**恢复后**的一次最小文本探活。
  - **监控配置注意**：该计划任务 `LogonType` 若为 `Interactive`，**用户未登录 Windows 时根本不跑**（改 S4U 需管理员权限）；任务 XML 备份见 `deploy-vpn/scheduled_task_backup_20261006.xml`。

### 支付宝官方直连（电脑网站支付，2026-10-20 上线并可支付）

- **现状**：生产已开通并启用。option `AlipayEnabled=true`，AppID `2021007109672654`，电脑网站支付已签约、即时到账，收款方显示为「**算力 服务**」（支付宝商家账号主体名，不是「黑白道」）。回调 `https://api.heibaidao.cn/api/user/alipay/notify`，同步返回 `https://api.heibaidao.cn/api/user/alipay/return`。`Price=7.3` → 充值 10 美元 = ¥73。订单表名是 **`top_ups`**（不是 `topups`）。
- ✅ **端到端已验收（2026-10-20 22:12 首笔真实付款 ¥7.30）**：支付宝服务器 IP `203.209.251.104` POST 回调 → 验签通过（响应体 7 字节 `success`，日志无「验签失败」）→ 订单 `USR28ALIPAYV1WXNm53jNOouXzxKwERiYIw` 转 `success` → 充值日志「支付宝官方直连充值成功，充值额度: ¥7.300000，支付金额：7.30」→ 额度 +500000（$1 × QuotaPerUnit 500000）。同步 return 302 回 `/usage-logs`。**改回调/验签相关代码后，用同样方式再验一次**（小额真实付款 + 查 `top_ups.status='success'` + `logs.content LIKE '%充值成功%'`）。
- **三道闸门缺一不可**（`isAlipayTopUpEnabled()` = `AlipayEnabled` + `AlipayAppID` + `AlipayPrivateKey` + `AlipayPublicKey` 全非空）：① option `AlipayEnabled`；② `payment_setting.compliance_confirmed=true` 且 `compliance_terms_version=v1`（不确认则前端**一个支付方式都不显示**）；③ `PayMethods` 含 `type=alipay`。
- **改 option 后必须等 ≥70s**：`SYNC_FREQUENCY`=60s 才同步进内存，早查会读到旧值、误判「没生效」。
- 🔥 **通知验签必须用「原始表单参数」，不能用 gopay 的结构体验签**：gopay 的 `VerifySign` 把解析后的 `NotifyRequest` 结构体重新序列化再拼签名串，`json` 的 `omitempty` 会把**空数组字段**（支付宝通知里常见的 `voucher_detail_list=[]`）整条丢掉，而支付宝签的是原始上报值 → 签名源差这一个字段 → **每一笔真实回调都判验签失败，用户付了钱却充不到账**。本项目已按官方 SDK 口径自己验签：`controller/topup_alipay.go` 的 `verifyAlipayNotifySignature`。回归用例 `TestAlipayGopayStructVerificationDropsEmptyVoucherList` 锁死该行为，**改回调验签前先跑它**。
  - 官方口径 = 支付宝 Java SDK `AlipaySignature.rsaCheckV1` → `getSignCheckContentV1`：**排除 `sign` 与 `sign_type`** + **跳过空值参数**（`areNotEmpty(key,value)`）+ 按 key 升序 + `k=v&k=v` + RSA2(SHA256)。原文可查 `alipay/alipay-sdk-java-all` → `v2/src/main/java/com/alipay/api/internal/util/AlipaySignature.java`（V2 才是「只排 sign、保留 sign_type」，那是**请求签名**方向，别混用）。
- ⚠️ **支付宝公钥与应用私钥本来就不是一对**（别当配置错误排查）：应用私钥签请求、支付宝公钥验**支付宝**的签名，两者是各自独立的密钥对。库里 `AlipayPrivateKey` 与 `AlipayPublicKey` "不配对"是**正常状态**；判据是「支付宝网关响应签名能被 `AlipayPublicKey` 验过」。
- ❌ **「用应用私钥自签一个假通知打回调」这条探针不可用**（2026-10-20 踩过，浪费一轮）：通知签名只能用**支付宝私钥**签，支付宝私钥不归我们，永远造不出能通过的验签样本——自签通知必然报「验签失败」，**该结果对生产代码没有任何证明力**。验签是否正确只能靠**真实小额付款**（¥0.01）看是否到账来判定。
- **RSA2 签名串两个方向口径不同**：**请求**（拉起支付）的签名串**包含 `sign_type`**（gopay `EncodeAliPaySignParams()` 只排除 `sign`，与官方 Java SDK `getSignContent` 一致）；**异步通知验签**要**同时排除 `sign` 和 `sign_type`**（官方 `rsaCheckV1` 就这么干）。手写签名时搞混 → `isv.invalid-signature`。
- **零成本只读探针**：`alipay.trade.query` 查一个**不存在**的 `out_trade_no`，返回 `ACQ.TRADE_NOT_EXIST` 即说明 app_id / 私钥 / 交易类 API 权限都正常（不建单、不扣费）；响应里的 `sign` 可用 `AlipayPublicKey` 验签 → 确认支付宝公钥与该应用匹配（回调验签就靠它）。
- **收银台必须用真浏览器验**：服务端裸 GET 签名 URL 会被支付宝风控拦成 `excashier.alipay.com/home/error.htm?errorCode=FISHING_RISK`，**这不是配置错误**（同一 URL 在浏览器里正常渲染 `excashier.alipay.com/standard/auth.htm?payOrderId=...`，能看到商品/收款方/金额）。别据此判断「没开通」。
- **测试单要清**：测试单是 `pending` + `payment_provider=alipay` + `trade_no` 形如 `USR<id>ALIPAY<rand>`，验完记得删，否则后台充值记录里堆一堆 pending。
- 🔥 **入账必须「原子 + 幂等」，三条硬规矩（2026-10-20 安全审查定稿）**：
  1. **行锁要用 `clause.Locking{Strength:"UPDATE"}`**：`tx.Set("gorm:query_option","FOR UPDATE")` 是 **gorm v1** 的写法，**gorm v2 已移除该特性，会被静默忽略（等于没锁）**。本仓库是 gorm v1.25.2。SQLite 驱动会主动忽略 `FOR` 子句（其 `ClauseBuilders` 里写了 "SQLite3 does not support row-level locking"），所以 SQLite 部署不受影响，PG/MySQL 才会真正加锁。
  2. **状态流转要 CAS（条件更新）+ `RowsAffected` 判定**：`UPDATE ... WHERE trade_no=? AND status='pending'`，只有 `RowsAffected==1` 的那个请求才可以加额度；其余按幂等返回。**为什么必须**：支付宝/易支付的回调报文是**可重放的合法签名**（签名伪造不了，但报文能重放），攻击者拿自己那笔合法通知并发重放 N 份，若状态流转不是原子的，N 个事务会同时通过状态检查、各加一次额度 = 直接资金损失。
  3. **「置成功」与「加额度」必须同一事务**：旧易支付实现是 `topUp.Update()`（置成功）后再 `IncreaseUserQuota()`（加额度）两步，额度更新一旦失败就变成「订单已成功、额度没加」，而管理员补单又会被状态校验拒绝 → **钱货两失且无法补救**。现已收敛到 `model.CompleteEpayTopUp`（行锁+CAS+加额度同事务）。
- **加额度后要同步 Redis 额度缓存**：`model.GetUserQuota` **优先读 Redis**（TTL=`SYNC_FREQUENCY`=60s），裸 `quota = quota + ?` 只改库会让用户看到旧余额、且额度差值持续漂移。直接写库的路径要额外 `gopool.Go(cacheIncrUserQuota(...))`（`IncreaseUserQuota` 就是这么做的）。
- **当前生产的支付面**：支付宝（`AlipayEnabled=true`）+ 易支付（EpayId/EpayKey/PayAddress 有值，但 `PayMethods` 只有 `alipay`，故新单走不了易支付；**历史 41 笔 pending 易支付订单的回调仍然活着**，那条链路同样受上面的并发规则约束）。Stripe/Creem/Waffo 未配置。
- **已知未覆盖的风险（产品决策，不是 bug）**：支付宝侧**退款/拒付不回收额度**——我们只处理 `TRADE_SUCCESS`/`TRADE_FINISHED`，用户付款到账后再在支付宝侧退款或被拒付，额度不退。要堵这个口子需要接入退款/拒付通知并做额度回收，属功能需求。

### Project Governance

**Required attribution:** The footer must always include a line crediting the original project:
- `footer.newapi.projectAttributionSuffix` i18n key and its translations MUST remain intact
- The footer copyright line must retain `Designed and developed by New API` (or translated equivalent)

**License headers:** All source file copyright headers (`Copyright (C) 2023-2026 QuantumNous`) must be preserved as required by the AGPL-3.0 license.

**Pull requests:** When creating a pull request:

- First compare the current git user (`git config user.name` / `git config user.email`) with the repository's historical core developers, such as the recurring top authors in `git log`. Do not change git config.
- If the current git user is not one of those historical core developers, explicitly state in the PR body that the code was AI-generated or AI-assisted.
- Always use the repository PR template at `.github/PULL_REQUEST_TEMPLATE.md` when drafting the PR title/body. Preserve the template structure and fill in the relevant sections instead of replacing it with an ad hoc format.
