# 图片模型：两轨协议与实测边界

涉及 `Nano Banana Pro` / `Seedream 4.5` / `Seedream 5.0 Flash` / `gpt-image-2.5-官方`、
`relay/channel/task/manwu` 的图片形态、或 m3only 沙箱组时必读。
价格与当前路由查实际配置（`/api/pricing`、`abilities`），本文只记**协议差异**与**昂贵反证**。

## 两轨协议，不可混用

| 模型 | 协议 | 实测耗时 | 客户侧成本（2026-11-07 口径） |
|---|---|---|---|
| gpt-image-2.5-官方 | 同步 `POST /v1/images/generations`，**只回 b64_json、无 url** | 40.6s | ¥0.15 |
| Nano Banana Pro | 任务协议 `POST /v1/videos` → 轮询 `GET /v1/videos/{id}` → `GET /v1/videos/{id}/content` 取 PNG | ~110s | ¥0.30 |
| Seedream 4.5 | 同上（轮询约 160s，客户端 timeout 至少 300s） | ~160s | ¥0.20 |
| Seedream 5.0 Flash | 同上 | ~50s | ¥0.20 |

- 后三档走 `/v1/images/generations` 一律 **404** —— 别指望「统一成 images 接口」。
- 成本随价目变动，改价看 `options.ModelPrice`，不看本表。

## manwu（type=66）图片形态硬约束

- 参考图只收**公网 http(s) URL**：data URI → 400 `invalid_input_reference`，不建单不扣费。
- 比例白名单随模型形态：`kindImage` 六档（缺省 16:9）、`kindSeedreamImage` 八档（含「自动」，缺省不下发）。
- 图片形态**拒收** `seconds` / `duration` / `resolution` / `negative_prompt`。
- 张数上限：`kindImage` 10 张、`kindSeedreamImage` 4 张。
- 消费侧兜底链在 `ArcReelLocalDesktop/scripts/cloud_image_fallback.py`（双协议自动分派，单模型失败一次即切换），配置 `ARCREEL_CLOUD_IMAGE_*`（arcreel 机 `/opt/arcreel/gateway.env`）。

## 参数矩阵实测（2026-11-07，台账 `scripts/prod-ledger-img4-paramatrix-20261107.sql`，¥6.40）

结论仍适用，**未复测**；改动对应渠道前先按本条预期验一次。

- **Seedream 4.5**（dola）：8 档比例 + `count=2` + 参考图全过；**部分档交付低清预览**
  （9:16 216×384/134KB、3:4 288×384、4:3 384×288、ref 384×384），同档其它次为 5–6MB 全清 ——
  疑取图节点抓到站点预览图，未定位。
- **Seedream 5.0 Flash**（doubao）：8 档比例 + `count=2` 全过（1536×1536 ~ 2848×1600）；
  **参考图当时不可用** —— 豆包官网无 `input[type=file]` 上传入口，Worker 硬闸门拒单、
  未提交不扣费，换号无效（页面结构变更后需重新确认）。
- **Nano Banana Pro**（gemini）：比例**只有 3:4 / 16:9 生效**，9:16 与 1:1 都实出 1024×559（16:9 缺省）——
  Worker `buildFillImageParamsScript` 找不到宽高比控件时按设计跳过（`image-params-skipped:aspect-ratio-unavailable`），
  **不报错**。唯一 gemini 账号掉登录后整链排队/取消，须重登。
- new-api 侧负向校验 3/3 通过（免费）：非法比例、`seconds` 字段、data URI 参考图均 400。

## m3only 沙箱组（视频优先级饿死图片时的服务器侧兜底）

用户 id=53 `m3only_user` / token id=166 `m3only受限令牌`（key 前缀 `FOd34WzRQY`），
`model_limits` = `MiniMax-M3.1,Seedream 4.5,Nano Banana Pro,Seedream 5.0 Flash,gpt-image-2.5-官方`，
`unlimited_quota=t`、`cross_group_retry=f`。

专属通道：ch26 MiniMax-M3.1(type 35)、ch29 GPT-Pool-image(type 1，克隆 ch13)、
ch30 漫屋ArcReel-image(type 66，克隆 ch20)。
改动前先备份：`bak_20261107_img_channels` / `bak_20261107_img_abilities`。

## gpt-image-2.5-官方 现状（2026-11-25 复查，未解决）

- 唯一出图账号 = sub2api group 4 的 `codex-plus-snehitoge828`（account 4），
  `schedulable=false / status=error`（OAuth 被上游吊销 `Token revoked (401)` 后未恢复），
  调用只剩 503「No available compatible accounts」（sub2api 日志 `account_select_failed ... pool=1`）。
- 因此 ch13（default 组的 GPT-Pool）已 `status=2` 手动禁用、`abilities.enabled=false`；
  仅 ch29（m3only 组）还有路由，`/api/pricing` **不下发**该模型 ——
  **普通客户实际上买不到**，别在文档/广场里当在售模型描述。
- 恢复须在 sub2api 管理端重新授权该账号或补账号进 group 4 ——
  **禁止自动触发 OAuth 恢复**，先报用户拍板。
