package seedanceroute_setting

import (
	"fmt"
	"strconv"
	"strings"
)

// 规则一「比例闸门」的后台勾选入口（2026-11-12）。
//
// 闸门本身（Policy.SecondsRuleRatioGate）与判定（middleware.secondsRatioGatePasses）
// 早就有了，但生产上一直只能靠 SQL 改 options 表来开/关
// （scripts/prod-seedance-gate-20261112.sql）。运营在后台既看不到、
// 也改不动，所以这里补上唯一的配置写入口：一个勾选 = 要不要这个条件。
//
// 语义：
//   - 勾上（enabled=true）：写入闸门比例，不传值就用 9:16；
//   - 取消（enabled=false）：清空闸门，回到「任何比例超阈值都改派」的旧行为。
//
// 空闸门 = 不生效，是刻意的向后兼容：2026-11-12 之前的生产 option JSON
// 里没有这个字段，解析后为空、行为与改造前逐字节一致。

// DefaultSecondsRuleRatioGate 是后台勾选不指定比例时使用的默认值。
//
// 运营看到的是一个勾选框，不该再被要求填比例字符串；9:16 是当前生产
// 唯一在用的闸门值（竖屏短视频），也是规则二 trigger_ratio 的口径。
const DefaultSecondsRuleRatioGate = "9:16"

// WithSecondsRuleRatioGate 返回「把可选比例条件设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由 controller 用 model.UpdateOption 完成，
// 与 WithSecondsRulePausedUntil / WithManualPause 同一套做法。
//
// ⚠️ 只改 SecondsRuleRatioGate 与 UpdatedAt 两个字段：这份策略里还压着
// 暂停截止时间、按源模型覆盖、规则二/三的目标模型、豆包链路开关状态等
// 经营配置，勾一个条件把它们冲掉是没有任何报错的静默事故。
//
// 比例值非法时返回错误而不是写进去：一个永远匹配不上的闸门值
// 会让所有超阈值请求都不再改派，而后台勾选看起来仍是勾着的。
// 取消勾选时不校验比例值 —— 运营关条件不需要先填一个合法比例。
func WithSecondsRuleRatioGate(enabled bool, ratio string) (Policy, error) {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()

	if !enabled {
		out.SecondsRuleRatioGate = ""
		out.UpdatedAt = nowUnix()
		return out, nil
	}

	normalized, err := normalizeRatioGateValue(ratio)
	if err != nil {
		return Policy{}, err
	}
	out.SecondsRuleRatioGate = normalized
	out.UpdatedAt = nowUnix()
	return out, nil
}

// normalizeRatioGateValue 归一比例写法，口径与 middleware.normalizeRatioText 一致。
//
// 为什么要重新写一份而不是复用：middleware 依赖本包，反过来依赖会形成
// 循环；而配置侧必须与请求侧按同一种格式比对，否则「配置里写 9／16、
// 请求里是 9:16」会变成永远不命中的静默故障。
//
// 接受 "9:16" / "9/16" / "9：16" / 带空格 / 带前导零，统一输出 "9:16"。
// 空值回落到 DefaultSecondsRuleRatioGate。
func normalizeRatioGateValue(value string) (string, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return DefaultSecondsRuleRatioGate, nil
	}
	text = strings.ReplaceAll(text, "：", ":")
	text = strings.ReplaceAll(text, "/", ":")
	parts := strings.Split(text, ":")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid ratio %q: expected W:H", value)
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", fmt.Errorf("invalid ratio %q: expected positive integers", value)
	}
	return strconv.Itoa(w) + ":" + strconv.Itoa(h), nil
}
