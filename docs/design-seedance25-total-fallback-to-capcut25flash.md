# 规则八：全量兜底（`seedance2.5-特惠` / `seedance2.5-活动` → `cp-seedance-2.5`）

状态：**已实施**（2026-11-15）

> ⚠️ **2026-11-16 晚目标换档**：本文成文时的目标模型是 `cp-seedance-2.5flash`，
> 现已改为 `cp-seedance-2.5`（满血档）—— 老板口径「特惠/活动路由到 CapCut 用
> 2.5，保证质量做口碑」。同时换档的还有规则七（电商）与规则九（音视频参考）；
> 豆包 `seedance-2-5` 的备选备胎 `failover_alt_model` **仍是 2.5 Flash**，不随之变更。
> 下文提到 `cp-seedance-2.5flash` 的地方按「那一档的出口」理解，其余设计不变
> （准入、480p 生成 + 超分交付、计费原点口径全部保持一致）。

## 一、要解决的问题

`seedance2.5-特惠` 与 `seedance2.5-活动` 的**常驻链路是有赞（ch17 / prime）**。
前七条条件路由规则都建立在同一个隐含前提上：

> 源链路至少能跑一部分请求 —— 我们只是把其中一部分**优化性地**挪到别的链路上。

有赞彻底不能用时（渠道被关、账号封、上游大面积失败），这个前提不成立：
不管规则怎么配，请求都只能打到一条已经死掉的链路上。

规则八就是这条前提破了之后的**一键逃生口**：打开开关，
这两个模型收到的所有请求无条件改派 `cp-seedance-2.5flash`。

## 二、开关与接口

| 项 | 值 |
|---|---|
| option 字段 | `total_fallback_enabled`（bool，缺省 false） |
| 管理接口 | `POST /api/video-route/policy/total-fallback`，body `{"enabled": true}`（AdminAuth） |
| 后台入口 | 渠道抽屉 → 「全量兜底（所有请求 → CapCut）」卡片，打开需二次确认 |
| 目标模型 | 常量 `cp-seedance-2.5flash`（**不做成配置项**，见下） |

目标模型为什么不可配：规则八复用 `prepareCapcutRoute` 的准入与改写，
它只适配了这一条契约（480p/720p、4–30 秒、CapCut 比例表）。开放成配置项后，
配成别的模型名**不是「不生效」，而是把请求改写到一条没适配过的链路上** ——
典型的「看起来配了、实际必挂」。要换目标就改常量并补对应准入。

## 三、优先级与「无条件」的边界

顺序（`middleware/seedance_route.go`，顺序即优先级）：

```
规则五（故障转移，源模型是 seedance-2-5，与本节无关）
规则八（全量兜底）        ← 本规则
规则七（电商内容）
规则三（首尾帧）
规则六（短时长分流）
规则一（时长 > 20 秒）
规则二（9:16 比例）
```

「无条件」去掉的是**业务触发条件**（时长、比例、提示词内容、首尾帧），
不是目标链路的**物理能力**。下面四类仍留在原链路：

| 情况 | 原因 |
|---|---|
| 1080P | CapCut 只卖 480p/720p，改派过去必 400 |
| 2–3 秒 | CapCut 时长下界 4 秒 |
| 带参考视频 / 参考音频 | **路由层现口径**一律不路由（见 `prepareCapcutRoute` ① ②）。上游 2026-11-15 已放开（音频≤5 / 视频≤5），准入尚未跟进，规则六/七/八共用这一份准入 |
| 自适应比例 + 带参考图 | CapCut 没有 adaptive 档，替他挑一档等于改需求 |
| 45 秒以上 | CapCut 上界 30 秒；**不做夹取**（夹取 = 按 45 秒卖、出 30 秒） |

留在原链路与改派过去都是失败（前者 503 无渠道、后者 400 参数非法），
但留原链路能保住一个准确的原因；改派过去会把「链路挂了」伪装成「参数不合法」，
客户只会去改参数，永远发现不了是我们的问题。

## 四、与规则总开关（`enabled`）的关系

规则八**不依赖** `enabled`（其它七条都依赖）。理由：它是在救火时用的，
不应该要求运营先确认「规则总开关是不是开着」。若它依赖 `enabled`，
就会出现「应急开关亮着、请求全部失败」这种最难排查的状态 ——
运营只会以为「CapCut 也挂了」。

代价：`enabled=false`（所有规则停用）时打开它仍会改派。这是有意的 ——
**停用规则 ≠ 链路可用**，两者是不同的事。

## 五、计费

改派不改变对客价格：客户买的是 `seedance2.5-特惠` / `-活动`，
账单按这两个模型的 `ModelPrice` 算。中间件记的秒数与 size 倍率都取**源模型口径**
（不覆盖就会被 CapCut adaptor 的档位差价超收一倍）。

## 五之一、生成档位：480p 生成 + 超分 720P 交付（2026-11-15）

改派到 CapCut 时**生成档固定 480p**，交付前用腾讯 MPS 超分到 720P
（`capcutGenerationResolution` → `relay/channel/task/manwu/capcut_upscale.go`
的 `needsCapcutUpscale`）。CapCut 的 720p 是 480p 的 **2 倍 credits**
（2.5 Flash 30 秒：510 vs 240），超分单价低于这半个档位，净省成本；
交付档位与售价都不变。

- 超分链路（`TencentMPSSetting`）没配时**退回 720p 直接生成** ——
  宁可多花 credits 也不能「按 720P 的价交付 480P 的片」。
- 规则五备选链路（豆包 → CapCut）早已是这套口径，两条链共用同一个超分钩子。
- 客户**直接购买** `cp-seedance-2.5flash` 不触发超分：买什么档交付什么档。
按次档（活动版）由 `applyRouteBillingSeconds` / `applyRouteBillingSizeRatio`
按 `BillingModelName` 判定，天然不叠秒数与倍率。

## 六、明确不做的事

- **不自动触发**：不做「有赞不可用」的自动探测 / 自动开开关。
  判定链路是不是真的死了，人工比探针可靠；自动误判的代价是**全站流量挪到另一条上游**。
- **不做渠道预检**：不像其它规则那样先看源链路有没有渠道 ——
  本规则的使用场景恰恰是「源链路已经不可用」，先看它会永远不生效。
  目标链路（CapCut）的可用性仍然预检（没有渠道就不改派，让请求落回源链路）。
- **不自动恢复**：没有截止时间。源链路恢复后**必须人工关掉** ——
  这是刻意的：开着它的成本结构与正常链路完全不同，不该在没人注意时继续跑。

## 七、代码位置

| 关注点 | 文件 |
|---|---|
| 开关、判定、目标常量 | `setting/seedanceroute_setting/total_fallback.go` |
| 中间件判定与改写调用 | `middleware/seedance_route.go`（规则八段）、`middleware/seedance_route_ecommerce.go`（`prepareTotalFallbackRoute`） |
| 管理端接口与状态字段 | `controller/video_route_total_fallback_admin.go` |
| 路由注册 | `router/api-router.go` → `POST /api/video-route/policy/total-fallback` |
| 后台卡片 | `web/default/src/features/video-route/components/total-fallback-card.tsx` |
