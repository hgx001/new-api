package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 路由层的固定档常量必须与后端唯一来源**逐值相等**。
//
// 为什么需要这条：路由层把「按次固定档」的时长归一成 perCallFixedTierSeconds，
// 源链路（youzanwan3）用它做 fail-loud 门控。两处不一致时：
//
//	路由层归一成 30、源链路门控 20 → 客户传 20 秒：路由层认成 30 放行，
//	  源链路判 20 != 30 → 400 —— 客户明明传的就是合法档却被打回；
//
//	路由层归一成 20、源链路门控 30 → 客户传 30 秒：路由层认为非固定档 →
//	  放弃路由 → 源链路放行 30 秒 —— 恰好能跑，但客户传 5 秒时
//	  路由层归一成 20 写回请求体 → 源链路 400。
//
// 两种都表现为「同一个模型、同样的参数，时好时坏」，极难排查。
//
// 现在这两个常量是**派生**关系（都来自 constant.Seedance25PerCallFixedSeconds），
// 编译期就不可能不一致。这条测试守的是「派生关系没被改回手写字面量」。
func TestFixedTier_路由层常量与唯一来源一致(t *testing.T) {
	if perCallFixedTierSeconds != constant.Seedance25PerCallFixedSeconds {
		t.Fatalf(
			"路由层固定档常量 %d 与 constant.Seedance25PerCallFixedSeconds %d 不一致 —— "+
				"请让 perCallFixedTierSeconds 派生自 constant 的单一来源，"+
				"不要写手写字面量（手写副本会悄悄漂移，且不报错）",
			perCallFixedTierSeconds, constant.Seedance25PerCallFixedSeconds)
	}
}

// TestFixedTier_归一后的时长就是固定档 守的是「路由层归一落到 30 秒」。
//
// 这条锁的是**行为**（归一结果），不只是常量相等：
// 万一有人改了归一函数里的写入值，这条会先红。
func TestFixedTier_归一后的时长就是固定档(t *testing.T) {
	body := map[string]interface{}{"model": constant.Seedance25HuoDongModel}
	out, ok := normalizeFixedTierSeconds(body)
	if !ok {
		t.Fatalf("未传时长时归一必须成功（应归一成 %d 秒）",
			constant.Seedance25PerCallFixedSeconds)
	}
	seconds, has := extractRequestSeconds(out)
	if !has {
		t.Fatalf("归一后必须写入 seconds，否则改派到目标链路会缺省成 5 秒 —— " +
			"客户会「按 30 秒的价拿到 5 秒的片」")
	}
	if seconds != constant.Seedance25PerCallFixedSeconds {
		t.Fatalf("归一后的时长 = %d，期望 %d",
			seconds, constant.Seedance25PerCallFixedSeconds)
	}
}

// 按次档 2026-11-11 起支持 2–30 秒：区间内显式时长必须原样保留，
// 不能被归一成 30 秒（静默改档），越界时长必须放弃路由（交给源链路报错）。
func TestFixedTier_时长区间归一(t *testing.T) {
	cases := []struct {
		name       string
		body       map[string]interface{}
		wantOK     bool
		wantSecond int
	}{
		{name: "下限 2 秒", body: map[string]interface{}{"seconds": float64(2)}, wantOK: true, wantSecond: 2},
		{name: "区间内 12 秒", body: map[string]interface{}{"seconds": float64(12)}, wantOK: true, wantSecond: 12},
		{name: "语义边界 20 秒", body: map[string]interface{}{"seconds": float64(20)}, wantOK: true, wantSecond: 20},
		{name: "上限 30 秒", body: map[string]interface{}{"seconds": float64(30)}, wantOK: true, wantSecond: 30},
		{name: "字符串 25", body: map[string]interface{}{"seconds": "25"}, wantOK: true, wantSecond: 25},
		{name: "1 秒越界", body: map[string]interface{}{"seconds": float64(1)}, wantOK: false},
		{name: "31 秒越界", body: map[string]interface{}{"seconds": float64(31)}, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := normalizeFixedTierSeconds(tc.body)
			require.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				assert.Nil(t, out)
				return
			}
			seconds, has := extractRequestSeconds(out)
			require.True(t, has)
			assert.Equal(t, tc.wantSecond, seconds)
		})
	}
}

// 改派到心笔按次链路时，下发时长必须等于客户请求的时长（2–30 内），
// 没传才落默认 30 秒 —— 固定写死 30 会让「请求 22 秒」的客户拿到 30 秒的片。
func TestXinbiPerCallRewriteKeepsRequestedSeconds(t *testing.T) {
	t.Run("保留 22 秒", func(t *testing.T) {
		out := rewriteBodyForXinbiPerCall(map[string]interface{}{
			"model":   constant.Seedance25HuoDongModel,
			"seconds": float64(22),
		}, xinbiPerCallTargetModel)
		assert.Equal(t, "22", out["seconds"])
		assert.Equal(t, xinbiPerCallTargetModel, out["model"])
	})

	t.Run("没传时长落默认 30 秒", func(t *testing.T) {
		out := rewriteBodyForXinbiPerCall(map[string]interface{}{
			"model": constant.Seedance25HuoDongModel,
		}, xinbiPerCallTargetModel)
		assert.Equal(t, "30", out["seconds"])
	})
}
