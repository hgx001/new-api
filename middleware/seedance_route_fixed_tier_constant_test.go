package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
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
