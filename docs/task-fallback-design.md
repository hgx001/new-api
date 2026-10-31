# 任务失败兜底：`seedance2.5-特惠` → `wan3.0-720p-特惠`

状态：**设计待拍板，未实施**（2026-10-31）

## 一、结论先行

「失败后自动转派」分两类，难度差一个量级：

| 失败类型 | 发生位置 | 客户在做什么 | 能否兜底 |
|---|---|---|---|
| **提交失败** | `POST /v1/videos` 同步返回错误 | 在等 HTTP 响应 | 容易，改重试循环即可 |
| **执行失败** | 轮询到 `FAILURE`（几秒到几分钟后） | 已在轮询 `GET /v1/videos/{id}` | 难，要重建请求 + 保住 task_id |

生产里**绝大多数是执行失败**（「生成结果疑似包含真人面部」「错误请重试」），提交失败只有参考视频超限那类。

所以：**不做执行失败转派，这个需求等于没做**。

## 二、现状事实（已核对，不是推测）

1. **`RetryTimes = 1`**（生产 `options` 表），提交失败会重试 1 次。
2. **`seedance2.5-特惠` 只有 ch17 一条渠道** —— 重试时换不到别的渠道，等于没重试。
3. **提交失败会全额退还预扣**：`RelayTaskSubmit` 步骤 7 先 `PreConsumeBilling`，`controller/relay.go` 的 `RelayTask` 有 `defer` 在 `taskErr != nil` 时调 `Billing.Refund(c)`。而且步骤 7 有 `if info.Billing == nil` 守卫 —— **同一请求内换模型重试不会重复扣费**。
4. **执行失败会退款**：`service/task_polling.go` 的 `TaskStatusFailure` 分支置 `shouldRefund = true` → `RefundTaskQuota`。
5. **任务里没存原始请求参数**：`model.Properties` 只有 `input`（恒空）、`upstream_model_name`、`origin_model_name`。**没有 prompt / duration / ratio / media**。→ 转派时无法重建请求。
6. **轮询阶段没有提交能力**：`TaskPollingAdaptor` 接口只有 `Init` / `FetchTask` / `ParseTaskResult` / `AdjustBillingOnComplete`，没有 `DoRequest`。而且轮询用的是 `context.Context`，`RelayTaskSubmit` 要 `*gin.Context`。
7. **客户 task_id 不能变**：客户端已经拿到 `task_xxx` 并在轮询，新建任务等于客户永远拿不到结果。

## 三、四个方案

### A. 渠道级备选（零代码）

给 ch12 配 `model_mapping: seedance2.5-特惠 → wan3.0-video`，`abilities` 加 `(ch12, seedance2.5-特惠)`，priority 低于 ch17。

- ✅ 复用现有「同模型换渠道重试」，不动代码
- ❌ 只覆盖提交失败
- ❌ 走的是**单跳 480P**：`isOfficial720PModel()` 判的是对外模型名，这里对外仍是 `seedance2.5-特惠`，两跳链路不生效 → 交付质量和成本都变

### B. 提交失败换模型重试（同步，中等代码量）

改 `controller/relay.go:526` 的循环：渠道耗尽后，把 `relayInfo.OriginModelName` 换成 `wan3.0-720p-特惠`，重写 body 的 `model`，重算 `PriceData`，再走一轮。

- ✅ 覆盖提交失败，客户无感（拿到的就是 wan3 的 task_id）
- ✅ 账务简单：`RelayTaskSubmit` 步骤 7 有 `if info.Billing == nil` 守卫，换模型重试**不会重复预扣**；最终失败由 `RelayTask` 的 defer 一次性退还
- ❌ 覆盖不到执行失败

### C. 执行失败转派（异步，大代码量）

在 `TaskStatusFailure` 分支里，先不退款，用快照重建请求提交到 wan3，复用**同一个 task_id**。

需要三件前置工作：

**C1. 建单时保存请求快照**

`PrivateData` 加 `ReplayRequest`（JSON 列，无需迁移）：

```go
type TaskReplayRequest struct {
    Prompt     string
    Duration   int
    Ratio      string
    Size       string
    Resolution string
    Media      []TaskMedia  // 只存 URL；data URL 超过 8KB 的整条放弃
    Truncated  bool         // 有素材没存下来 → 不可转派
}
```

只对白名单模型（`seedance2.5-特惠`）存，避免全表膨胀。

**C2. 轮询里拿到提交能力**

两种做法：

- **C2a（推荐）**：抽一个不依赖 `gin.Context` 的 `SubmitTaskInternal(replay, channel, model)`，内部只做「建 URL + 发 HTTP + 解析回执」。需要给 19 个 adaptor 加一个可选接口（类型断言，不实现就走不了转派），不破坏现有接口。
- **C2b（快但脆）**：用 `gin.CreateTestContext` 伪造一个 context，塞好 body storage 和必要 key，直接调 `RelayTaskSubmit`。生产代码里用测试工具，后续 gin 升级容易断。

**C3. 转派条件（缺一不可）**

1. 源模型在转派白名单里（配置项，不硬编码）
2. `PrivateData.RedispatchedAt == 0`（**只转派一次**，防循环）
3. `ReplayRequest` 存在且 `Truncated == false`
4. 目标模型有可用渠道
5. 请求参数在 wan3 能力内：时长 2~30 秒、非 1080P、参考视频 ≤15 秒且「参考+生成 ≤30 秒」、参考图 ≤10
6. 失败原因属于可重试类型（**需要拍板**，见下）
7. 提交后未超过时间窗（建议 30 分钟，超时客户可能已经放弃）

**C4. 账务**

不退款、不重新扣费，只换执行链路：

- 两个模型同价（¥0.20/秒），`BillingContext` 不用改
- 转派成功 → 轮询继续，最终成功则正常结算；失败则按原额度退款
- 需要更新 `ChannelId`、`PrivateData.UpstreamTaskID`、`Properties.OriginModelName`
- 加一条 task log「已转派到 wan3」，后台可见

**C5. 并发安全**

多个节点可能同时轮询同一任务 → 转派必须走 `UpdateWithStatus` 的 CAS（已有），只有抢到的那次执行转派。

### D. 扩大 wan3 路由范围（避免失败，而不是补救）

把 `trigger_ratio` 从 `9:16` 扩展成「排除法」：只要不是 1080P、参考视频合规，全部路由 wan3。

- ✅ 从源头减少走不稳定链路，零新增机制
- ✅ 已有的参考视频挡板直接复用
- ❌ 需要确认 wan3 对 16:9 / 1:1 / 21:9 各比例的交付质量
- ❌ wan3 自己挂了就全挂（有赞变成备选，需要回退机制）

## 四、推荐路径

**第一步（今天就能做）：D + B**

D 把大部分请求提前导到 wan3，B 兜住提交失败。两者都不碰任务表、不碰账务、不动轮询，风险最低。

**第二步（需要拍板）：C**

只有 C 能兜住执行失败。但 C 要动三处核心：任务表结构、轮询主循环、计费上下文。建议单独排期，不要和路由调整混在一起。

## 五、需要老板拍板的问题

1. **内容审核失败要不要转派？**
   生产 4 条失败都是「生成结果疑似包含真人面部」。换到 wan3 大概率还是被拒（两家审核都严），转派等于白烧一次上游额度。建议**不转派**这类，只转「错误请重试」这类临时故障。

2. **转派后上游成本上升，能接受吗？**
   带参考视频时 wan3 按「输入+输出」计费，有赞只按生成秒数计费。同一条请求走 wan3 的上游成本更高，客户价格不变。

3. **C 的提交能力用 C2a 还是 C2b？**
   C2a 干净但要给 19 个 adaptor 加接口；C2b 一天能出但要接受「生产代码里用测试工具」的技术债。

4. **时间窗定多久？**
   建议 30 分钟。超过这个时间客户可能已经放弃，转派等于白花钱。

5. **要不要先做 A 当临时兜底？**
   零代码，十分钟能上，但只覆盖提交失败，且交付质量会变（单跳 480P）。如果只是想先止血，可以上；想彻底解决就直接 B。
