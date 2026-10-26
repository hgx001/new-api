# 下游接入教程（api.heibaidao.cn）

- **适用对象**：下游系统 / 开发者，拿 API key（`sk-` 开头）对接本网关
- **Base URL**：`https://api.heibaidao.cn/v1`
- **鉴权方式**：`Authorization: Bearer sk-xxxxxxxx`（也可用 `x-api-key` 头，仅 `/v1/messages`、`/v1/models` 路径生效）
- **兼容性**：OpenAI 兼容为主，另提供 Gemini 与 Anthropic 入口（见 §2）
- **最近更新**：2026-10-19 新增余额查询接口（见 §3）

---

## 1. 鉴权

所有请求都要带 API key。key 在控制台「令牌」页创建，支持设置：

- **过期时间**：可设永久或指定日期
- **额度**：可设限额或「不限量」（不限量时只消耗所属用户的余额）
- **分组**：令牌可绑定分组，走对应渠道池

```bash
curl https://api.heibaidao.cn/v1/models \
  -H "Authorization: Bearer sk-xxxxxxxx"
```

| 情况 | HTTP 状态 | 响应体 |
|---|---|---|
| 没带 key | 401 | `{"error":{"message":"无效的令牌","type":"new_api_error","code":""}}`（未提供与无效都归为同一文案） |
| key 不存在 / key 禁用或过期 / key 额度耗尽 | 401 | 同上 |
| 所属用户被封禁 | 403 | `{"error":{"message":"用户已被封禁",...}}` |

> 错误响应统一是 OpenAI 风格：`error.message` 可读、`error.type` 固定为 `new_api_error`、`error.code` 可能为空字符串。

---

## 2. 能力入口

| 入口 | 说明 |
|---|---|
| `GET /v1/models` | 模型列表（OpenAI 格式） |
| `POST /v1/chat/completions` | 对话（文本，流式/非流式） |
| `POST /v1/completions` | 文本补全 |
| `POST /v1/embeddings` | 向量 |
| `POST /v1/images/generations`、`/v1/images/edits` | 图像生成/编辑 |
| `POST /v1/audio/speech`、`/v1/audio/transcriptions` | 语音合成/识别 |
| `POST /v1/videos` | 视频任务建单（异步，见下） |
| `GET /v1/videos/:task_id` | 视频任务状态轮询 |
| `GET /v1/videos/:task_id/content` | 视频产物下载（代理回源） |
| `GET /v1/dashboard/billing/quota` | **余额查询**（只读，无需参数），见 §3 |
| `POST /v1/messages` | Anthropic 兼容入口（带 `x-api-key` + `anthropic-version` 头时也可用） |
| `POST /v1beta/models/*` | Gemini 兼容入口 |

对话示例：

```bash
curl https://api.heibaidao.cn/v1/chat/completions \
  -H "Authorization: Bearer sk-xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"你好"}]}'
```

---

## 3. 余额查询（给下游做余额监控）

### 3.1 接口

```
GET /v1/dashboard/billing/quota
Authorization: Bearer sk-xxxxxxxx
```

- **无需任何参数**。身份完全由 Authorization 头里的 key 决定——接口只返回**这把 key 自己**的余额，加 `?user_id=` 之类的参数也会被忽略，不存在越权查询的可能
- 查询本身**不扣费、不记日志、零副作用**
- 响应带 `Cache-Control: no-store`，禁止中间层缓存余额

### 3.2 完整响应示例

不限量令牌（`unlimited_quota=true`，只消耗用户钱包）：

```json
{
  "success": true,
  "message": "",
  "data": {
    "object": "billing.quota",
    "user":  { "id": 28, "username": "admin8989" },
    "token": { "id": 11, "name": "auto" },
    "quota": {
      "remain_user":      34913880,
      "used_user":        75423270,
      "remain_token":     null,
      "used_token":       64105986,
      "token_unlimited":  true,
      "effective_remain": 34913880,
      "unit":             "quota",
      "quota_per_unit":   500000,
      "display_remain":   "¥509.942648"
    },
    "warning": { "enabled": false, "threshold": 1000, "notify_type": "" },
    "stale_seconds_max": 5
  }
}
```

> 示例按站点当前展示口径计算：`quota_display_type=CNY`、汇率 7.3，即 `34913880 / 500000 × 7.3 ≈ ¥509.94`。展示口径可在后台调整，调整后 `display_remain` 会跟着变，`quota` 系列数值永远与口径无关，程序请用 `effective_remain`。

限额令牌（`unlimited_quota=false`）：

```json
{
  "success": true,
  "message": "",
  "data": {
    "object": "billing.quota",
    "user":  { "id": 28, "username": "admin8989" },
    "token": { "id": 12, "name": "video-bot" },
    "quota": {
      "remain_user": 34913880,
      "used_user":   75423270,
      "remain_token": 300,
      "used_token":   700,
      "token_unlimited": false,
      "effective_remain": 300,
      "unit": "quota",
      "quota_per_unit": 500000,
      "display_remain": "¥0.004380"
    },
    "warning": { "enabled": true, "threshold": 500000, "notify_type": "webhook" },
    "stale_seconds_max": 5
  }
}
```

### 3.3 字段说明

| 字段 | 类型 | 含义 |
|---|---|---|
| `remain_user` | int | 用户钱包剩余，**权威余额**，单位 quota |
| `used_user` | int | 用户累计已消耗，仅供参考 |
| `remain_token` | int \| null | 令牌剩余。**`unlimited_quota=true` 时为 `null`**（不限量令牌没有令牌级余额概念，别把它当 0 或负数用） |
| `used_token` | int | 令牌累计已消耗 |
| `token_unlimited` | bool | 令牌是否不限量 |
| `effective_remain` | int | **下游真正能花的钱** = `min(remain_user, remain_token)`；令牌不限量时等于 `remain_user`。**做余额预警请以这个字段为准** |
| `unit` / `quota_per_unit` | string / number | 计价单位说明。`quota_per_unit = 500000`，即 **500000 quota = $1**，再按站点汇率折算显示货币 |
| `display_remain` | string | 人类可读余额（跟随站点设置的币种/符号），仅供展示，程序判断请用 `effective_remain` |
| `warning` | object | 该用户配置的额度预警：`enabled` = 是否配了通知方式；`threshold` = 预警阈值（quota）；`notify_type` = 通知方式（`email`/`webhook`/`bark`/`gotify`，空表示未配置） |
| `stale_seconds_max` | int | 数据最大陈旧秒数。服务端为提升性能按批量落库，最多可能落后这个秒数 |

### 3.4 HTTP 语义

| 状态码 | 含义 |
|---|---|
| 200 | 查询成功。**余额很小也返回 200**——余额不足不是查询失败，是查出来一个很小的数 |
| 401 | 没带 key / key 不存在 / key 被禁用或过期 |
| 403 | 所属用户被封禁 |
| 429 | 触发限流（见 3.5），退避后重试。429 响应不带 `Retry-After` 头，按 §3.5 的固定间隔退避即可 |
| 500 | 服务端查询失败，错误体同 §1 的 OpenAI 风格（`error.type = server_error`），可重试 |

查询接口的**错误响应也走 OpenAI 风格**（`{"error":{"message":"...","type":"...","code":""}}`），不是 `{"success":false}` 形状——拿到非 2xx 按 `error.message` 处理即可。

**永远不会有 402。** 余额不足发生在调用模型接口时，那是另一种错误（见 §4）。

### 3.5 限流与轮询建议

- 每用户 **6 次 / 60 秒**（按用户 ID 计数，换 IP 绕不过）
- 推荐轮询间隔 **10 秒以上**；监控告警用「低于阈值才报警 + 报警后静默一段时间」即可，不需要 1 秒级轮询
- 429 响应不带头信息，收到后直接等下一个轮询周期再试即可，不会丢数据；不要把限流当故障重试轰炸

### 3.6 Python 轮询示例

```python
import time
import requests

BASE = "https://api.heibaidao.cn/v1"
KEY = "sk-xxxxxxxx"

def get_quota():
    r = requests.get(f"{BASE}/dashboard/billing/quota",
                     headers={"Authorization": f"Bearer {KEY}"}, timeout=10)
    if r.status_code == 401:
        raise RuntimeError("API key 无效或被禁用")
    if r.status_code == 429:
        return None  # 触发限流，下一轮再试
    r.raise_for_status()
    q = r.json()["data"]["quota"]
    return q["effective_remain"], q["token_unlimited"], q["remain_token"]

ALERT_THRESHOLD = 100_000_000   # 按需调整，单位 quota
while True:
    result = get_quota()
    if result is not None:
        effective, unlimited, token_remain = result
        if effective < ALERT_THRESHOLD:
            print(f"余额不足告警：剩余 {effective} quota，请充值")
    time.sleep(10)
```

---

## 4. 余额不足时的行为（调用模型接口时）

- **你自己这把 key 的余额不足**：请求返回 **403**，`error.message` 形如 `用户额度不足, 剩余额度: ¥X`。这个错误发生在选渠道之前，不会扣费、不会调用上游
- **中转侧某个上游渠道余额不足**：网关会自动切换到同模型的下一个可用渠道，下游无感知；渠道被自动禁用后需等管理员恢复
- **同模型所有渠道都不可用**：错误会透传，形态是上游原文

---

## 5. 注意事项

1. **模型列表里有少量历史遗留名字不可调用**：`/v1/models` 目前会列出少量已下架的 `gpt-5.x` 旧名，调用返回 503 `model_not_found` 且**不扣费**。以下游实际测试结果为准，别把列表当可用性承诺；发现某个模型调不通请联系管理员确认
2. **别把 `remain_token` 当唯一余额**：不限量令牌它是 `null`；限额令牌它可能大于钱包余额。统一用 `effective_remain`
3. **金额单位是 quota 不是元**：程序内一律用 `effective_remain`（quota），要展示再除以 `quota_per_unit`（或直接用 `display_remain`）
4. **令牌不限制来源 IP**：key 在任何 IP 都能访问全部接口（含余额查询）；需要限 IP 请在网关层（nginx / CDN）做
5. **视频类任务是异步的**：`POST /v1/videos` 建单后轮询 `GET /v1/videos/{task_id}` 取状态，产物走 `GET /v1/videos/{task_id}/content`；反解类参考视频只收公网 `http(s)` URL

---

## 6. 变更记录

| 日期 | 变更 |
|---|---|
| 2026-10-19 | 新增 `GET /v1/dashboard/billing/quota` 余额查询接口（§3），供下游自助监控余额 |
| — | OpenAI 兼容对话 / 模型列表 / Gemini 入口 / Anthropic 入口 |
