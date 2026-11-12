# 设计：seedance2.5-特惠 · 30 秒条件路由到 seedance-2-5

> 状态：**设计稿（未实施）**
> 日期：2026-10-30
> 需求方：老板（口头描述）——「用户调用 `seedance2.5-特惠`，**只有时长 = 30 秒**时路由到
> `seedance-2-5`，且要有个开关控制」

---

## 一、为什么要做（动机）

今天 23 条 `seedance2.5-特惠` 任务里，30 秒的占 12 条（52%）。而两个模型的**计费口径差异巨大**：

| | seedance2.5-特惠（有赞） | seedance-2-5（漫屋→豆包官网） |
|---|---|---|
| 30 秒售价 | ¥0.20 × 30 = **¥6.00** | 按次 **¥3.00** |
| 我们的成本 | 30 × ¥0.15 = **¥4.50** | 自建浏览器自动化 ≈ ¥0 |
| 毛利 | ¥1.50（25%） | ¥3.00（~100%） |

客户只要时长填 30 秒，同一个商品就能省一半多。**但必须由客户显式要 30 秒**才能走这条 ——
因为豆包官网只有 5/10/15/30 四档，且**不支持参考视频和音频**（漫屋 adaptor 里 `audio` 零结果）。

---

## 二、两个模型的硬性差异（决定了不能只改模型名）

### 2.1 渠道与上游

| 维度 | seedance2.5-特惠 | seedance-2-5 |
|---|---|---|
| 渠道 | 17 有赞 Wan3（type=63） | 20 漫屋 ArcReel（type=66） |
| 上游 | 有赞 API `wan3.0-video-prime` | 豆包官网**浏览器自动化** `doubao-seedance-2-5-260628` |
| 计费 | 按秒，`EstimateBilling` 返 `{"seconds":30,"size":1}` | 按次，`EstimateBilling` 返 **nil** |
| 时长 | 2–30 秒**逐秒** | **仅 5/10/15/30 四档** |
| 参考图 | ≤10 | ≤10 ✅ 兼容 |
| 参考视频 | ≤5 | **不支持** |
| 参考音频 | ≤5 | **不支持** |
| 比例 | `adaptive` + 6 档 | 七档，自适应档叫 **`auto`**（不是 `adaptive`） |
| 参考图入参 | `metadata.media` = `[{type,url}]` | `input_reference` / `images` / `reference_image` 等**顶层键** |

### 2.2 ⚠️ 三个必须处理的转换点

**① 参考图格式不同 —— 只改模型名会静默丢图**

漫屋 `clientRequest` 的参考图白名单（`relay/channel/task/manwu/adaptor.go:1461`）只认：

```
input_reference / images / image / image_url / image_urls /
reference_image / reference_images / input_images
+ 各自的 camelCase 变体
```

**不含 `metadata.media`**。有赞格式的图会被整个丢弃 → 建单不带图 →
官网回「参考资源 图 1 当前不存在」，额度照烧。这正是 **2026-10-22 丢图事故**的同型问题，
漫屋那边已经踩过并做了大量别名兼容。

**② 比例自适应档叫法不同**

有赞用 `adaptive`，豆包用 `auto`。转 `adaptive` 时要映射成 `auto`（或干脆不传，
豆包「未指定不编造」会保持官网当前档位）。

**③ 参考视频 / 音频无法迁移**

30 秒 + 带参考视频/音频的请求，豆包链路**根本不支持**。必须**明确拒绝**，
不能静默丢掉素材（否则客户以为带上了）。

---

## 三、实现方案

### 3.1 总体思路：在 Distribute 之前改写请求体

渠道是在 `middleware.Distribute()` 里由 `getModelRequest(c)` 读 body 的 `model` 字段选定的
（`middleware/distributor.go:36`）。`controller/relay.go:294` 的 `getChannel` 只是兜底读
`c.GetInt("channel_id")`。

所以：**新增一个前置中间件**，在 `Distribute` 之前：
1. 读 body（`common.GetBodyStorage(c)`，body 可重复读）
2. 判断是否命中「模型 = seedance2.5-特惠 且 时长 = 30」
3. 命中则**改写 body**：`model` 换成 `seedance-2-5`、参考图转成漫屋格式、比例映射
4. 用 `common.CreateBodyStorage(newBytes)` 替换 context 里的 storage + `c.Request.Body`

改写完成后，**后续链路全自动正确**：
- `Distribute` 按新模型名 `seedance-2-5` 选渠道 → 选到漫屋（channel 20）
- `GetTaskPlatform` 从 `channel_type=66` 推出 manwu adaptor
- `ModelPriceHelperPerCall` 按 `seedance-2-5` 取价 → ¥3.0/次
- `EstimateBilling` 走漫屋分支 → 按次计费

**不需要改任何 adaptor**，也不需要动 `abilities` 表。

### 3.2 时长判定口径

必须覆盖客户可能的所有写法（`TaskSubmitReq` 的自定义 UnmarshalJSON 兼容多种形态）：

| 来源字段 | 说明 |
|---|---|
| `duration` | 数字或字符串（`"30"`） |
| `seconds` | 数字或字符串 |
| `metadata.duration` / `metadata.seconds` | 部分客户端塞 metadata |

判定逻辑：**任一解析出 30 即命中**（与有赞 adaptor 的 `resolveDurationForModel` 同口径：
`duration` 优先，缺省回退 `seconds`，都没有则默认 5）。

⚠️ 注意：`defaultDuration = 5`，所以「不传时长」→ 5 秒 → **不命中**，正确。

### 3.3 开关设计

新建 `setting/seedanceroute_setting/`（照抄 `setting/youzanwan3_setting` 的模式）：

```go
const OptionKey = "Seedance25TeHuiRoute30s"

type Policy struct {
    // Enabled 总开关。false = 完全不路由，全部走有赞（默认，行为不变）。
    Enabled bool `json:"enabled"`
    // SourceModel 源模型名（对外名）。
    SourceModel string `json:"source_model"`
    // TargetModel 目标模型名（对外名）。
    TargetModel string `json:"target_model"`
    // TriggerSeconds 触发时长（秒）。只有等于该值才路由。
    TriggerSeconds int `json:"trigger_seconds"`
    // UpdatedAt 最近一次修改时间。
    UpdatedAt int64 `json:"updated_at"`
}
```

**默认值必须「关」** —— `defaultPolicy()` 里 `Enabled: false`，
保证「没有这个 option 时行为与改造前完全一致」。

配置放 `options` 表，管理员可在后台改，或用 SQL 直接改：

```sql
UPDATE options SET value = '{"enabled":true,"source_model":"seedance2.5-特惠",
  "target_model":"seedance-2-5","trigger_seconds":30}'
WHERE key = 'Seedance25TeHuiRoute30s';
```

#### 3.3.0.1 可选比例条件（2026-11-12 补充）

规则一除了时长阈值，还可以再挂一道**比例闸门**：只有请求里指定了
指定比例（如 9:16）时才改派。

```go
// SecondsRuleRatioGate 规则一的比例闸门（如 "9:16"）。空 = 不限比例。
SecondsRuleRatioGate string `json:"seconds_rule_ratio_gate,omitempty"`
```

判定位置在**时长阈值之后**（`middleware/seedance_route.go` 规则一分支）：

```
源模型匹配 && 时长 > 阈值 && 比例闸门放行  →  改派 target_model
```

闸门放行规则（`secondsRatioGatePasses`）：

| 闸门值 | 请求里的比例 | 是否改派 |
|---|---|---|
| 空 | 任意 | ✅ 改派（2026-11-12 之前的老配置没有这个字段，行为必须不变）|
| "9:16" | 9:16 | ✅ 改派 |
| "9:16" | 16:9 | ❌ 留源链路 |
| "9:16" | 没写比例 | ❌ 留源链路 |

「没写比例也不放行」是刻意的：客户没要求 9:16，就不该替他改派到
只对 9:16 效果好的链路上。比例的读取顺序与源链路一致 ——
`metadata.ratio` / `metadata.aspect_ratio` → 顶层同名字段 → 从 `size`
（`720x1280` 这类）推断。

**后台勾选（本次新增）**：闸门此前只能靠 SQL 改 options 表，运营看不到
也改不动。现在渠道抽屉的「豆包链路」卡片里多一个独立勾选：

- 勾上 → 闸门 = 9:16（只有 9:16 且超过阈值才改派）
- 取消 → 清空闸门，恢复「任何比例超阈值都改派」

接口 `POST /api/video-route/policy/ratio-gate`（管理端登录态），
写入走 `setting.WithSecondsRuleRatioGate`（纯函数，只改闸门与
`UpdatedAt` 两个字段）。

> ⚠️ 这个勾选与「豆包链路开关」是两个独立控件：前者决定**哪些请求有资格**
> 改派，后者决定**这条路通不通**（关闭会停规则一并强制故障转移，
> 到北京时间凌晨 2 点自动恢复）。两者互不覆盖。

### 3.3.0.2 目标模型的常驻渠道（2026-11-12 变更史）

`seedance-2-5` 是规则一的**目标模型**，它本身挂在哪个渠道上，决定了改派
之后的请求实际落到哪条链路。当天来回迁过两次：

| 时段 | 常驻渠道 | 上游 | 状态 |
|---|---|---|---|
| 迁移前 | ch20 漫屋ArcReel（type 66） | 豆包官网 `https://arcreel.heibaidao.cn` | 原始状态 |
| 21:00 前 | ch27 视频生成车站（type 69） | `https://yuanmengstudio.com` | 「全部走班车」 |
| 21:50 后 | **ch20 漫屋ArcReel（type 66）** | 豆包官网 | **当前状态**（「恢复回去 改为豆包的」）|

**当前结论：`seedance-2-5` 常驻 ch20（豆包官网）**，与本文档 2.1 节
「上游 = 豆包官网浏览器自动化」的原始设计一致。

变更只动两处（`channels.models` 与 `abilities` 必须成对改），**不动**
路由配置（`options.Seedance25TeHuiRoute30s` 的 `target_model` 始终是
`seedance-2-5`）与 `ModelPrice`。台账脚本：

- `scripts/prod-seedance25-banche-20261112.sql`（迁班车，历史台账，勿重跑）
- `scripts/prod-seedance25-restore-doubao-20261112.sql`（回迁豆包，当前状态）

**回迁带来的一个行为变化**：故障转移（规则五）的源是 `seedance-2-5`、
备胎是 `wan3.0-班车`。迁班车期间两者同在 ch27，
`failoverSharedChannelChain` 会显式放弃改写（改名无路由收益）；
回迁后源在 ch20、备胎在 ch27，**规则五重新变成一条真正可用的降级链**：

```
促销族 → seedance-2-5（豆包 ch20）→ 备胎 wan3.0-班车（车站 ch27）
```

### 3.3.0.3 代码上线记录（2026-11-12 22:22～22:36）

回迁只改了数据库，**代码侧还有一批改动在同一批次上线**（双机同 SHA
`fad7255e2`）：

| 提交 | 内容 |
|---|---|
| `fd395f3cd` / `b0196cfc6` | 豆包链路开关的**可选比例条件**勾选（后台可改，闸门值只存一份）|
| `45c3108ed` / `6d2dc88a7` | 比例默认值改由后端下发（`seconds_rule_ratio_gate_default`），前端不再硬编码 9:16 |
| `dc1436173` | **规则六**：短任务（≤10 秒）分流到有赞标准版 `wan3.0-video`；含 failover 共享渠道判定、`GetChannelIDsForModel`、提示词字节预算 |
| `fad7255e2` | 修正 `dc1436173` 带来的过时注释（按回迁后的豆包 ch20 写）|

上线后实测（生产日志）：

```text
条件路由：命中短时长 2 秒（≤ 10 秒），改派 seedance2.5-特惠 → wan3.0-video
条件路由：命中短时长 10 秒（≤ 10 秒），改派 seedance2.5-特惠 → wan3.0-video
```

**回迁后新代码与旧注释的一个语义差**：`Wan3PromptCompress` 现在统一按
**字节**预算（20000 字节）判断是否压缩，而豆包官网的上限是 20000 **字符**。
对当前常驻的豆包链路来说这个预算更严格（约 6667 字到顶而非 20000 字）——
方向是偏保守的：宁可多压一次（压到 ≤5000 字符必然安全），也不放行注定
被上游 400 的提示词。

### 3.3.1 素材门槛：带参考视频/音频 → **不路由**（老板 2026-10-30 拍板）

豆包链路**只支持参考图**（漫屋 adaptor 里 `audio` 零结果，参考视频也只有反解场景用）。
所以带参考视频/音频的请求**一律不路由**，走有赞原路径 —— **不是报错**。

理由：
- 客户不被打断（报错会让 30 秒 + 素材的正常需求直接失败）；
- 语义也更准确：「**能省的才省**」——素材形态不适合豆包时就老老实实走有赞；
- 避免静默丢素材（如果路由过去但把视频/音频丢了，客户以为带上了，实际没生效）。

**判定素材类型**：有赞格式的素材在 `metadata.media` = `[{type,url}]`，
`type` 取值 `reference_image` / `reference_video` / `reference_audio`
（也兼容 `first_frame` / `last_frame` 这类历史类型名 —— 它们本质是图片）。
**只要出现 `reference_video` 或 `reference_audio`，就不路由。**

⚠️ 注意：判定的输入是**原始请求体**（改写前），不是改写后的目标格式。

### 3.3.2 其它「不路由」的正常情形（不算异常，不记警告）

| 情形 | 处理 |
|---|---|
| 模型名不是 `seedance2.5-特惠` | 不路由 |
| 时长 ≠ 30 秒（含不传 → 默认 5 秒）| 不路由 |
| 带参考视频 / 参考音频 | 不路由（走有赞）|
| 请求不是 JSON（multipart 表单）| 不路由 |
| body 解析失败 | 不路由（交回原链路，**不挡客户**）|
| 开关关闭 | 不路由 |

### 3.4 中间件伪代码

```go
// middleware/seedance_route.go
func Seedance25TeHuiRoute30s() func(c *gin.Context) {
    return func(c *gin.Context) {
        policy := seedanceroute_setting.GetPolicy()
        if !policy.Enabled {
            c.Next()
            return
        }
        // 只处理 JSON 提交（非 multipart）
        if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
            c.Next()
            return
        }
        storage, err := common.GetBodyStorage(c)
        if err != nil { c.Next(); return }   // 读不动就交回原链路，不挡客户
        raw, err := storage.Bytes()
        if err != nil { c.Next(); return }

        var body map[string]any
        if err := common.Unmarshal(raw, &body); err != nil { c.Next(); return }
        model, _ := body["model"].(string)
        if model != policy.SourceModel {
            c.Next()
            return
        }

        seconds, ok := extractSeconds(body)
        if !ok || seconds != policy.TriggerSeconds {
            c.Next()
            return
        }

        // 素材门槛：带参考视频/音频 → **不路由**（走有赞原路径），不是报错。
        if hasUnsupportedMedia(body) {
            c.Next()
            return
        }

        newBody, ok := rewriteToTarget(body, policy)   // 见 3.5
        if !ok {
            // 转换失败 → 不路由，交回原链路，绝不挡客户
            c.Next()
            return
        }
        newRaw, err := common.Marshal(newBody)
        if err != nil { c.Next(); return }
        newStorage, err := common.CreateBodyStorage(newRaw)
        if err != nil { c.Next(); return }
        c.Set(common.KeyBodyStorage, newStorage)
        c.Request.Body = io.NopCloser(newStorage)
        c.Request.ContentLength = int64(len(newRaw))
        c.Next()
    }
}
```

**注册位置**（在 `router/video-router.go` 的 `videoV1Router` 上，**必须早于 `Distribute()`**）：

```go
videoV1Router.Use(middleware.TokenAuth(), middleware.Seedance25TeHuiRoute30s(), middleware.Distribute())
```

### 3.5 请求体转换（`rewriteToTarget`）

```go
func rewriteToTarget(body map[string]any, policy Policy) (map[string]any, bool) {
    out := map[string]any{}
    for k, v := range body {
        if k == "model" || k == "metadata" { continue }
        out[k] = v
    }
    out["model"] = policy.TargetModel   // "seedance-2-5"

    // 1. 参考图：metadata.media[{type,url}] → input_reference:[url]
    //    只取 type=reference_image 的（first_frame/last_frame 也算图片）。
    //    ⚠️ 转出来的图数必须与原图数一致 —— 不一致说明解析口径有问题，
    //    直接放弃路由（返回 false），**绝不能带着少掉的图去建单**。
    // 2. 时长：归一到 "seconds":"30"（豆包只认 5/10/15/30）
    out["seconds"] = "30"
    // 3. 比例：metadata.ratio → 顶层 ratio，且 adaptive → auto
    return out, true
}
```

---

## 四、边界与风险清单

| # | 风险 | 处理 |
|---|---|---|
| 1 | **静默丢图**（`metadata.media` 不在漫屋白名单） | 必须做格式转换 + 转换后**断言图数一致**，不一致就**放弃路由**（走有赞）|
| 2 | 参考视频/音频豆包不支持 | **不路由**，走有赞原路径（老板 2026-10-30 拍板，**不是报错**）|
| 3 | 比例 `adaptive` → 豆包不认 | 映射为 `auto` 或不传 |
| 4 | 时长写法多样（duration/seconds/字符串/数字/metadata） | 判定函数覆盖全部形态，任一为 30 即命中 |
| 5 | 客户传 29 或 31 秒 | 不命中，走有赞（正常） |
| 6 | 客户不传时长 | 默认 5 秒 → 不命中，走有赞（正常） |
| 7 | **售价从 ¥6.00 变 ¥3.00，客户会察觉** | 这是本需求的目的；但要在文档/卡片里写清「30 秒走特惠档」 |
| 8 | 豆包官网链路**更慢**（浏览器自动化） | 30 秒视频耗时长，客户端超时阈值要够（教程页已写明超时实测） |
| 9 | 豆包链路**失败率**未知 | 上线后要盯 `tasks` 表 status 分布；漫屋链路失败是否退款也要确认 |
| 10 | 幂等/重试 | 重试时 body 已被改写，`OriginModelName` 变成 `seedance-2-5`，日志里看不到原始模型 → 需在 `properties` 里额外记 `route_from` |
| 11 | 广场展示 | 改写发生在路由层，广场仍只显示 `seedance2.5-特惠` 一个模型 ✅ 不需要动白名单 |
| 12 | `ModelPrice` | 改写后自动取 `seedance-2-5` 的 ¥3.0/次，**不需要**额外配价 |
| 13 | 转换逻辑有 bug | 转换失败一律**放弃路由**（走有赞），**绝不挡客户**、绝不带着少掉的素材建单 |

### 关于第 10 条的补充

`relay/relay_task.go:162` 的 `info.OriginModelName` 取的是 body 里的 model，
改写后就是 `seedance-2-5` 了。任务表的 `properties.origin_model_name` 会记成
`seedance-2-5`，**对外暴露了内部路由**（与「上游脱敏」的精神冲突）。
建议：在改写中间件里把原模型名塞进 `c.Set("route_origin_model", ...)`，
由 `controller/relay.go` 写进 `task.Properties` 的新字段（或复用 `input` 字段）。

---

## 五、决策点

**✅ 已拍板（老板 2026-10-30）：**
- **遇到参考视频/音频 → 不路由**，走有赞原路径（不是报错）。见 3.3.1。

**⏳ 待拍板：**
1. **命中后是否要回显**：比如响应里加个字段说明「本单走了 30 秒特惠档 ¥3.00」？
   （现在会只显示 ¥3.00，客户可能困惑为什么 30 秒比 15 秒还便宜 —— 因为按次）
2. **失败率兜底**：豆包链路如果失败率明显高于有赞，是否需要「失败自动回落有赞重跑」？
   （复杂，且会拖长响应时间，建议先上线观察）
3. **是否同时保留「有赞 30 秒」这条路径**：比如开关做成枚举（`off` / `only_30s` / `prefer_30s`）？

---

## 六、实施步骤

1. 新建 `setting/seedanceroute_setting/`（Policy + option 读写 + 默认关闭）
2. 新建 `middleware/seedance_route.go`（中间件 + 时长解析 + body 改写 + 图数断言）
3. 在 `router/video-router.go` 注册中间件（早于 `Distribute`）
4. 单元测试：时长解析（6 种写法）、开关关闭时零改动、图数断言、
   adaptive→auto 映射、**参考视频/音频不路由**
5. 部署 → 用**零成本探针**验路由（传 30 秒 + 无图，看是否命中漫屋）
6. 跑 1 条 30 秒真机任务，核对扣费 ¥3.00 与成片
7. 写生产 SQL 打开开关（默认先关，观察后再开）
