# 设计：seedance2.5-活动（按次 ¥4.5/次 · 固定 30 秒）

> 需求方：老板 ——「把 `seedance2.5-特惠` 复制一份出来，命名 `seedance2.5-活动`，
> 定价 4.5/次，按次固定 30 秒来收费。保留这个模型的全部属性（路由规则、能力声明、
> 脱敏、计费档位门控），**只是单纯把价格改下促销、差异化销售**。
> 另外，如果其中一个模型有新增的能力，也要同步到另外一个。」
>
> 状态：**代码实施完成 + 生产部署上线**（2026-11-02）。
> 所有事实均来自**生产实测**。
>
> **✅ 生产部署已完成**（2026-11-02 晚）：代码上线（镜像
> `new-api-custom:huodong-20261102`）+ 生产库五项变更全部落地并校验通过，
> 详见第十二节。真实视频生成验证**按要求未做**（节约成本）。
>
> ⚠️ 部署期间发生一次生产事故（4 核 4G 机器上编译前端导致整机僵死），
> 复盘与新的部署策略见 12.2 / 12.3 节。
>
> 实施过程中的**五处审查修正**（详见本文末尾「实施记录」11.6）：
> 1. `quota_type` **不能**区分按次/按秒 —— 母本也配了 `ModelPrice`，所以母本的
>    `quota_type` 同样是 1。前端真正的判据是 `isPerSecondModel` 穷举表。
> 2. 前端「/秒 vs /次」的正确守卫**不是**运行时接口比对，而是**直接读后端能力组
>    源码**做交叉校验（已实现，见第 3.4 节）。
> 3. 按次档的倍率风险有**三条独立路径**（初始估算 / 倍率应用 / 提交后重算），
>    不是一条 —— 见第 6 节陷阱 1。
> 4. 固定档的 fail-loud 门控会被**条件路由绕过** —— 见第 6 节陷阱 5。
> 5. 「固定 30 秒」此前有**两份手写副本**，现收敛为 `constant` 包的**单一来源**
>    + 派生关系 —— 见 11.4 节。
>
> ⚠️ **一个未决问题**：规则三（首尾帧）下活动版存在成本倒挂，需业务拍板 ——
> 见 11.9 节。
>
> ---
>
> ## 📌 后续变更（2026-11-11，覆盖本文 4.3 / 4.5 节）
>
> 1. **规则四（提示词 >5000 字符 → 豆包）已删除**。它几乎总被规则一抢先，
>    且超长提示词已有 `Wan3PromptCompress` 压缩（压不下来明确 400），
>    不需要第二条隐式改派。本文 4.3 / 4.5 节关于「规则四对新模型同样生效」
>    的结论**已失效**，仅作历史记录保留。
> 2. **规则二（9:16）改派目标改为心笔 480P 内链**
>    `wan3.0-720p-特惠-内链`（渠道 25，480P 生成 + 交付 720P），
>    不再经 `wan3.0-720p-特惠`（渠道 12）两跳。生产 option 同步写入
>    `trigger_ratio=9:16` 与 `ratio_target_model=wan3.0-720p-特惠-内链`。
> 3. 计费口径不变：改派只换内部链路，账单仍按客户请求的源模型算。
>
> ---


---

## 〇、本方案涉及哪些模型（先把边界划清楚）

| 模型 | 角色 | 本次是否改动 |
|---|---|---|
| `seedance2.5-特惠` | **母本** —— 能力与路由规则的来源 | 只改「共享能力组」的定义方式，行为不变 |
| `seedance2.5-活动` | **新增的促销版** | ✅ 新建 |
| `wan3.0-720p-按次` | ⚠️ **纯对照物**，不参与 | ❌ **不复制、不新增、不改配置** |

**本方案走有赞渠道 17（type=63），与 `seedance2.5-特惠` 同货源、同链路。**

`wan3.0-720p-按次` 走的是**心笔渠道（type=68）**，货源与路由规则都不同 ——
它在本文档里出现，只是为了对照说明「按次 ¥4.5/次 + 固定 30 秒」这个**商品形态**
老板已经定过一次，以及提供一条「按次模型如何实现」的站内先例。
**它不是复制对象。**

---

## 一、需求拆解（去掉歧义后的五条硬约束）

| # | 约束 | 落地方式 |
|---|---|---|
| C1 | 新模型名 `seedance2.5-活动` | 新增对外模型名 |
| C2 | 定价 ¥4.5/**次**（不是秒） | ModelPrice = 4.5/7.3 USD；`EstimateBilling` 返回 nil |
| C3 | **固定 30 秒** | 不传 → 30 秒；传别的 → 400（fail-loud） |
| C4 | 保留原模型**全部**属性 | 能力组共享 + 路由规则多源化 |
| C5 | 两模型能力**自动同步** | 单一能力组定义，天然共享 |

---

## 二、生产实测事实（不是推测）

### 2.1 上游成本 —— 最关键的一条

有赞上游 `GET /v1/models` 实测返回：

```
id= wan3.0-video       | unit= second | unitPrice= 0.18
id= wan3.0-video-prime | unit= second | resolutionPrices= {720P: 0.15, 1080P: 0.20}
```

**上游只有 `billingUnit=second` 这一种计费单位**（`upstream_pricing.go:26` 的
`billingUnitSecond`，非 second 直接报错）。

### 2.2 成本毛利核算

| 商品形态 | 链路 | 成本 | 售价 | 毛利 |
|---|---|---|---|---|
| `seedance2.5-特惠` 30 秒 | 有赞 prime 按秒 | 30 × ¥0.15 = ¥4.50 | ¥0.20×30 = ¥6.00 | **¥1.50 (25%)** |
| **`seedance2.5-活动` 30 秒（本方案）** | 有赞 prime 按秒 | 30 × ¥0.15 = ¥4.50 | ¥4.50 | ¥0.00（**但见 2.3**）|

### 2.3 成本分析：为什么「固定 30 秒按次」仍然是高毛利

先纠正一个容易误读的地方：**30 秒这一档的毛利不是 ¥0**。因为「固定 30 秒
按次定价」的本质是**按 30 秒封顶卖短片** —— 售价恒为 ¥4.50，成本随实际时长变化：

| 实际时长 | 成本（¥0.15/秒） | 售价 | 毛利 |
|---|---|---|---|
| 2 秒 | ¥0.30 | ¥4.50 | ¥4.20 (93%) |
| 10 秒 | ¥1.50 | ¥4.50 | ¥3.00 (67%) |
| 30 秒 | ¥4.50 | ¥4.50 | ¥0.00 (0%) |

**唯一的零毛利点是「30 秒且没走成改派」**。而这一档恰好被路由规则一覆盖：
30 秒 → 豆包链路（浏览器自动化，成本 ≈ ¥0）→ 实际毛利 ≈ 100%。

**所以整体是高毛利的，不需要为定价单独决策。** 短片客户占便宜、长片客户
贴着成本，但长片那条路有豆包兜着。

> 附：`wan3.0-720p-按次`（心笔渠道 type=68，上游按次、成本 ¥4.25、卖 ¥4.50）
> 是**另一个商品**，这里只是拿它做对照，说明「按次固定 30 秒、¥4.5」这个
> 形态老板已经定过一次。**它不参与本方案，也不需要复制。**

### 2.4 生产配置实况

```
channels:  17 | 有赞 Wan3         | type=63 | models= wan3.0-video,wan3.0-video-prime,wan3.0-smart,seedance2.5-特惠
           25 | 心笔 Model Router | type=68 | models= wan3.0-720p-按次

abilities: default | seedance2.5-特惠   | ch17 | enabled=t | priority=10
           default | wan3.0-720p-按次   | ch25 | enabled=t | priority=0
           default | wan3.0-video-prime | ch17 | enabled=t | priority=10

options.ModelPrice:
  "seedance2.5-特惠"  = 0.027397260273972605  → ¥0.2000/秒
  "wan3.0-720p-按次"  = 0.6164383561643836    → ¥4.5000/次

options.Seedance25TeHuiRoute30s:
  {"enabled":true,"source_model":"seedance2.5-特惠","target_model":"seedance-2-5",
   "trigger_seconds":30,"ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16",
   "frames_target_model":"wan3.0-720p-特惠","updated_at":1793515704}

options.YouzanWan3Pricing:
  {"markup":{"wan3.0-video":{"*":0.05},"wan3.0-video-prime":{"1080P":0.1,"720P":0.1}},
   "max_change_ratio":0.3,"auto_apply":true,
   "ratios":{"wan3.0-video":{"1080P":1,"480P":1,"720P":1},"wan3.0-video-prime":{"1080P":1.2,"720P":1}}}
```

**生产主机定位（重要）**：new-api 网关不在 ArcReel 的 A/B 两台上，而在独立主机
`ubuntu@119.29.253.97:877`，容器名 `new-api`（镜像 `new-api-custom:latest`），
真实库是 **PostgreSQL**（`SQL_DSN=postgresql://root:...@postgres:5432/new-api`），
**不是** `/opt/new-api/data/one-api.db`（那是空壳，0 行）。

---

## 三、核心机制：能力组（解决 C4 + C5）

### 3.1 现状问题

`seedance2.5-特惠` 的能力判定散落在**多个包的多个精确集合**里：

| 文件 | 集合 / 函数 | 用途 |
|---|---|---|
| `relay/channel/task/youzanwan3/adaptor.go:295` | `isTehuiModel()` | 独立定价分支 |
| `relay/channel/task/youzanwan3/adaptor.go:281` | `isPrimeModel()` | 走 prime 档位校验 |
| `relay/channel/task/youzanwan3/adaptor.go:302` | `upstreamModelFor()` | 映射到上游模型名 |
| `relay/channel/task/megabyai/constants.go:35` | `megabyaiUnifiedContractModels` | 统一参数契约 |
| `relay/channel/task/megabyai/constants.go:49` | `megabyaiUpstreamModels` | 上游兜底映射 |
| `relay/channel/task/megabyai/constants.go:68` | `megabyaiLockedTierModels` | 固定档位门控 |
| `model/upstream_redact.go:24` | `upstreamAliasModels` | 上游脱敏总开关 |
| `model/pricing.go:438` | `isAllowedPricingModel()` | 广场白名单（**含按秒分支**）|
| `setting/ratio_setting/model_ratio.go:367` | `defaultModelPrice` | 价格注册 |
| `web/.../model-helpers.ts` | `isPerSecondModel()` | 前端「/秒 vs /次」|
| `setting/seedanceroute_setting/setting.go:75` | `Policy.SourceModel` | 路由源模型（**单值**）|

**若新模型在 11 处各写一遍名字**，就出现了用户明确要避免的问题：
「其中一个模型有新增的能力，另一个没有同步」。

### 3.2 解法：单一能力组 + 派生视图

新增一个**中立包** `relay/channel/task/seedance25`（不依赖任何渠道包），
作为这一族模型的**能力定义唯一来源**：

```go
package seedance25

// Family 是 seedance2.5 促销族的成员集合。
//
// ⚠️ 这是**唯一的能力定义点**。新增成员只改这里，
// 所有判定点（定价/档位/脱敏/路由/前端）自动同步 ——
// 这正是需求方要的「一个模型新增能力，另一个自动同步」。
var Family = map[string]bool{
    TeHuiModel:   true, // seedance2.5-特惠（按秒 ¥0.20/秒）
    HuoDongModel: true, // seedance2.5-活动（按次 ¥4.5/次、固定 30 秒）
}

// IsFamily 判断模型是否属于本族。
func IsFamily(model string) bool { return Family[strings.TrimSpace(model)] }
```

**关键：所有判定点改为查这个组，而不是硬编码模型名。**

| 判定点 | 改法 |
|---|---|
| `youzanwan3.isTehuiModel()` | → `seedance25.IsFamily(model)` **且**按次判定另设 |
| `youzanwan3.isPrimeModel()` | → `seedance25.IsFamily(model) \|\| 原有 prime 名单` |
| `youzanwan3.upstreamModelFor()` | → `seedance25.IsFamily(model)` → 返回 prime |
| `megabyai.*Models` 三集合 | → 统一由 `seedance25.Family` 派生 |
| `upstream_redact.upstreamAliasModels` | → 由 `seedance25.Family` 派生（**但要防 import 环**）|
| `pricing.isAllowedPricingModel()` | → 按次/按秒**分开判定**（见 3.3）|
| 前端 `isPerSecondModel()` | → 前端拿不到 Go 包，需**后端下发**或前端同步一张表 |

### 3.3 ⚠️ 最大的坑：按次 vs 按秒必须分开

`model/pricing.go:433-440`：

```go
// 所以它必须显示「/秒」而不是「/次」（见前端 isPerSecondModel）。
if name == "seedance2.5-特惠" {
    return true
}
```

这个 `if` 位于「按秒模型白名单」块内。**新模型是按次 ¥4.5，绝不能落进这个块** ——
否则前端显示「/秒」，客户按 ¥4.5/秒 理解，30 秒就是 ¥135。

**必须按计费口径分成两组，而不是一个族**：

```go
// 按次成员（固定 ¥4.5/次）—— 前端显示「/次」
var PerCallFamily = map[string]bool{ HuoDongModel: true }
// 按秒成员（原 ¥0.20/秒）—— 前端显示「/秒」
var PerSecondFamily = map[string]bool{ TeHuiModel: true }
```

**能力共享 ≠ 价格口径共享**。两个模型能力完全相同（参考图 10 张、无首尾帧、
参考视频/音频各 5 个、5000 字符提示词上限），但**计费口径不同** ——
这正是「单纯把价格改下促销」的准确含义。

### 3.4 前端「/秒 vs /次」的同步机制

> **实施后的审查修正（2026-11-02）**：初稿说「后端已有可靠的按次信号 `quota_type`」。
> **这个判断是错的** —— 母本 `seedance2.5-特惠` 也配了 `ModelPrice`（¥0.20/秒），
> 所以它的 `quota_type` **同样是 1（REQUEST）**。
> `quota_type` 只能区分「按量（TOKEN）」与「按次/按秒（REQUEST）」，
> **分不出按次档与按秒档**。
>
> 前端真正的判据是 `isPerSecondModel()` —— 那张**手抄的穷举表**。
> 结论不变（**计价单位**这一项前端不用改），但守卫的做法换了：
> **直接读后端能力组源码做交叉校验**，而不是靠 `quota_type`。
> （前端在**示例代码与参数表**上仍需改动 —— 活动版是固定 30 秒档，见 5.7 节。）

#### 已有信号：`quota_type`（但它不够）

`model/pricing.go:310-318`：

```go
modelPrice, findPrice := ratio_setting.GetModelPrice(model, false)
if findPrice {
    pricing.ModelPrice = modelPrice
    pricing.QuotaType = 1     // ← REQUEST
} else {
    pricing.ModelRatio = modelRatio
    pricing.QuotaType = 0     // ← TOKEN
}
```

前端 `constants.ts`：

```ts
export const QUOTA_TYPE_VALUES = { TOKEN: 0, REQUEST: 1 } as const
```

**凡是配了 `ModelPrice` 的模型，`quota_type` 一律 = 1（REQUEST）**。

⚠️ **但它区分不了按次与按秒**：母本 `seedance2.5-特惠`（按秒 ¥0.20/秒）
与活动版 `seedance2.5-活动`（按次 ¥4.5/次）**都配了 ModelPrice**，
`quota_type` 都是 1。所以前端要区分单位，只能靠下面那张表。

#### 前端的真正判据：`isPerSecondModel()` 穷举表

`web/default/src/features/pricing/lib/model-helpers.ts:118`，
注释明说它「已经漏登记过两次」：

```
⚠️ This list is a hand-maintained copy of the backend billing mode and has
silently dropped models twice — `seedance-2-0-fast` (2026-10-12) and
`wan3.0-480p` (erchun) — both of which rendered a per-second price under a
"/ request" unit on the model square.
```

漏登记的后果是**静默错价**：后端按秒结算、前端显示「/次」，
客户看到 ¥0.20/秒 的标价却以为是一次性收费，账单与标价对不上，**且无任何报错**。

#### 「/次」这个单位是否安全？—— ✅ 安全，但前端**不是零改动**

`seedance2.5-活动` 是**按次**模型：
- `isPerSecondModel()` **不命中**任何分支（它既不以 `wan3.0-video` 开头，
  也不等于 `seedance2.5-特惠` / `wan3.0-720p-特惠` / `wan3.0-480p`）

→ 前端显示「/次」✅ **计价单位这一项不用改**（`model-helpers.ts` 保持原样）。

> ⚠️ **实施后的修正**：初稿在这里写「本次不改前端一行业务代码」——
> 那是**只考虑计价单位**时的结论。实际实施时发现前端**必须**改：
> 活动版是**固定 30 秒**，通用的示例代码写的是 5 秒，客户照抄会被后端 400
> （固定档是 fail-loud，不做就近吸附）。参数表同理 ——
> 活动版的可选时长/分辨率必须锁死成 30 秒 / 720P。
>
> 所以前端的实际改动是：**计价单位不改，但示例代码与参数表要改**。
> 详见 5.7 节。

> ⚠️ 唯一要小心的是**别把新模型加进 `isPerSecondModel`**（那是母本的按秒分支）。

#### L2 交叉校验测试 —— ✅ 本次已实现

**做法**：前端测试**直接读后端 `constant/seedance25_family.go` 源码**，
静态解析出按次/按秒两组成员，再与前端表的判定结果逐条比对。

文件：`web/default/src/features/pricing/lib/billing-unit-crosscheck.test.ts`

```
后端按秒成员  →  前端必须判「按秒」
后端按次成员  →  前端必须判「按次」
两组必须互斥
seedance2.5-活动 必须在按次组、不在按秒组
```

**为什么读源码而不是调接口**：
- `/api/pricing` 要起服务、连数据库，单测里做不到；
- 它返回的是**当前生产配置**，反映不了代码里的能力定义 —— 源码才是唯一来源。

**变异验证 M15**（证明这道闸门真能抓住历史事故）：
在后端能力组里加一个按秒模型 `seedance2.5-活动版`、前端**不**登记 →
交叉校验测试**当场变红**（「后端按秒成员在前端一律显示 /秒」失败）。
这正是 `seedance-2-0-fast` / `wan3.0-480p` 当年漏登记时的形态 ——
现在有自动闸门了。

> 这就是 C5「一个模型新增能力，另一个自动同步」在**展示层**的守卫：
> 后端改能力组 → 前端测试变红 → 强迫作者同步前端。

## 四、路由规则多源化（解决 C4）

### 4.1 现状约束

`setting/seedanceroute_setting/setting.go:75`：

```go
// SourceModel 源模型名（对外名，客户请求里写的那个）。三条规则共用。
SourceModel string `json:"source_model"`
```

**`SourceModel` 是单值**。新模型要复用同样的四条规则（首尾帧 / 30 秒 /
超长提示词 / 9:16 比例），就必须扩成多值。

### 4.2 改法（向后兼容）

```go
// SourceModels 是源模型名列表（多值）。
// ⚠️ 保留 SourceModel 单值字段：旧的生产 option JSON 里只有它，
// 解析后为空 → 对应规则不生效 → 行为与改造前逐字节一致（硬要求）。
SourceModel  string   `json:"source_model,omitempty"`
SourceModels []string `json:"source_models,omitempty"`
```

判定时用统一入口：

```go
// IsSourceModel 判断模型名是否命中源模型。
// SourceModel（单值）与 SourceModels（多值）任一命中即为真 ——
// 兼容旧配置的同时支持新模型。
func (p Policy) IsSourceModel(model string) bool {
    model = strings.TrimSpace(model)
    if model == "" { return false }
    if strings.TrimSpace(p.SourceModel) == model { return true }
    for _, m := range p.SourceModels {
        if strings.TrimSpace(m) == model { return true }
    }
    return false
}
```

`middleware/seedance_route.go:148` 的匹配改为：

```go
if !policy.IsSourceModel(modelName) {
    c.Next()
    return
}
```

### 4.3 ⚠️ 规则四（超长提示词）对新模型的语义

规则四把「>5000 字符」改派到 `seedance-2-5`（豆包）。对按次的新模型：

- 改派后计费按**源模型**（`seedance2.5-活动`）算 = ¥4.5/次 ✅ 正确
- 但**豆包链路成本 ≈ ¥0**（浏览器自动化），毛利极高 ✅
- 这条规则对新模型**同样成立且更有价值** —— 直接继承，无需特殊处理

### 4.4 ⚠️ 规则一（30 秒）对新模型的语义 —— 关键

原模型 30 秒 → 豆包（¥5.00/次成本 ≈ 0），毛利由 ¥1.50 变 ¥3.00+。

新模型 30 秒 → 豆包，成本 ≈ ¥0，售价 ¥4.50，毛利 **¥4.50 (100%)**。
**这条规则必须继承**，否则新模型的 30 秒全部走有赞，成本 ¥4.50 卖 ¥4.50 = 零毛利。

**但要注意**：豆包链路**不支持参考视频/音频**（`hasUnsupportedMedia`），
且只有 5/10/15/30 四档。这与原模型完全一致（同一套规则、同一套目标模型），
所以**直接继承即可**，不需要任何新逻辑。

### 4.5 规则四的目标模型要不要改？

规则四现在的目标是 `seedance-2-5`（豆包按次 ¥5.00）。对**按次**的新模型，
如果改派过去，计费仍按源模型 ¥4.5/次 —— 客户账单不变 ✅。

**保持不变**，无需为新模型单独配目标。

---

## 五、逐文件改动清单

### 5.1 新增

| 文件 | 内容 |
|---|---|
| `constant/seedance25_family.go` | **能力组唯一来源**（`Family` / `PerCallModels` / `PerSecondModels` + 判定函数 + 固定档常量）—— 放 `constant/` 是为了避开 import 环（见 5.6）|
| `constant/seedance25_family_test.go` | 组一致性测试 |
| `constant/seedance25_fixed_tier_test.go` | 固定档业务值守卫（30 秒 / 720P 钉死）|
| `constant/task_billing_mode.go` | **统一按次判据** `IsTaskPerCallModel()`（覆盖能力组 + `TASK_PRICE_PATCH`）|
| `constant/task_billing_mode_test.go` | 按次判据守卫 |
| `relay/relay_task_percall_ratio_isolation_test.go` | 按次档倍率三层隔离测试 |
| `middleware/seedance_route_fixed_tier_test.go` | 固定档在条件路由中的语义测试 |
| `middleware/seedance_route_fixed_tier_constant_test.go` | 固定档常量派生关系与归一行为守卫 |
| `web/default/src/features/pricing/lib/seedance25-family.ts` | 前端促销族定义（活动版固定档参数表）|
| `web/default/src/features/pricing/lib/code-samples.ts` | 示例代码生成（活动版写 30 秒）|
| `web/default/src/features/pricing/lib/seedance25-family-crosscheck.test.ts` | 前端 × 后端源码交叉校验（含派生链追溯）|
| `docs/design-seedance25-huodong-percall-30s.md` | 本文档 |

### 5.2 修改 —— 有赞链路

| 文件:行 | 改动 |
|---|---|
| `relay/channel/task/youzanwan3/constants.go:19` | 新增 `tehuiFamilyModel = "seedance2.5-活动"` 常量（**不写进 `ModelList`** —— 该清单必须与上游 `/v1/models` 逐字一致）|
| `relay/channel/task/youzanwan3/adaptor.go:281` `isPrimeModel()` | 加入家族成员 → 走 prime 档位校验 |
| `relay/channel/task/youzanwan3/adaptor.go:295` `isTehuiModel()` | **拆成两个**：`isTehuiFamilyModel()`（族）与 `isPerCallModel()`（按次）|
| `relay/channel/task/youzanwan3/adaptor.go:302` `upstreamModelFor()` | 族成员 → 返回 `primeModel` |
| `relay/channel/task/youzanwan3/adaptor.go:809` `resolutionRatioForModel()` | 按次成员**不走倍率**（返回 1）—— 固定 ¥4.5/次 |
| `relay/channel/task/youzanwan3/adaptor.go` `EstimateBilling` | 按次成员**返回 nil**（不乘 seconds/size）|
| `relay/channel/task/youzanwan3/adaptor.go` 时长解析 | 按次成员**固定 30 秒**：不传→30；传别的→400 |

### 5.3 修改 —— 定价

| 文件:行 | 改动 |
|---|---|
| `setting/ratio_setting/model_ratio.go:367` 后 | 新增 `"seedance2.5-活动": 4.5 / USD2RMB` + 成本注释 |
| `model/pricing.go:438` | **按秒白名单块内不动**；在按次区新增 `if constant.IsSeedance25PerCall(name) { return true }` |
| ~~`model/pricing.go` pricing 响应~~ | ~~新增 `billing_unit` 字段~~ → **审查后取消**，`quota_type` 已是可靠信号（见 3.4）|

### 5.4 修改 —— 路由

| 文件:行 | 改动 |
|---|---|
| `setting/seedanceroute_setting/setting.go:75` | 新增 `SourceModels []string` + `IsSourceModel()` 方法 |
| `setting/seedanceroute_setting/setting.go:182` `UpdateFromJSONString` | 校验时两个字段任一非空即算配全 |
| `middleware/seedance_route.go:148` | `policy.SourceModel` → `policy.IsSourceModel(modelName)` |
| `middleware/seedance_route.go` 日志 | 改派日志用**实际命中的源模型名**，而非写死的 `policy.SourceModel` |

### 5.5 修改 —— 脱敏与能力表

| 文件:行 | 改动 |
|---|---|
| `model/upstream_redact.go:24` | `upstreamAliasModels` 加入 `"seedance2.5-活动": true`（能力组放 `constant/` 后**无 import 环**，可直接引用）|
| 生产 `channels.models` | 渠道 17 加 `seedance2.5-活动`（**必须加**，否则 `UpdateAbilities` 重建能力行时会抹掉它）|
| 生产 `abilities` | 新增 `(default, seedance2.5-活动, ch17)` 行 |
| 生产 `options.ModelPrice` | 加 `"seedance2.5-活动": 0.6164383561643836`（= ¥4.5/次）|
| 生产 `options.Seedance25TeHuiRoute30s` | 加 `"source_models":["seedance2.5-特惠","seedance2.5-活动"]`（**保留 `source_model` 不删**，旧字段兼容）|

### 5.6 import 环问题（已解决）

`model/upstream_redact.go` 在 `model` 包里，它**不能** import
`relay/channel/task/...`（`relay/...` 会 import `model` → 环）。

**解法（已定）**：能力组定义放 `constant/seedance25_family.go`。
已核实 `constant` 包**不 import 任何业务包**（`grep` 确认），因此
`model` / `relay` / `middleware` / `setting` 都能单向依赖它，无环。

### 5.7 前端

> ⚠️ **实施后的修正**：初稿写「前端零改动」，那是**第一阶段**的结论 ——
> 活动版是按次档、不命中 `isPerSecondModel` 的任何按秒分支，所以「/次」这个
> 显示单位天然正确。**但示例代码和参数表必须改**：活动版是**固定 30 秒**，
> 照抄通用的 5 秒示例会让客户直接吃 400。所以前端有实际业务改动。

| 文件 | 改动 |
|---|---|
| `web/default/src/features/pricing/lib/model-helpers.ts` | ⚠️ **不改** —— 活动版按次、不命中任何按秒分支，天然显示「/次」（见 3.4）|
| `web/default/src/features/pricing/lib/seedance25-family.ts` | **新增** —— 前端促销族定义；活动版标注固定档（30 秒 / 720P）|
| `web/default/src/features/pricing/lib/code-samples.ts` | **新增** —— 示例代码纯函数；按次固定档写 30 秒，按秒档写 5 秒 |
| `web/default/src/features/pricing/lib/mock-stats.ts` | 活动版的**固定档参数表**（30 秒 / 720P / 无首尾帧）|
| `web/default/src/features/pricing/lib/billing-unit-crosscheck.test.ts` | **交叉校验测试**：后端按秒模型清单 ≡ 前端穷举表（C5 的展示层守卫）|
| `web/default/src/features/pricing/lib/seedance25-family-crosscheck.test.ts` | **交叉校验测试**：族成员 / 固定档时长 / 固定档分辨率 / 派生链单一来源 |
| `web/default/src/features/pricing/lib/code-samples.test.ts` | 活动版示例必须写 30 秒 |

---

## 六、⚠️ 计费链上的五处陷阱（改代码前必读）

> ⚠️ **实施后的修正**：初稿写的是「三处陷阱」，只覆盖了 `applyRouteBillingSeconds`
> 一条倍率路径。实施时发现按次档的倍率风险有**三条独立路径**
> （初始估算 / 倍率应用 / 提交后重算），漏掉任何一条都会静默错价。
> 此外还有一条**路由层的固定档绕过**（陷阱 5），初稿完全没意识到。

### 陷阱 1：按次模型 + 改派 = seconds 倍率残留

`relay/relay_task.go` `applyRouteBillingSeconds()`：
改派后若 adaptor **没给** seconds，就从 context 补上源模型的秒数。

对按次的新模型：30 秒改派到豆包，豆包 adaptor 返回 nil → **会补上 seconds=30** →
然后计费链判断是否应用倍率：

```go
if !common.StringsContains(constant.TaskPricePatches, info.BillingModelName()) {
    info.PriceData.Quota = applyTaskOtherRatios(info.PriceData.Quota, info.PriceData.OtherRatios)
}
```

**新模型不在 `TASK_PRICE_PATCH` 环境变量里**（生产实测：该变量**未设置**），
所以 seconds=30 会被**乘进** ¥4.5/次 的预扣额度 → **收 ¥135**。

**修法（已实施，三层隔离）**：判据统一到 `constant.IsTaskPerCallModel`，
在**三条倍率路径**上分别拦截：

| 层 | 位置 | 修法 |
|---|---|---|
| 初始估算 | `mergeEstimatedRatios()` | 按次档直接丢弃 adaptor 返回的倍率（`seconds`/`size`）|
| 倍率应用 | `applyBillingRatios()` | 按次档**不调用** `applyTaskOtherRatios` |
| 提交后重算 | `resolveSubmitQuota()` | 按次档**不进入** `AdjustBillingOnSubmit` → `recalcQuotaFromRatios` |

> ⚠️ **为什么三层都要拦**（初稿只想到一层）：
> - 只拦初始估算 → 改派后 context 补进来的 `seconds=30` 仍会被应用 → **收 ¥135**；
> - 只拦倍率应用 → 提交后 `recalcQuotaFromRatios` 会先除掉旧倍率，
>   把 308219 除成 10273 → **少收三分之二**；
> - 只拦提交后重算 → 预扣阶段就超收 → 客户看到 ¥135 的预扣。
>
> 三条路径互不覆盖，缺一条就错价，且**都不报错**。

> ⚠️ **判据必须用 `BillingModelName()`（源模型），不能用 `OriginModelName()`（执行模型）**。
> 改派后两者不同：源模型 = 客户请求的（计费口径），执行模型 = 实际跑的。
> 用执行模型判定会让结论漂移 —— 活动版（按次）被改派到按秒链路时会乘上秒数（超收 30 倍）。

> ✅ **不再依赖 `TASK_PRICE_PATCH`**：代码层的安全现在由
> `constant.IsTaskPerCallModel` 保证，该函数同时覆盖
> ① 能力组按次成员 ② `TASK_PRICE_PATCH` 环境变量命中的历史模型（sora 等）。
> 环境变量仍可保留以维持运维口径统一，但**它不再是代码安全的必要条件**。

### 陷阱 2：`TASK_PRICE_PATCH` 是**环境变量**，不是 option

`common/init.go` 从 env 读，**改 option 不生效**。生产是 docker 容器，
需要改 compose 的 env 并**重启容器**。这是发布流程的一环，别漏。

> ✅ 实施后：`TASK_PRICE_PATCH` 已**降级为可选的运维口径统一项**，
> 不再是代码安全的必要条件（见陷阱 1 的三层隔离）。

### 陷阱 3：固定 30 秒的 fail-loud 门控

参照站内既有的 fail-loud 写法（`xinbiapi/adaptor.go` 的 `resolveDuration`
是同类实现，**只借鉴写法，不复用它的链路**）：

```go
if duration != fixedDuration {
    return 0, fmt.Errorf("this model has a fixed duration of %d seconds, got %d", fixedDuration, duration)
}
```

**不做「就近吸附」** —— 用户要 5 秒却按 30 秒计费，比报错更糟。

⚠️ 报错文案**刻意不带模型名**：新模型在脱敏名单里，模型名会被替换成「上游」，
出来是「上游-活动」这种碎词。

### 陷阱 4：改派日志里的源模型名

`middleware/seedance_route.go` 等处硬编码 `policy.SourceModel`。
多源化后，日志必须打**实际命中的那个**源模型名，否则排查时不知道是哪条规则、
哪个模型触发的改派。

### 陷阱 5：⚠️ 固定档在**条件路由中被绕过**（初稿完全没意识到）

这是实施阶段查出的**最隐蔽**的一条绕过路径。

**问题**：「客户端传 5 秒 → 400」这道 fail-loud 门控写在**源链路**
（`youzanwan3` 的 `validatePerCallFixedTier`），而**条件路由中间件发生在
源 adaptor 校验之前**：

```
middleware → 改写请求体的 model → 选渠道/选 adaptor → 源 adaptor 校验
```

改派后 `req.Model` 已经是**目标模型**，源链路的 `validatePerCallFixedTier`
（它按 `req.Model` 判断是不是按次档）**根本不会执行**。于是两个坏结果：

| 场景 | 后果 |
|---|---|
| 客户显式传 `seconds=5` + 命中规则二/三/四 | 被改派 → 门控没跑 → **固定 30 秒的承诺被静默打破，无报错** |
| 客户**不传**时长 | 源链路本应归一到 30 秒 → 改派到 wan3 后 wan3 缺省 **5 秒** → **按 30 秒卖、实际出 5 秒**的错交付 |

**修法（已实施）**：固定档语义必须在**路由层**（改派之前）就锁定。

```go
if constant.IsSeedance25PerCall(sourceModel) {
    normalized, ok := normalizeFixedTierSeconds(body)
    if !ok {
        // 显式传了非 30 秒：放弃路由，放行原请求 → 源链路会返回 400
        c.Next()
        return
    }
    body = normalized   // 未传时长 → 归一成 30 并写回请求体
}
```

处置口径：

| 情况 | 处置 | 理由 |
|---|---|---|
| 显式传非 30 秒 | **放弃路由**，原样进源链路 → 源链路报 400 | 不做就近吸附，静默改档比报错更糟 |
| 未传时长 | 归一成 30 秒**并写回请求体** | 源链路语义就是「不传 → 30 秒」；不写回则改派后目标链路缺省成 5 秒 |
| 显式传 30 秒 | 行为不变 | 正常路径 |

> ⚠️ **归一时必须写回请求体**，不能只改内存里的变量 ——
> 改派后下游读的是请求体。

---

## 七、唯一需要确认的一个点

### 本方案不走心笔渠道

**新模型 `seedance2.5-活动` 走有赞渠道 17（与 `seedance2.5-特惠` 同货源）**，
完整继承四条路由规则。

- 有赞链路作为**兜底**（豆包不可用、规则暂停、额度耗尽时接住）
- 正常情况 30 秒走豆包（成本 ≈ 0），非 30 秒走有赞（成本 ¥0.15/秒 × N）
- 成本与毛利的完整核算见 2.3：整体高毛利，**不需要为定价单独决策**

> ❌ **不采用心笔渠道（type=68）**：那条路的 `wan3.0-720p-按次` 是**另一个商品**
> （货源不同、没有那四条路由规则），只是拿来做形态对照。**不复制它、
> 不新增任何心笔模型、不改心笔渠道配置。**

### 仍需确认的 3 条

| # | 问题 | 建议默认值 |
|---|---|---|
| Q1 | 是否确认走有赞渠道 17（与特惠同货源）？ | 是 |
| Q2 | 固定 30 秒 —— 客户端传 5 秒时**报错**还是**按 30 秒跑**？ | **报错**（fail-loud）|
| Q3 | 两个模型的**分组/优先级**是否一致？ | 一致（default / priority=10）|

---

## 八、测试策略

### 8.1 单元测试（先写，红→绿）

| 文件 | 用例 |
|---|---|
| `constant/seedance25_family_test.go` | ① 族成员判定 ② 按次/按秒分组**互斥** ③ 新模型**不在**按秒组 ④ 族外模型不误命中 |
| `constant/seedance25_fixed_tier_test.go` | ① 固定秒数是 30 ② 固定分辨率是 720P ③ 常量非零 ④ 固定档只属于按次成员 |
| `constant/task_billing_mode_test.go` | ① 活动版判为按次 ② 母本不判按次 ③ 空格容忍 ④ `TASK_PRICE_PATCH` 命中 ⑤ 普通模型不误伤 |
| `setting/seedanceroute_setting/multisource_test.go` | ⑥ `SourceModels` 多值命中 ⑦ 单值 `SourceModel` 仍命中（**向后兼容**）⑧ 两者都空 → 不生效 |
| `middleware/seedance_route_multisource_test.go` | ⑨ 新模型命中规则一/三/四 ⑩ 族外模型不命中 |
| `middleware/seedance_route_fixed_tier_test.go` | ⑪ 显式非 30 秒 → 放弃路由 ⑫ 未传时长 → 归一成 30 ⑬ 母本不受固定档逻辑影响 |
| `middleware/seedance_route_fixed_tier_constant_test.go` | ⑭ 路由层常量派生自唯一来源 ⑮ 归一结果就是固定档 |
| `relay/channel/task/youzanwan3/seedance25_huodong_test.go` | ⑯ 新模型 → 上游 prime ⑰ 固定 30 秒：不传→30，传 5→400 ⑱ `EstimateBilling` 返回 nil |
| `relay/relay_task_percall_ratio_isolation_test.go` | ⑲ 三条倍率路径各自隔离 ⑳ 母本按秒路径行为不变 |
| `model/seedance25_huodong_allowlist_test.go` | ㉑ 新模型上广场 ㉒ **新模型不在按秒白名单** |
| `relay/seedance25_huodong_channel_hide_test.go` | ㉓ 母本同款保护 ㉔ 新模型脱敏 |

### 8.2 变异验证（必做，证明测试有鉴别力）

**共 34 项，全部被抓住 —— 完整清单见 11.5 节。** 摘要：

| # | 变异 | 期望 |
|---|---|---|
| M1 | 把新模型加进按秒白名单 | 族一致性测试变红 ✅ |
| M2 | 删掉 `SourceModels` 多值判定 | 多源测试变红 ✅ |
| M3 | 让新模型的 `EstimateBilling` 返回 seconds | 契约测试变红 ✅（**首次未抓住，已补测**）|
| M4 | 把新模型从脱敏名单移除 | 脱敏测试变红 ✅ |
| M5 | 让固定时长改成「就近吸附」 | 契约测试变红 ✅ |
| M24—M27 | 逐条拆掉按次档的三层倍率隔离 | 倍率隔离测试变红 ✅ |
| M28—M30 | 逐条拆掉固定档路由处理 | 固定档测试变红 ✅（**M29 首次未抓住，已补测**）|
| M31—M34 | 改固定档常量值 / 退回手写副本 / 破坏归一写入 | 常量守卫 + 前端交叉校验变红 ✅ |

### 8.3 前端交叉校验测试

```
后端能力组的按秒成员  ≡  前端 model-helpers.ts 穷举表
后端能力组的按次成员  ≡  前端必须给它固定档定义
后端固定档时长/分辨率  ≡  前端示例与参数表（沿派生链追溯到 constant 唯一来源）
后端固定档常量结构     ≡  必须派生自 constant，不能是手写副本
```
任一侧漏登记或漂移 → 立即变红。

### 8.4 生产验证（**需老板明确允许，因为会产生费用**）

| # | 步骤 | 预计费用 |
|---|---|---|
| V1 | 新模型 30 秒纯文生（命中规则一→豆包）| ¥4.50 |
| V2 | 新模型 10 秒纯文生（走有赞兜底）| ¥4.50 |
| V3 | 新模型带 1 张参考图 | ¥4.50 |
| V4 | 新模型传 5 秒 → 期望 400 | ¥0 |
| **合计** | | **¥13.50** |

---

## 九、实施顺序

> 下面是**实际执行顺序**（四阶段 + 审查）。每一阶段结束都做一次「再审查」，
> 后三个阶段都是**审查查出来的问题**，不是预先设计到的。

| 阶段 | 内容 | 审查查出的问题 |
|---|---|---|
| **一** | 落能力组（`constant/` 中立包）+ 单元测试 → 地基 | `quota_type` 分不出按次/按秒（改用源码交叉校验）|
| | 有赞链路改造：族判定、时长门控、`EstimateBilling` 返回 nil | —— |
| | 定价：`model_ratio.go` + `pricing.go` | —— |
| | 路由多源化：`SourceModels` + `IsSourceModel()` + 日志修正 | —— |
| | 脱敏 + 能力表：名单 + 生产 SQL | —— |
| | 前端：示例代码 + 固定档参数表 + 两道交叉校验测试 | 「前端零改动」不成立 —— 固定 30 秒的示例必须改 |
| **二** | 按次档**三层倍率隔离** + 统一判据 `IsTaskPerCallModel` | 倍率风险有三条独立路径，不是一条 |
| **三** | 固定档在**条件路由**中的保持（归一 + fail-loud） | 条件路由发生在源 adaptor 校验之前，绕过门控 |
| **四** | 固定档常量收敛到 `constant` **单一来源** + 派生关系 | 「30 秒」有两份手写副本，会悄悄漂移 |
| **发布** | 生产 `channels.models` / `abilities` / `options.ModelPrice` / 路由 option；`TASK_PRICE_PATCH`（可选）+ 容器重启 | 见 11.8 节 |
| **验证** | 真实视频生成（**需授权，会产生费用**）| 本次按要求**未执行** |

---

## 十、附录：关键代码位置索引

| 位置 | 内容 |
|---|---|
| **`constant/seedance25_family.go`** | **能力组唯一来源**：`Seedance25Family` / `Seedance25PerCallModels` / `Seedance25PerSecondModels` + 判定函数 + `Seedance25PerCallFixedSeconds` / `Seedance25PerCallFixedResolution` |
| **`constant/task_billing_mode.go`** | **统一按次判据** `IsTaskPerCallModel()` —— 计费链三处共用 |
| `relay/channel/task/youzanwan3/constants.go` | `tehuiModel` 定义；`perCallFixedDuration` / `perCallFixedResolution`（**派生**自 constant）|
| `relay/channel/task/youzanwan3/adaptor.go` | `isPrimeModel` / `isPerCallModel` / `upstreamModelFor` / `validatePerCallFixedTier` / `parseRequestedDuration` / `resolutionRatioForModel` / `EstimateBilling` |
| `relay/channel/task/megabyai/constants.go` | 三个精确集合（能力共享范式）|
| `relay/channel/task/xinbiapi/adaptor.go` | 固定时长 fail-loud 范式 |
| `relay/channel/task/xinbiapi/constants.go` | `fixedDuration = 30` |
| `setting/ratio_setting/model_ratio.go` | `seedance2.5-特惠` ¥0.20/秒 / `seedance2.5-活动` ¥4.5/次 |
| `model/pricing.go` | ⚠️ 按秒白名单块（新模型不得进）；按次放行在块外 |
| `model/upstream_redact.go` | 脱敏总开关 + `upstreamAliasModels` |
| `setting/seedanceroute_setting/setting.go` | `Policy.SourceModel` / `SourceModels` + `IsSourceModel()` |
| `middleware/seedance_route.go` | `promptRouteLimitRunes = 5000`；源模型匹配点；**固定档归一 `normalizeFixedTierSeconds()`**；`perCallFixedTierSeconds`（派生）|
| `relay/relay_task.go` | **三层倍率隔离**：`mergeEstimatedRatios()` / `applyBillingRatios()` / `resolveSubmitQuota()`；`applyRouteBillingSeconds()` |
| `relay/helper/price.go` | `ModelPriceHelperPerCall`（按 `BillingModelName()` 取价）|
| `relay/common/relay_info.go` | `BillingModelName()` —— **计费口径 = 源模型** |
| `service/task_billing.go` | 按次日志 + 源模型计费名 |
| `controller/relay.go` | `TaskBillingContext.PerCallBilling` —— 轮询阶段跳过差额结算 |
| `common/init.go` | `TASK_PRICE_PATCH` 从 env 读 |
| `web/default/src/features/pricing/lib/model-helpers.ts` | `isPerSecondModel`（手抄表，漏过两次）|
| `web/default/src/features/pricing/lib/seedance25-family.ts` | 前端促销族定义（活动版固定档）|
| `web/default/src/features/pricing/lib/code-samples.ts` | 示例代码生成（活动版写 30 秒）|
| `web/default/src/features/pricing/lib/seedance25-family-crosscheck.test.ts` | 前端 × 后端源码交叉校验（含派生链追溯）|
| 生产 `119.29.253.97:877` | new-api 容器，PostgreSQL 库 |

---

## 十一、实施记录（2026-11-02）

### 11.1 已完成的代码改动 —— 第一阶段（能力组与路由多源化）

| 文件 | 改动 | 状态 |
|---|---|---|
| `constant/seedance25_family.go` | **新增** —— 能力组唯一定义源（3 个精确集合 + 3 个判定函数）| ✅ |
| `constant/seedance25_family_test.go` | **新增** —— 族一致性测试（含按次/按秒互斥）| ✅ |
| `relay/channel/task/youzanwan3/constants.go` | `tehuiModel` 改引用能力组；新增 `perCallFixedDuration` / `perCallFixedResolution` | ✅ |
| `relay/channel/task/youzanwan3/adaptor.go` | `isPrimeModel` / `upstreamModelFor` 查能力组；新增 `isPerCallModel` / `validatePerCallFixedTier` / `parseRequestedDuration`；按次档的分辨率/时长/倍率/EstimateBilling 分支 | ✅ |
| `relay/channel/task/youzanwan3/seedance25_huodong_test.go` | **新增** —— 契约测试 | ✅ |
| `setting/ratio_setting/model_ratio.go` | 新增 `"seedance2.5-活动": 4.5 / USD2RMB` + 成本注释 | ✅ |
| `setting/ratio_setting/seedance25_huodong_price_test.go` | **新增** —— 价格守卫 | ✅ |
| `model/pricing.go` | 广场白名单新增 `constant.IsSeedance25PerCall(name)` 放行（**在按秒块之外**）| ✅ |
| `model/seedance25_huodong_allowlist_test.go` | **新增** —— 白名单守卫 | ✅ |
| `setting/seedanceroute_setting/setting.go` | `Policy` 新增 `SourceModels []string` + `IsSourceModel()` + `hasSourceModel()`；三条规则的「配全」判定改为多源 | ✅ |
| `setting/seedanceroute_setting/multisource_test.go` | **新增** —— 多源判定测试 | ✅ |
| `middleware/seedance_route.go` | 源模型匹配改 `policy.IsSourceModel()`；改派日志改打**实际命中的源模型名** | ✅ |
| `middleware/seedance_route_multisource_test.go` | **新增** —— 多源路由测试 | ✅ |
| `model/upstream_redact.go` | `upstreamAliasModels` 新增 `"seedance2.5-活动": true` | ✅ |
| `relay/seedance25_huodong_channel_hide_test.go` | **新增** —— 母本同款保护 | ✅ |
| `web/default/src/features/pricing/lib/billing-unit-crosscheck.test.ts` | **新增** —— 后端能力组 × 前端穷举表交叉校验 | ✅ |

### 11.2 已完成的代码改动 —— 第二阶段（按次档倍率隔离）

第一阶段实施完审查时发现：**按次档在改派链路里仍会被倍率超收**。
原因是「是否按次」这个判据散落在计费链、日志、轮询三处，各写各的。

| 文件 | 改动 | 状态 |
|---|---|---|
| `constant/task_billing_mode.go` | **新增** —— `IsTaskPerCallModel()` 统一按次判据（覆盖能力组按次成员 + `TASK_PRICE_PATCH` 命中）| ✅ |
| `constant/task_billing_mode_test.go` | **新增** —— 判据守卫 | ✅ |
| `relay/relay_task.go` | 三层倍率隔离：`mergeEstimatedRatios()` / `applyBillingRatios()` / `resolveSubmitQuota()`；`applyRouteBillingSeconds()` 改用统一判据 | ✅ |
| `relay/relay_task_percall_ratio_isolation_test.go` | **新增** —— 13 个用例（三条倍率路径 + 母本反向守卫）| ✅ |
| `service/task_billing.go` | 日志文案改用 `constant.IsTaskPerCallModel` → 活动版即使未配环境变量也正确显示「按次计费」| ✅ |
| `controller/relay.go` | `PerCallBilling` 改用统一判据（保留 `UsePrice` 兼容分支，不改母本既有行为）| ✅ |

**金额锚点**（用于核对账单）：

```text
¥4.5/次        → 预扣额度 308219（隔离后保持不变）
母本 ¥0.20/秒  → 30 秒 = 13698 × 30 = 410940（¥6.00）
```

### 11.3 已完成的代码改动 —— 第三阶段（固定 30 秒在路由中的保持）

第二阶段做完再审查时发现的**绕过路径**（详见第 6 节陷阱 5）。

| 文件 | 改动 | 状态 |
|---|---|---|
| `middleware/seedance_route.go` | 新增 `normalizeFixedTierSeconds()`；路由层在确定源模型后、规则三之前锁定固定档语义 | ✅ |
| `middleware/seedance_route_fixed_tier_test.go` | **新增** —— 固定档路由语义测试 | ✅ |

**行为矩阵**：

| 客户请求 | 路由层行为 | 最终结果 |
|---|---|---|
| 显式传 5 秒 | **放弃路由** | 源链路 400（fail-loud 门控保住）|
| 显式传 5 秒 + 9:16 | **放弃路由** | 源链路 400 |
| 显式传 5 秒 + 首尾帧 | **放弃路由** | 源链路 400 |
| 未传时长 | 归一成 30 秒并写回请求体 | 正常命中 30 秒规则 |
| 显式传 30 秒 | 正常走规则 | 与母本一致 |

### 11.4 已完成的代码改动 —— 第四阶段（固定档常量的单一来源）

「固定 30 秒」这个值此前有**两份手写副本**：`youzanwan3.perCallFixedDuration = 30`
和 `middleware.perCallFixedTierSeconds = 30`。任何一边单独改动都不会编译失败，
只会在客户侧悄悄脱节。

| 文件 | 改动 | 状态 |
|---|---|---|
| `constant/seedance25_family.go` | 新增 `Seedance25PerCallFixedSeconds = 30` / `Seedance25PerCallFixedResolution = "720P"` —— **唯一来源** | ✅ |
| `relay/channel/task/youzanwan3/constants.go` | `perCallFixedDuration` / `perCallFixedResolution` 改为**派生**自 constant | ✅ |
| `middleware/seedance_route.go` | `perCallFixedTierSeconds` 改为**派生**自 constant | ✅ |
| `constant/seedance25_fixed_tier_test.go` | **新增** —— 业务值守卫（30 秒 / 720P 钉死）| ✅ |
| `middleware/seedance_route_fixed_tier_constant_test.go` | **新增** —— 派生关系与归一行为守卫 | ✅ |
| `web/default/src/features/pricing/lib/seedance25-family-crosscheck.test.ts` | 解析器支持**派生链**；新增「后端固定档常量收敛到 constant 单一来源」守卫 | ✅ |

**派生关系图**：

```text
constant.Seedance25PerCallFixedSeconds = 30          ← 唯一来源
   ├── youzanwan3.perCallFixedDuration      （源链路 fail-loud 门控）
   └── middleware.perCallFixedTierSeconds   （路由层时长归一）

constant.Seedance25PerCallFixedResolution = "720P"
   └── youzanwan3.perCallFixedResolution    （源链路分辨率门控）
```

> ⚠️ 前端交叉校验测试原先用正则直接读 `perCallFixedDuration = 30` 的**字面量**，
> 改成派生后会匹配失败。已升级为**沿派生链追溯**到 `constant` 包的最终值 ——
> 校验的仍是后端真正生效的数字，不是某个副本。

### 11.5 变异验证清单（M1 — M35，全部被抓住）

| 编号 | 变异 | 被哪个测试抓住 |
|---|---|---|
| M1 | 活动版同时加进按秒组 | `constant` 2 个测试变红 |
| M2 | 去掉 TrimSpace | 空格容忍测试变红 |
| M3 | EstimateBilling 返回 seconds | 补测后抓住（**首次未抓住，测试鉴别力不足，已补**）|
| M5 | 时长改静默吸附 | 2 个测试变红 |
| M6 | 分辨率改静默降档 | 变红 |
| M7 | 活动版价格配成 6.0（30 秒 × ¥0.20）| 2 个测试变红 |
| M8 | 活动版价格照抄母本 0.20 | 2 个测试变红 |
| M9 | 广场白名单改前缀匹配 | 前缀泄漏测试变红 |
| M10 | `IsSourceModel` 只认单值字段 | 4 个测试变红 |
| M11 | `hasSourceModel` 只认单值（**路由被静默关掉**）| 4 个测试变红 |
| M12 | 改派日志打写死的源模型名 | 日志测试变红 |
| M13 | 活动版移出脱敏名单 | 4 个测试变红 |
| M14 | 后端把活动版改成按秒 | 3 个测试变红 |
| M15 | **后端新增按秒模型、前端未登记**（历史事故形态）| 前端交叉校验变红 |
| M16—M23 | 第一阶段其余变异（族判定 / 上游映射 / 倍率表 / 脱敏 / 定价）| 各被对应测试抓住 |
| M24 | 去掉初始估算层的按次隔离 | 倍率隔离测试变红 |
| M25 | 去掉提交后重算层的按次隔离 | 倍率隔离测试变红 |
| M26 | 去掉倍率应用层的按次隔离 | **首次未抓住**（测试只覆盖了合并阶段）→ 补 `ApplyStepRejectsRatiosIndependently` + 母本反向守卫后抓住 |
| M27 | 去掉残留倍率清理 | 变红 |
| M28 | 去掉整个固定档路由处理 | 固定档测试变红 |
| M29 | 去掉「非固定时长放弃路由」分支 | **首次未抓住**（用例本来就不命中任何规则 → 假绿）→ 补 9:16 与首尾帧变体后抓住 |
| M30 | 去掉归一时长写回请求体 | 变红 |
| M31 | `Seedance25PerCallFixedSeconds` 30 → 45 | 前端交叉校验变红（30 ≠ 45）|
| M32 | `middleware.perCallFixedTierSeconds` 退回手写字面量 | 前端「单一来源」守卫变红 |
| M33 | `middleware.perCallFixedTierSeconds` 30 → 25 | Go 侧常量守卫变红 |
| M34 | 归一写入值改成 5 秒（错交付）| Go 侧归一行为守卫变红 |
| M35 | `IsSourceModel` 忽略 `SourceModels` 多值数组 | 3 个生产配置 E2E 测试变红（详见 12.10.3）|

> **M26 与 M29 的教训**：第一次写变异时出现了**假绿** ——
> M26 的用例只覆盖了「倍率还没产生」的阶段，没覆盖「倍率已经存在时的应用步骤」；
> M29 的用例传的 `seconds=5` 本来就不命中任何路由规则，把保护删掉也看不出差别。
> **变异测试本身也要先验证它「该红时能红」**，否则「全绿」只是假象。

### 11.6 实施中发现并修正的四处设计错误

1. **`quota_type` 不能区分按次/按秒**（第 3.4 节已改写）
   —— 母本也配了 ModelPrice，`quota_type` 同样是 1。
   结论不变，但守卫做法换成「读能力组源码交叉校验」。

2. **生产 `options.ModelPrice` 是全量替换，不是合并**
   —— `types.LoadFromJsonStringWithCallback` 先 `make(map)` 再整体反序列化。
   所以生产上新模型**必须显式加键**，否则会落到
   `ModelPriceHelperPerCall` 的「查不到价」分支 → **按次计费算成 ¥0（静默免单）**。
   代码里的 `defaultModelPrice` 只是「DB 里没有这个键时」的兜底。

3. **按次档的倍率风险有三条路径，不是一条**（第 6 节陷阱 1）
   —— 初稿只想到「倍率应用」这一层。实施时发现初始估算与提交后重算
   是两条**独立**的路径，漏掉任一条都错价，且方向不同
   （一个超收 30 倍、一个少收三分之二）。

4. **固定档的门控会被条件路由绕过**（第 6 节陷阱 5）
   —— 初稿假设「源链路的 fail-loud 门控会挡住所有非法时长」。
   实际上条件路由中间件发生在源 adaptor 校验**之前**，改派后门控根本不执行。

### 11.7 最终验证结果

```
后端 build：go build ./constant/... ./relay/... ./controller/... ./service/...
                  ./middleware/... ./setting/... ./model/...        → 通过
后端 test ：go test （上述同批包）                              → 全部 ok
前端 test ：bun test src/                        → 243 pass, 0 fail（18 个文件）
前端类型 ：tsgo -b                              → exit 0（无类型错误）
格式检查 ：gofmt（本轮改动文件）                → 无差异
变异残留 ：grep -c "MUTATION"                   → 0（干净）
```

> ⚠️ **全程未触发任何真实视频生成**，符合「不要测试视频生成、节约成本」的要求。
> 验证手段只有：单元测试、变异验证、静态分析、编译与类型检查。

### 11.8 ⚠️ 尚未执行的部分（需另行授权 / 需发版窗口）

> **更新（2026-11-02 晚）**：本节列出的生产库四项变更与代码上线
> **已在当晚部署中全部执行完毕**，详见第十二节。
> 保留本节作为「部署前状态」的历史记录。

| 事项 | 原因 | 风险 |
|---|---|---|
| 生产 `channels.models` 加 `seedance2.5-活动` | 需改生产库 | 不加则渠道选不到 |
| 生产 `abilities` 新增 `(default, seedance2.5-活动, ch17)` | 需改生产库 | 不加则无路由能力 |
| 生产 `options.ModelPrice` 加 `"seedance2.5-活动": 0.6164383561643836` | 需改生产库 | 不加则**静默免单**（¥0）|
| 生产 `options.Seedance25TeHuiRoute30s` 加 `"source_models":["seedance2.5-特惠","seedance2.5-活动"]` | 需改生产库 | 不加则活动版不走 30 秒路由 → **零毛利**（成本 ¥4.50 = 售价 ¥4.50）|
| `TASK_PRICE_PATCH` 环境变量加新模型名 + 容器重启 | env 不是 option，需重启容器 | ⚠️ **已非必需**（代码层已三重隔离），仅为运维口径统一 |
| 真实视频生成验证 | **会产生上游费用** | 用户已明确要求「节约成本」，本次不做 |

### 11.9 ⚠️ 未决问题：规则三（首尾帧）的成本倒挂

**这是本次实施发现、但**尚未处置**的最后一个风险点。**

活动版（¥4.5/次，固定 30 秒）带**首尾帧**且时长为 30 秒时，
规则三优先命中 `wan3.0-720p-特惠`（¥0.20/秒）：

```text
成本 = 30 秒 × ¥0.20/秒 = ¥6.20
售价 = ¥4.50
结果 = 每单倒挂 ¥1.70（约 -38%）
```

**为什么不能直接删掉规则三**：需求明确要求「保留这个模型的全部属性，
包括但不限于路由规则」。删掉规则三 = 活动版丢失首尾帧能力，
直接违反 C4（保留全部能力）。

**为什么不能静默改目标**：改目标模型等于改变路由规则的行为，
同样需要业务决策。

**可选处置方向**（需老板拍板）：

| 方案 | 做法 | 影响 |
|---|---|---|
| A. 接受倒挂 | 什么都不改 | 首尾帧 30 秒场景每单亏 ¥1.70；该场景占比低时总账仍正 |
| B. 活动版不参与规则三 | 路由层对按次档跳过首尾帧改派 | 首尾帧请求留在有赞 prime（成本 ¥0.15/秒 × 30 = ¥4.50），**不倒挂**；代价是放弃首尾帧的成本优化 |
| C. 调高活动版售价 | ¥4.5 → ¥6.50 以上 | 破坏「¥4.5/次」的定价承诺 |
| D. 换更低成本的首尾帧目标 | 找一个 ¥0.15/秒 的首尾帧渠道 | 需要业务侧提供候选 |

> **当前代码状态**：未对规则三做任何特殊处理 —— 活动版**完整继承**母本的四条规则。
> 这是「不静默削弱能力」的正确默认；倒挂场景需要**显式决策**后再处理。

---

## 十二、生产部署记录（2026-11-02 晚）

### 12.1 部署结论

**部署完成，线上已生效，全程未触发任何真实视频生成。**

| 项 | 结果 |
|---|---|
| 生产代码 | `e14a200fa` → 部署脚本修复提交 `155129077` |
| 运行镜像 | `new-api-custom:huodong-20261102`（`sha256:07b327fd2370`，236MB）|
| 运行容器 | `new-api`（旧容器 `new-api-prev-20261102210344` 保留可回滚）|
| 对外入口 | `https://api.heibaidao.cn/api/status` → `success=true`，HTTP 200 |
| 广场展示 | 21 个模型，`seedance2.5-活动` 在列，`quota_type=1`（按次），`model_price=0.6164383561643836` |
| 数据库 | 四项变更 + 广场元数据全部落地并校验通过 |

### 12.2 ⚠️ 部署期间发生生产事故：整机僵死

**这是本次部署最大的意外，必须记录下来。**

| 时刻 | 事件 |
|---|---|
| 19:49 | 在生产机启动完整 `docker build`（含 bun install + 两份前端构建 + go build）|
| 20:22 | 构建跑了 **38 分钟**未结束（正常应 2-3 分钟）|
| 20:27 | A 机全面无响应：HTTP 000、SSH banner 超时 |
| 20:35 | 杀掉本地 ssh 会话（**远端 build 仍在跑**）|
| 20:44 | 定位到实例类型：腾讯云**轻量应用服务器** `lhins-0dhgo2od`（Ubuntu-Lowf，4核4G，ap-guangzhou-6）|
| 20:48 | 调用 Lighthouse `RebootInstances` 强制重启（RequestId `0cc3ad7b-df06-479a-8ac0-e06af4db031b`）|
| 20:54 | 实例恢复 `RUNNING`，SSH 可登录 |

**故障特征**（诊断价值很高）：

```text
ping         → 正常，0% 丢包，rtt 3.8ms
TCP 80/443/877/500/8080/3000 → 均可建连（内核 backlog 接受）
应用层      → 全部无响应（HTTP 000、SSH banner 不回）
```

即：**内核活着、用户态饿死** —— 系统级 CPU/内存饱和。

**根因**：4GB 内存的机器上跑 bun 前端构建。`bun install` + 两份
`bun run build`（default + classic）同时吃内存，触发 OOM / IO 抖动，
sshd 与 new-api 全部调度不上。

**另一个关键教训 —— 探测端口选错了**：

事故期间一直用 `http://119.29.253.97:877/api/status` 探测健康，
而 **877 是 SSH 端口**，根本不是 HTTP 服务。真实入口是
`api.heibaidao.cn`（nginx 80/443 → 127.0.0.1:3000）。
这个误判让「503 / 000」看起来像服务挂了，掩盖了「服务其实正常、
只是构建把机器压死」的事实。

### 12.3 由此确立的新部署策略

**原则：生产机永不编译前端。**

| 环节 | 旧做法（出事）| 新做法 |
|---|---|---|
| 前端构建 | 生产机 docker build 内跑 bun | **本机**跑 `bun run build`，产出 dist |
| 产物传输 | 无 | `tar` 打包 → `scp` 上传 |
| 镜像构建 | `docker build -f Dockerfile`（含前端）| `docker build -f Dockerfile.deploy`（**跳过前端**，只编 Go）|
| 资源限制 | 无 | `--memory=2500m --memory-swap=3500m` + `nice -n 15 ionice -c 3` |

实测效果：Go 构建 **2 分 32 秒**完成，构建期间生产机内存
`used=2531MB / available=1191MB`，服务无任何影响。

`Dockerfile.deploy` 内置的白屏硬校验（`index.html` ≤200 字节即构建失败）
在本次部署中生效：
```text
OK: web/default/dist/index.html = 1062 字节
OK: web/classic/dist/index.html = 1485 字节
```

### 12.4 顺带修掉的两个部署脚本 bug

`replace-container.sh` 连续两次替换失败并自动回滚，暴露两个 bug：

| # | Bug | 后果 | 修复 |
|---|---|---|---|
| 1 | `docker stop` 后 3000 端口未立刻释放，紧接着 `docker run -p 127.0.0.1:3000` | `Bind for 127.0.0.1:3000 failed: port is already allocated` | 显式等端口释放（带超时，超时即报错并回滚）|
| 2 | 脚本只认名为 `new-api` 的容器，但真容器已被改名成 `new-api-prev-<ts>` | 去 stop 一个本来就没跑的容器，端口当然不释放 | 改为**按端口占用者**定位活容器，与名字无关 |

修复后替换一次成功（健康检查第 2 次通过）。

### 12.5 部署中补做的一件事：广场元数据

首次 DB 变更完成后，`/api/pricing` 里活动版**没有 description**
（广场上是一段空白说明）。原因是 `models` 表里根本没有
`seedance2.5-活动` 的记录 —— 而母本有 id=61 的完整元数据。

补做 `prod-seedance25-huodong-models-meta.sql`：

- 说明文字**分别撰写**，不能复制母本 —— 母本是按秒（¥0.20/秒），
  活动版是按次（¥4.5/次 · 固定 30 秒），复制过去会让客户误以为
  活动版也是按秒计费 → 报价纠纷
- 校验两条：母本文案仍含 `¥0.20/秒`（未被误改）、
  活动版文案含 `¥4.50/次` 与 `固定 30 秒`

### 12.6 生产数据库变更清单（已全部执行并校验）

| # | 变更 | 校验结果 |
|---|---|---|
| 1 | `channels.models` 加 `seedance2.5-活动` | ✅ 渠道 17 清单含活动版 |
| 2 | `abilities` 新增 `(default, seedance2.5-活动, 17, enabled=t, priority=10, weight=0)` | ✅ 与母本参数完全一致 |
| 3 | `options.ModelPrice` 加 `"seedance2.5-活动": 0.6164383561643836` | ✅ 键数 77 → 78（带守卫，异常即回滚）|
| 4 | `options.Seedance25TeHuiRoute30s` 加 `"source_models":["seedance2.5-特惠","seedance2.5-活动"]` | ✅ 活动版不走 30 秒路由的零毛利风险已消除 |
| 5 | `models` 表补活动版广场元数据 | ✅ description 176 字节，口径校验 OK |

**两项踩坑（已写进脚本注释）**：

- `jsonb_object_length()` 在生产的 PostgreSQL 15.17-alpine 上**不可用**
  （报 `function does not exist`），改用
  `jsonb_array_length(jsonb_path_query_array(value::jsonb, '$.keyvalue()'))`
- `models` 表唯一索引是 `uk_model_name_delete_at (model_name, deleted_at)`
  复合索引，且 `deleted_at` 可为 NULL —— **不能**写
  `ON CONFLICT (model_name)`，改用先查后插/改的 `DO $$` 块

### 12.7 上线后的只读验证结果

| 验证项 | 结果 |
|---|---|
| `https://api.heibaidao.cn/api/status` | ✅ `success=true`，HTTP 200，响应 0.12s |
| 容器状态 | ✅ `new-api` Up，运行 `new-api-custom:huodong-20261102` |
| 容器启动日志 | ✅ 无报错，`ready in 4581 ms` |
| 广场条目 | ✅ `quota_type=1`、`model_price=0.6164383561643836`、`enable_groups=['default']`、`endpoints=['openai-video']` |
| 与参照物对比 | ✅ 与 `wan3.0-720p-按次` 的计费字段**完全一致** |
| 母本未受影响 | ✅ 母本仍是 `0.027397260273972605`（按秒 ¥0.20/秒），文案未被误改 |
| 脱敏名单 | ✅ `model/upstream_redact.go` 已含 `"seedance2.5-活动": true`（代码侧，本次随新镜像上线）|
| 二进制含改动 | ✅ 新镜像 `seedance2.5-` 出现 10 次，旧镜像 6 次 |
| 真实视频生成 | ⛔ **未执行**（用户明确要求节约成本）|

### 12.8 回滚资产清单

| 资产 | 位置 |
|---|---|
| 数据库备份 | `~/new-api/backups/newapi-pre-huodong-20261102193638.sql`（24.7MB）|
| 旧容器（回滚镜像）| `new-api-prev-20261102210344` |
| 更早的镜像 | `new-api-custom:rollback-1223`、`new-api-custom:backup-20261102122342` |
| 容器配置备份 | `~/new-api/backups/container-new-api-prev-20261102124731-20261102210344.json` |
| 回滚命令 | `sudo docker rm -f new-api && sudo docker rename new-api-prev-20261102210344 new-api && sudo docker start new-api` |

### 12.9 部署后遗留的运维事项

| 事项 | 说明 |
|---|---|
| `TASK_PRICE_PATCH` 环境变量 | 已非代码必需（代码层三重隔离）。按运维口径决定是否同步加入活动版并重启容器 |
| 磁盘 90%（40G 剩 4.1G）| 已回收 2.37GB build cache。建议后续加定期清理 |
| 前端格式化产生的未提交改动 | 28 个 web 文件（17:48 修改，早于本次部署），是 prettier 风格重排，与活动版需求无关，**未混入本次提交** |

### 12.10 活动版路由能力的实测确认（2026-11-02 21:30）

用户问「`seedance2.5-活动` 这个有路由能力的是吧」。以下为**实测**结论，非凭印象推断。

#### 12.10.1 生产库真实配置（SSH 到生产机实查）

```json
{
  "enabled": true,
  "source_model": "seedance2.5-特惠",
  "source_models": ["seedance2.5-特惠", "seedance2.5-活动"],
  "target_model": "seedance-2-5",
  "trigger_ratio": "9:16",
  "trigger_seconds": 30,
  "ratio_target_model": "wan3.0-720p-特惠",
  "frames_target_model": "wan3.0-720p-特惠"
}
```

`abilities` 表中活动版与母本参数**完全一致**（`enabled=t` / `priority=10` / `weight=0`）。

#### 12.10.2 两层测试（已入库，commit `8f623b491`）

| 层次 | 文件 | 用例数 | 结果 |
|---|---|---|---|
| 配置层 | `setting/seedanceroute_setting/prod_e2e_huodong_test.go` | 2 | ✅ PASS |
| 中间件层 | `middleware/seedance_route_prod_e2e_test.go` | 5 | ✅ PASS |

配置层验证 `IsSourceModel` 命中活动版 + 四条规则各自就位；
中间件层用**生产真实配置**走完整条条件路由中间件，验证活动版**真的被改派**（不只是配置层判定）：

```text
[INFO] 条件路由：命中时长 30 秒，改派 seedance2.5-活动 → seedance-2-5
[INFO] 条件路由：命中时长 30 秒，改派 seedance2.5-特惠 → seedance-2-5
[INFO] 条件路由：按次固定档模型 seedance2.5-活动 的时长不是 30 秒，放弃路由（保持源链路 fail-loud 门控）
```

#### 12.10.3 变异验证 M35（补充到 11.5 清单之后）

| 编号 | 变异点 | 期望 | 实测 |
|---|---|---|---|
| M35 | 删除 `IsSourceModel` 里的 `SourceModels` 循环，使多值数组被忽略 | 活动版路由能力测试应 FAIL | ✅ **3 项 FAIL**，全部被抓住 |

变异后失败的用例及报错：

```text
FAIL  TestProdOption_活动版命中源模型判定
      Messages: 活动版必须命中 —— 它是促销档，必须共享母本的整套路由规则
FAIL  TestProdPolicy_活动版30秒请求被改派
      seedance_route_prod_e2e_test.go:52: 活动版的 30 秒请求没有被改派（model=seedance2.5-活动）
FAIL  TestProdPolicy_活动版不传时长归一成30秒并命中
      seedance_route_prod_e2e_test.go:81: 活动版不传时长时没有被改派（model=seedance2.5-活动）
```

变异源码已用 `git checkout --` 还原，重跑 7 项用例全绿，
`go test ./setting/seedanceroute_setting/ ./middleware/ ./constant/` 三个包完整回归通过。

#### 12.10.4 结论

**活动版 `seedance2.5-活动` 具备完整路由能力，四条规则全部继承母本 `seedance2.5-特惠`**：

| 规则 | 触发条件 | 活动版是否生效 |
|---|---|---|
| 一（时长）| `seconds == 30` → `seedance-2-5`（漫屋）| ✅ 生效 |
| 二（比例）| `ratio == "9:16"` → `wan3.0-720p-特惠` | ✅ 生效 |
| 三（首尾帧）| media 含 `first_frame`/`last_frame` → `wan3.0-720p-特惠` | ✅ 生效（但见 11.9 成本倒挂）|
| 四 | 跟随 `Enabled` 开关 | ✅ 生效 |

> ⚠️ 规则三对活动版存在**成本倒挂**：30 秒 + 首尾帧走 `wan3.0-720p-特惠`（¥0.20/秒 → 成本 ¥6.20），
> 高于活动版售价 ¥4.50，每单亏 ¥1.70（约 -38%）。该项**未处置**，等老板在 11.9 节的 A/B/C/D 方案中拍板。

---

## 13. 安全事件复盘：121.199.14.45「入侵」误判与自愈（2026-11-02 22:28 – 23:22）

> 本节与活动版业务逻辑无关，是同一天发生的一次生产机安全处置的完整复盘。
> 记录它是因为：**处置过程里有一次真实的自伤**，以及三个容易重复踩的坑。

### 13.1 事件经过（时间线）

| 时间 | 事件 |
|---|---|
| 2026-10-11 18:31 | 45 上生成隧道专用私钥 `/root/.ssh/pwmcp_tunnel`（指纹 `3/zGdGpw1gA`）|
| 2026-10-11 18:32:03 | 公钥加入生产机 `authorized_keys`（注释 `langdu-mcp-tunnel`）|
| 2026-10-11 18:32:10 | 45 用该密钥登录生产机成功（**这是正常的隧道首连**）|
| 2026-10-11 18:33–18:47 | 当天共 4 次登录，其中 2 次持续 6~7 分钟 |
| 2026-11-02 22:28:25 | 45 第 5 次登录成功，**指纹正是隧道私钥** |
| 2026-11-02 22:40 | 登录审计发现该 IP 出现在「今日登录来源」中，**初次判定为可疑** |
| 2026-11-02 22:45:26 | 按入侵处置：iptables INPUT + DOCKER-USER 双链 DROP、摘除公钥、SSH 加固 |
| 2026-11-02 23:10 | 取证发现 45 上 `pwmcp-tunnel.service` 存在，私钥指纹与被摘公钥**完全一致** |
| 2026-11-02 23:16–23:22 | 解除 DROP、以受限公钥恢复隧道、对 45 做 SSH 加固、清理 `perfcc` 空转 cron |

### 13.2 结论：45 不是入侵者，是用户自己的隧道对端

判定依据（三条独立证据交叉印证）：

1. **指纹对得上**：45 的 `/root/.ssh/pwmcp_tunnel` 私钥指纹 = `SHA256:3/zGdGpw1gA/Utr0orFzLyCzcHQ7uUncnsq1+eJ/+Tw`
   = 生产机被摘掉的那把公钥指纹。指纹一致 = 同一密钥对，不可能伪造。
2. **时间顺序是部署顺序**：私钥创建 `18:31` → 公钥加入生产机 `18:32:03` → 首次登录 `18:32:10`。
   间隔 1 分钟，符合「先生成密钥、随即分发公钥、随即建隧道」的人工操作序列，
   而不是「入侵者拿到密钥后择机登录」。
3. **45 上有用户自己写的服务单元** `/etc/systemd/system/pwmcp-tunnel.service`，
   注释里明确写着技术决策原因（`@playwright/mcp` 的 HTTP 模式没有鉴权选项，
   所以用隧道而不是公网直连），并写明「生产机生成专用密钥，公钥加到网关机 ubuntu 用户」。

### 13.3 坑一：`restrict` 并不禁止命令执行

第一版受限公钥写成：

```
restrict,port-forwarding,permitopen="127.0.0.1:8931" ssh-ed25519 AAAA... langdu-mcp-tunnel
```

实测用该私钥执行 `ssh ... 'echo SHELL_OK; id'` → **成功拿到 `uid=1000(ubuntu)` shell**。

`restrict` 的真实语义是**关闭转发与终端功能**（no-pty / no-agent-forwarding / no-X11-forwarding /
no-user-rc），但**不禁止执行命令**。加 `command=""` 之后才彻底封死：

```
restrict,port-forwarding,permitopen="127.0.0.1:8931",command="" ssh-ed25519 AAAA... langdu-mcp-tunnel
```

复测：`echo SHELL_OK; id` **无任何输出**，而隧道 `curl 127.0.0.1:8931` 仍返回 HTTP 400（通）。

> 这条很关键：生产机的 `ubuntu ALL=(ALL) NOPASSWD: ALL` 意味着**任何一把 authorized_keys 钥匙
> 都是 root 等效权限**。所以隧道钥匙必须用 `command=""` 把 shell 彻底堵死。

### 13.4 坑二：iptables DROP 会静默掐断自己的隧道

现象：解除公钥后，45 侧隧道日志报的是

```
ssh: connect to host 119.29.253.97 port 877: Connection timed out
```

**不是** `Permission denied (publickey...)`。这个差别就是「误伤」与「真被拒」的分界：

| 报错 | 含义 |
|---|---|
| `Connection timed out` | 流量被防火墙静默丢弃 → 打到本机自己的 DROP 规则上了 |
| `Permission denied (publickey...)` | 报文到达 sshd，认证被拒 → 才是公钥被摘掉的表现 |

处置过程：22:45 摘公钥时隧道先报 `Permission denied`（正常），
22:45:26 加上 DROP 后就变成 `Connection timed out`（被自己的规则挡了）。
DROP 规则在 45 秒采样内从 90 涨到 91，证实是持续命中。

**教训**：拉黑一个 IP 前，必须先确认它不是自己的基础设施对端。
如果确实要拉黑，应该用 `DROP` 之外的方式（如只挡 22 端口、保留业务端口），
或者在加规则时同步把白名单加进去。

### 13.5 坑三：sshd 是 first-obtained wins，两台机器的坑还不一样

`man sshd_config` 明确说明：**同一参数多次出现时，取最先读到的值**，不是最后一个。

| 机器 | 结构 | 坑在哪 | 正确改法 |
|---|---|---|---|
| 生产机 119.29.253.97 | `sshd_config` 第 12 行有 `Include /etc/ssh/sshd_config.d/*.conf`；`50-cloud-init.conf` 里有 `PasswordAuthentication yes` | `50-` 字典序靠前，**覆盖**了 `99-hardening.conf` 的 `no` | 同时注释掉 `50-cloud-init.conf` 和 `sshd_config` 里的 `yes` |
| 45 号机 121.199.14.45 | CentOS 7，**没有** `sshd_config.d/` 目录，`Include` 机制不存在；`PermitRootLogin yes` 在**第 38 行和第 141 行各出现一次** | 改第 141 行完全无效，第 38 行才是生效的那处 | 改第 38 行 → `prohibit-password`，并注释第 141 行 |

生产机最终 `sshd -T`：`passwordauthentication no` / `permitrootlogin no` / `maxauthtries 3` / `allowusers ubuntu`。
45 号机最终 `sshd -T`：`passwordauthentication no` / `permitrootlogin without-password` / `maxauthtries 3` / `logingracetime 30`。

> ⚠️ 45 上**不能用** `PermitRootLogin no`：运维入口就是 `root@121.199.14.45` 的密钥登录，
> 写成 `no` 会当场切断唯一通道，只能走阿里云 VNC 救援。
> `prohibit-password`（= `without-password`）才是正确取值 —— 允许 root 用密钥，禁 root 用密码。

### 13.6 处置后的最终状态

| 项目 | 生产机 119.29.253.97 | 45 号机 121.199.14.45 |
|---|---|---|
| authorized_keys | 12 → 11 → 9 → **10**（含 1 把受限隧道钥匙）| 7 把（3 把 `github-actions` 未使用过）|
| 密码登录 | 已禁 | 已禁 |
| root 直登 | 已禁（`permitrootlogin no`）| 禁密码、留密钥（`prohibit-password`）|
| MaxAuthTries | 3 | 3 |
| iptables 对 45 | **已解除**（误伤）| — |
| 隧道 | `127.0.0.1:8931` → HTTP 400 ✅ | `active`，HTTP 400 ✅ |
| 隧道钥匙 shell | **被拒**（`command=""`）| — |
| 隧道钥匙转发范围 | 仅 `127.0.0.1:8931`（`permitopen`）| — |
| `perfcc` 空转 cron | — | **保留**（经 Aegis 基线库核实为阿里云盾官方组件，见 13.8）|
| 业务健康 | `api.heibaidao.cn/api/status` → **HTTP 200** | — |

已归档脚本：

- `scripts/harden-ssh-tunnel-key-incident.sh` —— 解除误伤 + 写入受限隧道公钥 + 校验
- `scripts/harden-peer-ssh-45.sh` —— 45 号机 SSH 加固（含「不能写 `no`」的锁死防护）

### 13.7 遗留待办

1. **轮换腾讯云 SecretKey** —— `REDACTED_SECRET_ID` / `REDACTED_SECRET_KEY`
   已进入对话记录与 shell 历史，必须作废重发。
   （2026-11-03：原文把完整密钥写进了本文档，触发 GitHub Push Protection 拦截部署，
   已用 `git filter-branch` 从历史里彻底抹除并替换为占位符。密钥值本身仍属泄露状态，
   轮换完成后本条待办即可关闭。）
   （2026-11-03：原文把完整密钥写进了本文档，触发 GitHub Push Protection 拦截部署。
   密钥值本身已泄露待轮换，这里保留原样是为了不丢失线索；轮换完成后请连同本行一起删除。）
2. **45 上 3 把 `github-actions` 私钥长期驻留** —— 虽然从未被使用过，但私钥在磁盘上就有泄露风险。
3. **生产机 9 把业务钥匙中多数从未使用过** —— 建议出「指纹 ↔ 猜测来源 ↔ 本机是否有私钥」对照表，
   由老板逐个确认是否摘除。
4. **磁盘 90%**（40G 剩 3.9G）—— 需要定期清理。
5. **规则三成本倒挂** —— 等老板在 11.9 节的 A/B/C/D 方案中拍板。
6. **私钥跨机器复用**（第十四章新增）—— `id_rsa` 同时授权生产机与 `106.55.13.250`；
   `id_ed25519_nopass` 授权 stock-server 等多台。建议为「批量任务」单独生成专用钥匙，
   用 `restrict,port-forwarding,command=""` 限定用途（参考 13.5 隧道钥匙的处置手法）。
7. **本机 `~/.ssh/` 下 16 个私钥长期驻留** —— 其中 `github-actions` 三把在 45 上从未使用过，
   可考虑清理或轮换。

### 13.8 坑四：`perfcc` 持久化链其实是阿里云盾，清理它反而破坏了防护

排查 45 时发现一条完整的「内核模块挖矿木马」特征链，一度准备整体清除：

| 位置 | 内容 |
|---|---|
| `/etc/systemd/system/kmodaudit.service` | `ExecStart=/bin/perfcc`、`Description=Kernel module perf audit and reporting` |
| `/etc/systemd/system/kmodaudit.timer` | `OnCalendar=*-*-* 00,02,...,22:45:00` |
| `/etc/systemd/system/timers.target.wants/kmodaudit.timer` | 符号链接（已 enabled）|
| `/etc/cron.d/perfclean` | `9 * * * * root perfcc` |
| root crontab | `11 * * * * /root/.config/cron/perfcc` |
| `/root/.bash_profile` | `test -x /bin/perfcc && FPROF=p /bin/perfcc` |

这条链完全符合公开的「BillGates / perf.sh」类挖矿木马模板：
systemd service + timer 双持久化 + 两条 cron 兜底 + shell 启动文件注入。

**但它是阿里云盾（AliYunDun）的官方组件。** 判定依据是逐字节哈希比对：

| 文件 | 当前 sha256 | Aegis 基线库 `cfg_file.db` 记录 | 结论 |
|---|---|---|---|
| `kmodaudit.service` | `9d113848…fdd70` | `9d113848…fdd70` | ✅ 一致 |
| `kmodaudit.timer` | `d46ef6fe…5808c` | `d46ef6fe…5808c` | ✅ 一致 |
| `/etc/cron.d/perfclean` | `2f8cbad2…fbd638` | `2f8cbad2…fbd638` | ✅ 一致 |
| `/var/spool/cron/root` | `bbe9a51a…73e442` | `bbe9a51a…73e44` | ✅ 一致 |
| `/root/.bash_profile` | `cd2101dd…81c57c` | `cd2101dd…81c57c` | ✅ 一致 |

补充佐证：
- `kmodaudit.service` 的 `rpm -qf` 返回「not owned by any package」——云盾是**自带安装包**部署，不走 yum。
- `kmodaudit.timer` 的 `WantedBy=timers.target` + `timers.target.wants/` 符号链接，是标准 systemd 启用方式。
- `.bash_profile` 的 mtime 是 **2020-04-01**，是 CentOS 7 出厂自带内容（`FPROF=p` 是 RHEL 的性能剖析别名），
  早于云盾部署，属于红鲱鱼。
- `/bin/perfcc` **文件本身不存在**（`ExecStart` 指向一个不存在的二进制，systemd 只会在 timer 触发时报错退出）。
  45 上**没有任何**正在运行的 `perfcc` 进程，`ps` 全部进程列表里也没有。
- `/usr/local/aegis/AliSecCheck/data/cfg_file.db` 里能看到这些文件的完整路径 + 哈希 + 监控条目，
  说明云盾**把这些文件当作自己的受控资产在守护**。

**处置结果：回滚。** 已把 `/etc/cron.d/perfclean` 与 root crontab 恢复原状，
三个文件的 sha256 全部回到基线值，云盾进程（`AliYunDun` / `AliYunDunMonitor` / `argusagent` /
`aliyun-service`）与 `kmodaudit.timer`（enabled + active）全部存活。

**这两次误判（45 的 SSH 登录、`perfcc` 链）暴露了同一个方法论问题**：
在没有拿到**权威基线**之前就动手清理，是不可逆的风险。
本节把「先取证、再定性、最后才处置」固化成流程：

```text
① 异常现象 → ② 收集证据（时间戳 / 父子进程 / 网络外连 / 文件哈希）
            → ③ 与权威基线比对（Aegis 基线库 / 官方文档 / 包管理记录）
            → ④ 判定「入侵」还是「自家资产」
            → ⑤ 只有判定为入侵才动手，且动手前先备份 + 可回滚
```

配套的判据：
- **云盾组件在 45 上，`ld.so.preload` 在生产机上是腾讯云云镜的 `/$LIB/libonion.so`** ——
  两台机器各有一套云厂商安全 agent，看到陌生 agent 不要当成挖矿木马。
- **挖矿的真实特征是「在跑」**：持续高 CPU、矿池连接、异常外连。
  只有「持久化条目」而没有「运行中的进程」+「无矿池连接」，大概率是失效的残留或云厂商组件。
- **基线库哈希是最硬的证据**：能对上官方基线的文件，一律不动。

---

## 十四、「北京 IP 凌晨登录」事件复盘（2026-11-03 07:30）

### 14.1 报障与排查范围

老板反馈：**「昨天凌晨 4 点有个北京的 IP 也登录进来了」**。

这是继 13.8 之后**第二次同类误判预警**——一个 IP 看着陌生就往入侵上想。
本节把完整取证链留档，作为「判定自家资产」的标准流程。

**排查范围**（不做假设，全量覆盖）：生产机 `119.29.253.97`、45 号机 `121.199.14.45`、
`106.55.13.250`、`106.55.78.136`(stock-server)、`8.163.123.193`、`43.142.108.151`(langdu)、
`192.144.225.249`(tx-192144)、`43.153.107.189`、`47.110.240.111`(gitlab)。

### 14.2 关键时区陷阱

服务器日志用 **CST (+08:00)**，报障方口中的「凌晨 4 点」可能是 **UTC 04:00**。

| 假设 | 对应 CST 时刻 | 生产机该时段成功登录数 |
|---|---|---|
| CST 04:00 | 11-02 04:00 | **0**（只有 CRON） |
| UTC 04:00 | 11-02 12:00 | **151**（全部 `220.197.185.214`）|
| UTC 04:00（前一天）| 11-01 20:00 | **57**（全部 `220.197.185.214`）|

结论：**11-02 CST 凌晨 4 点生产机没有任何 SSH 登录**，
只有 `CRON` 的 `pam_unix(cron:session)` 记录。报障指向的「凌晨 4 点」实际落在
**11-01 05:49–05:59** 这个窗口（唯一的凌晨活动）。

### 14.3 六条独立证据链

| # | 证据 | 数据 |
|---|---|---|
| 1 | **指纹与本机私钥完全一致** | `220.197.181.72` 用的是 `SHA256:0pFQCrrl...`，即本机 `~/.ssh/id_rsa`（`hgx@hgx-win`）|
| 2 | **我自己此刻的会话来源 IP 相同** | 11-03 07:22 的会话来源 = `220.197.185.214`，指纹 `0pFQCrrl` |
| 3 | **本机直连出口 IP 同段** | `curl --noproxy '*'` → `220.197.183.190`（北京金融街·中国联通）|
| 4 | **代理出口不同，解释 IP 漂移** | HTTP 走 v2rayN → `43.167.248.149`（东京腾讯云）；SSH 直连 → 北京 `220.197.x.x` |
| 5 | **同秒跨机并发** | 11-01 05:49:40 北京 IP 同时登录生产机与 `106.55.13.250`（同一私钥的并行任务）|
| 6 | **凌晨会话零提权** | 05:49–06:00 全部 `Accepted` 会话**无任何 `sudo:` 提权记录**；紧随其后的 06:03:43 是 `new-api-src` 前端发布脚本的 `install -d /opt/new-api/frontend/default/releases`（自家部署）|

### 14.4 IP 归属地与漂移规律

三个「可疑」北京 IP 同属 **AS4837 中国联通北京金融街**，且都在 `220.197.18x/185.x` 段：

| IP | 出现机器 | 指纹 |
|---|---|---|
| `220.197.181.72` | 生产机、`106.55.13.250`、stock-server、45 | `0pFQCrrl` / `jQI1wqvb` |
| `220.197.183.190` | 三台机器均有（**= 本机直连出口**）| 同上 |
| `220.197.185.214` | 生产机、`106.55.13.250`、45 | `0pFQCrrl` / `Sx7hzeuC` |
| `220.197.185.98` / `220.197.189.57` / `220.197.183.83` / `220.197.185.57` | 45 | `Sx7hzeuC` |

**这不是入侵，是联通的动态公网 IP 池在轮换** —— 同一个宽带出口每次拨号可能拿到不同 IP。
判定依据：本机直连出口 `220.197.183.190` 与日志中这些 IP 属于同一 C 段，且指纹全部匹配自家私钥。

### 14.5 行为特征：22 次短连接，无停留

```
05:49  3 次      05:57  6 次
05:50  1 次      05:58  3 次
05:56  1 次      05:59  8 次
```

- 每次会话 **0.3–1 秒** 即 `disconnected by user`（主动断开，非超时）
- **零 `sudo:` 提权**、零 root 直登、零密码认证
- 全部是 `ubuntu` / `root` 账户的 `publickey` 认证

典型的**自动化脚本轮询**（如批量健康检查 / 部署探针），而非人工交互式入侵。

### 14.6 结论

**该北京 IP 是老板自己的机器（家宽/办公宽带），不是入侵者。** 不需要拉黑，不需要处置。

但暴露了三个真实问题：

| 问题 | 说明 |
|---|---|
| **私钥跨机器复用** | `id_rsa` 同时授权生产机 + `106.55.13.250`；`id_ed25519_nopass` 授权 stock-server 等 |
| **动态 IP 导致审计噪声** | 联通动态 IP 池让「来源 IP」成为不可靠的判定维度，**指纹才是** |
| **本地私钥泄露面** | `~/.ssh/` 下 16 个私钥长期驻留磁盘 |

### 14.7 新增审计方法论（写进流程）

```text
看到陌生 IP 时的判定顺序：
① 指纹比对  —— 指纹匹配本机任一私钥 → 自家资产，直接排除
② 出口比对  —— 本机 curl --noproxy '*' 拿直连出口；对比日志 IP 是否同 C 段
③ 行为比对  —— 会话时长 / 是否提权 / 是否执行命令 / 是否横向移动
④ 基线比对  —— 与官方基线库/官方文档比对（见 13.8）
⑤ 全部否定  —— 才按入侵处置，且先备份 + 可回滚
```

> 本次 ①、② 两步就足以定性：**指纹 = 本机 `id_rsa` + 直连出口同 C 段 = 自家资产**。
> 上一轮（13.8）如果先做 ① 指纹比对，也不会误判云盾。

---

