// Package promptcompress_setting 保存「wan3 提示词超长自动压缩」的接入配置。
//
// 为什么单独一个包：压缩是**兜底链路**，只在「豆包接不走 + 提示词超过上游 5000
// 字符上限」时才跑。它的模型、端点、超时、内部令牌与「路由策略」「模型定价」
// 都是两回事，混在一起会让路由中间件被迫依赖计费配置。
//
// 为什么默认关闭：这条链路会**改写客户的提示词**。默认开着等于对全站生效，
// 而它只在少数客户需要超过 5000 字符时才有价值 —— 默认关、按需开，
// 行为与改造前逐字节一致。
//
// 数据流：
//
//	管理员/SQL 改 options 表的 Wan3PromptCompress
//	  → model/option.go 分发到本包 UpdateFromJSONString
//	  → middleware/wan3_prompt_compress.go 在条件路由之后读 Get()
//	  → 命中则调内部 /v1/chat/completions 压缩 → 四条不变量校验
//	  → 通过则改写 body.prompt（模型与计费不变）；不过则 400 提示词过长
package promptcompress_setting

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// OptionKey 是这份配置在 options 表里的键名。
const OptionKey = "Wan3PromptCompress"

// 压缩后的目标长度上限（字符）。
//
// 5000 是上游 wan3 的硬上限（constant/seedance25_family.go 的书面事实）。
// 4600 是**喂给模型的软目标**：模型数不准字符数，留 8% 余量；
// 落在 4600~5000 之间照样算过 —— 硬校验只有一条：≤ 5000。
const (
	defaultTargetRunes   = 4600
	maxTargetRunes       = 5000
	defaultTimeoutSecond = 25
	maxTimeoutSecond     = 120
	defaultMaxAttempts   = 2
	maxMaxAttempts       = 3
)

// Config 是提示词压缩的接入配置。
type Config struct {
	// Enabled 总开关。false = 完全不介入（默认，行为与改造前一致）。
	Enabled bool `json:"enabled"`

	// Model 压缩用的模型名，默认 MiniMax-M3.1（渠道 1，与豆包/有赞完全独立）。
	Model string `json:"model"`

	// Endpoint 内部调用地址，指向本站的 chat completions。
	//
	// 为什么走内部 HTTP 而不是直接调渠道：压缩要经过 normal 的中转链
	// （渠道选择、计费、重试），自己拼上游请求等于把渠道适配逻辑复制一份。
	// 走本站接口的另一个好处是消耗记在独立令牌下，可单独查账。
	Endpoint string `json:"endpoint"`

	// Token 是内部调用用的专用令牌（只允许 MiniMax-M3.1）。
	// ⚠️ 明文存储，只在 option 表里，不要出现在日志或前端。
	Token string `json:"token"`

	// TargetRunes 是喂给模型的**目标长度**（指令里写"≤ N 字符"）。
	//
	// ⚠️ 不是硬校验阈值：硬校验永远是 5000（上游上限）。
	// 目标写小一点，模型才有一轮就过的余量。
	TargetRunes int `json:"target_runes"`

	// TimeoutSeconds 单次调用超时（秒）。视频是异步任务，25 秒可以接受。
	TimeoutSeconds int `json:"timeout_seconds"`

	// MaxAttempts 最大尝试次数（含首次）。第二次会带「上一版超了多少」的反馈。
	MaxAttempts int `json:"max_attempts"`

	// ExposeHeader 是否在响应头回 `X-Wan3-Prompt-Compressed: 6120->3800`。
	//
	// 默认 false：客户请求的模型没变、计费没变，压缩是平台对上游限制的内部适配；
	// 主动披露容易被理解成「你们偷偷改了我的提示词」。留这个开关是给
	// 「客户明确要求可观测」的场景。
	ExposeHeader bool `json:"expose_header"`
}

// defaultConfig 与「不做压缩」等价：开关关、模型名与端点仍是可用值。
//
// ⚠️ Enabled 默认 false 是硬要求：没有这个 option 时（或 option 被删了），
// 中间件必须直接放行 —— 行为与改造前逐字节一致。
func defaultConfig() Config {
	return Config{
		Enabled:        false,
		Model:          "MiniMax-M3.1",
		Endpoint:       "http://127.0.0.1:3000/v1/chat/completions",
		Token:          "",
		TargetRunes:    defaultTargetRunes,
		TimeoutSeconds: defaultTimeoutSecond,
		MaxAttempts:    defaultMaxAttempts,
		ExposeHeader:   false,
	}
}

var (
	mu      sync.RWMutex
	current = defaultConfig()
)

// Get 返回当前配置的副本（值类型，调用方随便改）。
func Get() Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// MarshalJSONString 把当前配置序列化成 JSON（写回 option 用）。
func MarshalJSONString() string {
	mu.RLock()
	defer mu.RUnlock()
	encoded, err := common.Marshal(current)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// UpdateFromJSONString 用管理员提交的 JSON 覆盖配置。
//
// 缺省字段一律回落默认值，而不是保留上一次的值 —— 这份配置是**整份提交**的，
// 「这次没写 timeout」应该表示「用默认超时」，不是「沿用三个月前那次的 8 秒」。
// （与 seedanceroute_setting 的语义不同：那份是部分覆盖，因为它允许按源模型
// 只配一个字段。）
//
// 越界值直接夹到合法区间并拒绝开启：配出 5000 以上的目标等于让模型顶着上游
// 上限写，必然次次重试；配出 0 秒超时等于每次必失败。
func UpdateFromJSONString(jsonStr string) error {
	var parsed Config
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	parsed = normalize(parsed)
	mu.Lock()
	defer mu.Unlock()
	current = parsed
	return nil
}

// normalize 补齐缺省值并夹住越界值。
func normalize(cfg Config) Config {
	fallback := defaultConfig()
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = fallback.Model
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = fallback.Endpoint
	}
	if cfg.TargetRunes <= 0 {
		cfg.TargetRunes = fallback.TargetRunes
	}
	if cfg.TargetRunes > maxTargetRunes {
		cfg.TargetRunes = maxTargetRunes
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = fallback.TimeoutSeconds
	}
	if cfg.TimeoutSeconds > maxTimeoutSecond {
		cfg.TimeoutSeconds = maxTimeoutSecond
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = fallback.MaxAttempts
	}
	if cfg.MaxAttempts > maxMaxAttempts {
		cfg.MaxAttempts = maxMaxAttempts
	}
	return cfg
}

// Usable 判断「配置是否具备真正跑一次压缩的条件」。
//
// 没配内部令牌 = 压缩跑不了。此时中间件不会硬等一次注定 401 的调用，
// 而是按「压不下来」处理（400 提示词过长），并在日志里点名缺令牌 ——
// 静默跳过会让超长请求打到上游拿一个更难懂的错误。
func (c Config) Usable() bool {
	return c.Enabled && strings.TrimSpace(c.Token) != ""
}
