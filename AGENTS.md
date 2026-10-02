# AGENTS.md — Project Conventions for new-api

DO NOT send optional commentary

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

- Production Go backend releases MUST use the remote-build flow: SSH to the production server, check out the exact committed SHA there, build `Dockerfile.deploy` on the server, then recreate the backend container and run the health checks.
- A local Docker daemon MUST NOT be a prerequisite for backend deployment. Do not ask the user to start Docker Desktop or build the production image locally when the production server can build it remotely.
- The frontend and backend have separate deployment paths. `deploy/deploy-frontend.sh` is for the frontend static release only; backend changes must use the backend remote-build deployment flow and must not be treated as frontend-only changes.
- Before deployment, keep the source checkout pinned to the exact commit being released, preserve unrelated local files, use the smallest necessary production change, and verify the container status, application health endpoint, and relevant business API after release.

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

- 通道定位：`relay/channel/task/manwu/` 经 OpenAI 兼容 `POST /v1/videos` 承接四个远端模型，再转 ArcReel 远端建单（默认基址 `https://arcreel.heibaidao.cn`，提交路径 `/api/v1/remote-generation/jobs`）。完整链路：ArcReel 本地任务 → 本中转（heibaidao）→ ArcReel 远端建单 → worker（燃境 App）→ 目标官网。排查必须按此链路逐段确认，不要跳段。
- 模型路由表（`adaptor.go::modelSpecs`，四个模型共用一条建单链路，差异只在 platformId/outputMode/白名单；新增模型先加表项，禁止新开 adaptor）：
  - `dola-seedance-2.5` → `platformId: dola` + `outputMode: video`（历史模型，行为不得改）
  - `gemini-web-video` → `platformId: gemini` + `outputMode: video`。**故意不叫 `veo-*`**：官方 Gemini 渠道已占那些名字，重名会被路由到错渠道
  - `manwu-image` → `platformId: manwu-image`（ArcReel 抽象平台，服务端按 `MANWU_REMOTE_IMAGE_PROVIDER` 决定实际走 gemini/jimeng，客户端不感知）+ `outputMode: image`
  - `jimeng-video-reverse` → `platformId: jimeng` + `outputMode: prompt`，**文本出参**（`prompt` 可选；视频入参恰好 1 个 http(s) URL）
- 拒绝口径统一：官网没有可实现控件的参数**一律 400，不静默丢弃**（Worker 侧 `RemoteTaskAdapter` 对 Gemini/Veo 的 unsupported params 同样 fail-fast）。Veo 不接受 `seconds`/`duration`/`resolution`；图片与反解不接受时长/分辨率/比例。反解的 multipart 文件直传报 `invalid_input_reference`（提示改传公网 URL）。
- 结果收敛分两类（`ParseTaskResult` / `VideoProxy::resolveManwuResultURL`）：
  - ArcReel 托管产物（Gemini 图片 blob、Veo 的 data: mp4）的 `sourceUrl` 是**相对路径**且需渠道密钥 → adaptor **不吐** URL，让 new-api 落成 `/v1/videos/{task_id}/content` 代理，由 `controller/video_proxy.go` 的 `ChannelTypeManwu` 分支回查 job 取 sourceUrl 再带密钥下载。
  - dola/tiktok 这类公网 CDN 直链照旧透传，**绝不带渠道密钥**（否则凭证泄露给第三方 CDN）。禁止去掉这个区分。
- 文本结果链路：`relaycommon.TaskInfo.ResultText` → `model.TaskPrivateData.ResultText`（JSON 列，免迁移）→ `dto.TaskDto.ResultText`；OpenAI video 响应里落在 `metadata.prompt`（`metadata.media_type = "text"`）。新增文本型任务沿用这条链路。
- 计费：`setting/ratio_setting/model_ratio.go` 定义单价；`gemini-web-video` / `jimeng-video-reverse` 按次（`EstimateBilling` 返回 nil）；`manwu-image` 按张，张数走 `EstimateBilling` 的 `{"n": N}` 倍率。
  - ⚠️ **`manwu-image` 绝不能进 `TASK_PRICE_PATCH` 环境变量**：进了会被 `relay_task.go` 当按次计费跳过倍率相乘，n 张只扣 1 张的钱（静默少收费）。`dola-seedance-2.5` 在该变量里，属正常。
- 提交耗时：反解的参考视频由 ArcReel 代下载（≤100MB，上限对齐 Worker 硬限制），`POST /v1/videos` 可能阻塞 1–2 分钟。本中转 `RELAY_TIMEOUT` 默认为 0（不超时），不要为了「快」把它调到 60s 以下，否则大视频会出现「网关报错但上游已建单」。
- 模型广场可见性：`gemini-web-video` / `manwu-image` 已在 `model/pricing.go isAllowedPricingModel` 放行；`jimeng-video-reverse` **故意未放行**——¥1.0/次是占位价，商务核定前不得对外展示价格（白名单只管广场，不影响调用）。
- 本地错误码均为 400：`invalid_model` / `invalid_request` / `invalid_duration` / `invalid_ratio` / `invalid_count` / `invalid_input_reference`，改动错误语义时同步更新 `adaptor_test.go` 的拒绝用例表。
- 测试：`go test ./relay/channel/task/manwu/`。改上限必须同步维护边界用例（dola 10 张放行见 `TestValidateAcceptsTenReferenceImages`，11 张拒绝见 `TestValidateRejectsIllegalRequests` 的 `too many images`；图片张数上界、反解单视频约束各有独立用例），禁止只改常量不改测试。
- 双仓库同口径：ArcReel 侧 `lib/video_backends/openai.py` 的 manwu 分支（`_MANWU_MODEL_PATTERN` / `_MANWU_MAX_REFERENCE_IMAGES` / `_manwu_ratio`，ratio 形态 + 参考图公网 URL 透传）必须与本通道同口径。任一侧改上限、比例档或字段语义，必须双侧同步改、同步发版，否则一侧放行另一侧 400。注意 ArcReel 的部署脚本只更新 ArcReel 两台服务器，不会更新本中转；本中转发版走上面的 Deployment Rules 远程构建流程。

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

- **恢复被 AutoBan 的渠道必须同时改 `channels.status` 与 `abilities.enabled`**。2026-10-02 的真实事故：AutoBan 禁用渠道时连带把能力表置 `enabled=false`，事后只把 `status` 改回 1，导致渠道「看起来正常」但该渠道**所有模型 503 `model_not_found`**，持续数小时无人察觉。恢复用 `scripts/restore-channel.sh <id>`，它两边一起改并打印复核。
- **裸 SQL 建/改渠道后必须重启 new-api**：能力走内存缓存（`CacheGetRandomSatisfiedChannel`），SQL 改 `abilities` 对**新建**渠道不生效（对已缓存的旧渠道反而会立刻生效，所以容易误判成「缓存已刷新」）。走管理 API 建渠道不会有这个问题。
- **验证脚本不得用能通过校验的入参**。`dola-seedance-2.5` 的 `n` 字段不参与校验，拿它当「探针」会**真建单并扣费**（本项目已因此误建 2 次 dola 任务）。规则：① 只用必然被本地校验拦下的输入；② 每次跑完断言 `used_quota` 差值为 0、`tasks` 无非终态任务；③ 一旦误建，必须**同时**在 new-api（取消 + 退款 + 标记日志）与 ArcReel（`POST /remote-generation/jobs/{id}/cancel`）两侧撤销——只撤一侧的话 Worker 上线后仍会去跑那个排队任务。
- 部署走 `scripts/deploy-new-api.sh <sha>`：它带 **SHA 前缀断言**（曾出现「构建了旧 commit 却以为成功」）、fetch 失败时校验本地是否已有该 commit、旧容器只保留最近 3 个。生产容器不是 compose 管理，必须按旧容器原参数 `docker run` 重建。

### Project Governance

**Required attribution:** The footer must always include a line crediting the original project:
- `footer.newapi.projectAttributionSuffix` i18n key and its translations MUST remain intact
- The footer copyright line must retain `Designed and developed by New API` (or translated equivalent)

**License headers:** All source file copyright headers (`Copyright (C) 2023-2026 QuantumNous`) must be preserved as required by the AGPL-3.0 license.

**Pull requests:** When creating a pull request:

- First compare the current git user (`git config user.name` / `git config user.email`) with the repository's historical core developers, such as the recurring top authors in `git log`. Do not change git config.
- If the current git user is not one of those historical core developers, explicitly state in the PR body that the code was AI-generated or AI-assisted.
- Always use the repository PR template at `.github/PULL_REQUEST_TEMPLATE.md` when drafting the PR title/body. Preserve the template structure and fill in the relevant sections instead of replacing it with an ad hoc format.
