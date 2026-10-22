package common

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seconds 必须同时接受字符串与数字两种写法。
//
// WHY：Sora 协议里 seconds 是字符串（"4"/"8"），但大量第三方客户端直接传数字
// （{"seconds": 15}）。以前这里整包反序列化失败 → 客户端收到 400 invalid_json，
// 表现为「参数根本没传进去」，而不是「参数写错了」——排查时极难定位。
// 字符串是标准写法，行为完全不变；这里只是把数字也接住。
func TestTaskSubmitReqAcceptsSecondsAsStringOrNumber(t *testing.T) {
	var stringSeconds TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"seconds":"15"}`), &stringSeconds))
	assert.Equal(t, "15", stringSeconds.Seconds)

	var numberSeconds TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"seconds":15}`), &numberSeconds))
	assert.Equal(t, "15", numberSeconds.Seconds, "数字写法必须与字符串写法等价")

	var omitted TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{}`), &omitted))
	assert.Empty(t, omitted.Seconds)

	var nullSeconds TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"seconds":null}`), &nullSeconds))
	assert.Empty(t, nullSeconds.Seconds)

	// 非整数的数字没有意义（时长按秒计），直接报错而不是静默丢弃。
	var fractional TaskSubmitReq
	require.ErrorContains(t, common.Unmarshal([]byte(`{"seconds":15.5}`), &fractional), "seconds must be an integer")
}
