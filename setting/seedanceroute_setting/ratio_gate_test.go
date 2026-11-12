package seedanceroute_setting

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// 规则一「可选比例条件」的后台勾选（2026-11-12）。
//
// 生产上这个条件此前只能靠 SQL 手工写 options 表
// （scripts/prod-seedance-gate-20261112.sql）。加了后台勾选之后，
// 写入路径必须满足三件事：
//  1. 勾上 = 写进闸门值（默认 9:16），取消 = 清空，空闸门保持旧行为；
//  2. 只动闸门字段，不能顺手改掉暂停时间、按源覆盖、比例规则等经营配置；
//  3. 纯函数不落库（与 WithSecondsRulePausedUntil 同一套做法），
//     由 controller 决定何时写。
//
// 这里锁死上述口径，避免「勾一个条件把整份策略冲掉」这类静默事故。

const ratioGateBaselineJSON = `{
	"enabled": true,
	"source_model": "seedance2.5-特惠",
	"source_models": ["seedance2.5-活动"],
	"target_model": "seedance-2-5",
	"trigger_seconds": 20,
	"ratio_target_model": "wan3.0-720p-特惠",
	"trigger_ratio": "9:16",
	"frames_target_model": "wan3.0-720p-特惠",
	"seconds_rule_ratio_gate": "",
	"source_overrides": {
		"seedance2.5-活动": {"target_model": "seedance-2-5", "seconds_rule_enabled": false}
	}
}`

// withRatioGateBaseline 把全局策略重置成一份「配全了四条规则」的基线。
//
// 不用既有的 restore(t)：MarshalJSONStringOf 只序列化老字段，
// 会把 seconds_rule_ratio_gate / source_overrides 漏掉，
// 测试之间会互相污染（上一个用例勾上的闸门流到下一个用例）。
func withRatioGateBaseline(t *testing.T) {
	t.Helper()
	require.NoError(t, UpdateFromJSONString(ratioGateBaselineJSON))
	t.Cleanup(func() {
		_ = UpdateFromJSONString(ratioGateBaselineJSON)
	})
}

// 勾上但没指定比例值 → 用 9:16。运营看到的勾选只有一个，
// 不该再要求他填一个比例字符串。
func TestWithSecondsRuleRatioGateEnableUsesDefaultNineSixteen(t *testing.T) {
	withRatioGateBaseline(t)

	policy, err := WithSecondsRuleRatioGate(true, "")

	require.NoError(t, err)
	require.Equal(t, DefaultSecondsRuleRatioGate, policy.SecondsRuleRatioGate)
	require.Equal(t, "9:16", policy.SecondsRuleRatioGate)
}

// 比例写法归一：与 middleware.normalizeRatioText 同一口径
// （顶层 ratio / metadata.ratio / size 推断都按这个格式比对）。
// 后台传进来的值不归一，会出现「配置里写着 9／16、请求里是 9:16」
// 于是永远不命中的静默故障。
func TestWithSecondsRuleRatioGateNormalizesRatio(t *testing.T) {
	withRatioGateBaseline(t)

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"标准写法", "16:9", "16:9"},
		{"斜杠", " 9/16 ", "9:16"},
		{"中文冒号", "9：16", "9:16"},
		{"前导零", "09:016", "9:16"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := WithSecondsRuleRatioGate(true, tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.want, policy.SecondsRuleRatioGate)
		})
	}
}

// 非法比例必须报错，不能写进配置。
// 一旦写进一个永远匹配不上的值，结果是「所有超阈值请求都不再改派」，
// 而且后台勾选看起来是勾着的 —— 排查成本极高。
func TestWithSecondsRuleRatioGateRejectsInvalidRatio(t *testing.T) {
	withRatioGateBaseline(t)

	// 注意：纯空白不在此列 —— 它与不传 ratio 等价，都该落到默认 9:16。
	for _, invalid := range []string{"abc", "adaptive", "9:", ":16", "0:16", "9:0", "-1:9", "9:16:1", "9:16x"} {
		t.Run(invalid, func(t *testing.T) {
			_, err := WithSecondsRuleRatioGate(true, invalid)
			require.Error(t, err, "非法比例 %q 必须拒绝", invalid)
		})
	}
}

// 取消勾选 = 清空闸门，回到「任何比例超阈值都改派」的旧行为。
// 取消时不该校验比例值：运营关条件，不需要先填一个合法比例。
func TestWithSecondsRuleRatioGateDisableClearsWithoutValidatingRatio(t *testing.T) {
	withRatioGateBaseline(t)
	require.NoError(t, UpdateFromJSONString(`{
		"enabled": true, "source_model": "seedance2.5-特惠",
		"target_model": "seedance-2-5", "trigger_seconds": 20,
		"seconds_rule_ratio_gate": "9:16"
	}`))

	policy, err := WithSecondsRuleRatioGate(false, "这不是比例")

	require.NoError(t, err)
	require.Empty(t, policy.SecondsRuleRatioGate)
}

// 只改闸门字段。勾一个条件不能把暂停截止时间、按源模型覆盖、
// 规则二/三的配置、豆包链路开关状态一起冲掉。
func TestWithSecondsRuleRatioGateOnlyTouchesGateField(t *testing.T) {
	withRatioGateBaseline(t)
	before := Get()

	policy, err := WithSecondsRuleRatioGate(true, "9:16")
	require.NoError(t, err)

	// 闸门与 UpdatedAt 是本次写入允许改动的两个字段，抹平后其余逐字段相同。
	policy.SecondsRuleRatioGate = ""
	policy.UpdatedAt = 0
	before.SecondsRuleRatioGate = ""
	before.UpdatedAt = 0
	require.Equal(t, before, policy)
}

// 纯函数：调完不落库、不改内存状态。落库是 controller 的职责。
func TestWithSecondsRuleRatioGateDoesNotMutateGlobal(t *testing.T) {
	withRatioGateBaseline(t)
	before := Get()

	_, err := WithSecondsRuleRatioGate(true, "9:16")
	require.NoError(t, err)

	require.Equal(t, before, Get(), "纯函数不该改全局策略")
}

// 空闸门 omitempty：旧的生产 option JSON 里没有这个字段，
// 写回去时也不该凭空多出一个空字段（保持配置 diff 干净）。
func TestSecondsRuleRatioGateOmittedFromJSONWhenEmpty(t *testing.T) {
	withRatioGateBaseline(t)

	raw, err := json.Marshal(Get())
	require.NoError(t, err)
	require.NotContains(t, string(raw), "seconds_rule_ratio_gate")
}

// 端到端：勾选后的策略序列化 → 经 option 分发链读回 → 闸门生效，
// 且整份策略不会因为新增字段被 anyRuleReady 判成「没配全」而关掉。
func TestSecondsRuleRatioGateRoundTripThroughOptionJSON(t *testing.T) {
	withRatioGateBaseline(t)

	policy, err := WithSecondsRuleRatioGate(true, "9:16")
	require.NoError(t, err)
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	require.NoError(t, UpdateFromJSONString(string(raw)))

	require.Equal(t, "9:16", Get().SecondsRuleRatioGate)
	require.True(t, Get().Enabled, "勾上条件不能把总开关关掉")
	require.True(t, IsEnabled(), "规则一仍应生效")
	require.Equal(t, "9:16", Get().TriggerRatio, "规则二自己的触发比例不能被覆盖")
}
