package cosdirect_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 每个用例自己把全局配置恢复干净，避免跨用例串味。
func reset(t *testing.T) {
	t.Helper()
	require.NoError(t, UpdateFromJSONString("{}"))
}

func TestUpdateFromJSONStringFullConfig(t *testing.T) {
	reset(t)
	err := UpdateFromJSONString(`{"enabled":true,"secret_id":"ak","secret_key":"sk","region":"ap-guangzhou","bucket":"hbd-video-1256715892","output_dir":"/videos/"}`)
	require.NoError(t, err)

	cfg := Get()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "ak", cfg.SecretId)
	assert.Equal(t, "sk", cfg.SecretKey)
	assert.Equal(t, "ap-guangzhou", cfg.Region)
	assert.Equal(t, "hbd-video-1256715892", cfg.Bucket)
	assert.Equal(t, "/videos/", cfg.OutputDir)
}

// 部分更新：只改开关不应清空密钥（管理员改小项时不该把链路搞断）。
func TestUpdateFromJSONStringPartialKeepsSecrets(t *testing.T) {
	reset(t)
	require.NoError(t, UpdateFromJSONString(`{"enabled":true,"secret_id":"ak","secret_key":"sk","bucket":"b1"}`))
	require.NoError(t, UpdateFromJSONString(`{"enabled":false}`))

	cfg := Get()
	assert.False(t, cfg.Enabled)
	assert.Equal(t, "ak", cfg.SecretId)
	assert.Equal(t, "sk", cfg.SecretKey)
	assert.Equal(t, "b1", cfg.Bucket)
}

// Region 默认 ap-guangzhou；未显式配置时不应为空。
func TestDefaultRegion(t *testing.T) {
	reset(t)
	assert.Equal(t, "ap-guangzhou", Get().Region)
}

func TestIsConfigured(t *testing.T) {
	reset(t)
	assert.False(t, IsConfigured(), "未配置时必须不可用")

	// enabled=false 仍不可用（宁可不做后处理也不提交注定失败的任务）。
	require.NoError(t, UpdateFromJSONString(`{"enabled":false,"secret_id":"ak","secret_key":"sk","bucket":"b1"}`))
	assert.False(t, IsConfigured())

	require.NoError(t, UpdateFromJSONString(`{"enabled":true,"secret_id":"ak","secret_key":"sk","bucket":"b1"}`))
	assert.True(t, IsConfigured())

	// 部分更新语义下空密钥=保留旧值（不可构造「缺密钥」），但全新重置后
	// enabled=true 而无任何密钥的完整 JSON 也无法达成——空密钥会回填旧值。
	// 这里只验证重置后的干净状态不可用。
	reset(t)
	require.NoError(t, UpdateFromJSONString(`{"bucket":"only-bucket"}`))
	assert.False(t, IsConfigured())
}

func TestMarshalJSONStringRoundTrip(t *testing.T) {
	reset(t)
	require.NoError(t, UpdateFromJSONString(`{"enabled":true,"secret_id":"ak","secret_key":"sk","bucket":"b1"}`))

	encoded := MarshalJSONString()
	require.NoError(t, UpdateFromJSONString(encoded))
	cfg := Get()
	assert.True(t, cfg.Enabled)
	assert.Equal(t, "ak", cfg.SecretId)
	assert.Equal(t, "b1", cfg.Bucket)
}

// 非法 JSON 必须报错且不改动现有配置。
func TestUpdateFromJSONStringInvalidJSON(t *testing.T) {
	reset(t)
	require.NoError(t, UpdateFromJSONString(`{"bucket":"keep"}`))
	err := UpdateFromJSONString(`{invalid`)
	require.Error(t, err)
	assert.Equal(t, "keep", Get().Bucket)
}
