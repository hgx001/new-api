package seedanceroute_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 两个换货开关表达同一件事（官网商品由谁跑），必须互斥。
// 若允许同时为 true，一条请求会被两个中间件先后改写，计费原点与货源都会错。
func TestWan3GuanwangSwitchExclusive(t *testing.T) {
	enabledXindalu := WithWan3GuanwangXindaluEnabled(true)
	assert.True(t, enabledXindalu.Wan3GuanwangXindaluEnabled)
	assert.False(t, enabledXindalu.Wan3GuanwangTJEnabled, "打开新大陆必须关掉客美")

	enabledKemeitv := WithWan3GuanwangTjEnabled(true)
	assert.True(t, enabledKemeitv.Wan3GuanwangTJEnabled)
	assert.False(t, enabledKemeitv.Wan3GuanwangXindaluEnabled, "打开客美必须关掉新大陆")

	disabled := WithWan3GuanwangXindaluEnabled(false)
	assert.False(t, disabled.Wan3GuanwangXindaluEnabled)
}
