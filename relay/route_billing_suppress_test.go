package relay

// 「一口价模型降级到按秒链路时屏蔽倍率」的测试（2026-11-14）。
//
// 触发场景：规则五把 seedance-2-5（¥3.50/次）降级到 CapCut
// cp-seedance-2.5flash —— CapCut adaptor 一律返回 {"seconds": 5}，
// 不清理就会 ¥3.50 × 5 = ¥17.50（超收 5 倍，账单上看不出异常）。
//
// 本文件锁三件事：
//  1. 没有标记 → 倍率原样保留（不能影响任何既有链路）；
//  2. 有标记 → 倍率被清空（而不是设成 1，避免日志看起来像按秒计费）；
//  3. 没有倍率时静默跳过（不制造噪音日志）。

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

// newSuppressTestContext 造一个带（或不带）屏蔽标记的 gin 上下文。
func newSuppressTestContext(t *testing.T, marked bool) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(nil)
	if marked {
		common.SetContextKey(c, constant.ContextKeyRouteSuppressTaskRatios, true)
	}
	return c
}

func TestSuppressRouteTaskRatiosClearsRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newSuppressTestContext(t, true)
	info := &relaycommon.RelayInfo{}
	info.PriceData = types.PriceData{
		OtherRatios: map[string]float64{"seconds": 5, "size": 2},
	}

	suppressRouteTaskRatios(c, info)

	assert.Nil(t, info.PriceData.OtherRatios,
		"标记存在时必须清空倍率，否则一口价会被乘成 N 倍")
}

func TestSuppressRouteTaskRatiosLeavesUnmarkedRequestsAlone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newSuppressTestContext(t, false)
	info := &relaycommon.RelayInfo{}
	info.PriceData = types.PriceData{
		OtherRatios: map[string]float64{"seconds": 30},
	}

	suppressRouteTaskRatios(c, info)

	assert.Equal(t, map[string]float64{"seconds": 30}, info.PriceData.OtherRatios,
		"没有标记时倍率必须原样保留（按秒商品依赖它）")
}

func TestSuppressRouteTaskRatiosNoopWithoutRatios(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newSuppressTestContext(t, true)
	info := &relaycommon.RelayInfo{}

	// 不应 panic，也不应写入空 map（保持「没有倍率」的语义）。
	suppressRouteTaskRatios(c, info)
	assert.Nil(t, info.PriceData.OtherRatios)

	// nil 入参安全：路由层被其它入口复用时不能崩。
	suppressRouteTaskRatios(c, nil)
	suppressRouteTaskRatios(nil, info)
}
