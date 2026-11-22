package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 标签契约表：模型名 → 媒体标签（video / image）。
// 口径与供应商归类一致（见 pricing_default_test.go）。
func TestClassifyModelTag_媒体标签契约表(t *testing.T) {
	cases := []struct {
		model string
		tag   string
	}{
		// 视频
		{"wan3.0-video", "video"},
		{"wan3.0-video-官网", "video"},
		{"wan3.0-video-tj", "video"},
		{"wan3.0-video-prime-tj", "video"},
		{"wan3.0-video-prime", "video"},
		{"wan3.0-720p-官网", "video"},
		{"wan3.0-720p-特惠", "video"},
		{"wan3.0-720p-按次", "video"},
		{"wan3.0-smart", "video"},
		{"wan3.0-班车", "video"},
		{"hk-wan3", "video"},
		{"wan-3.0-pro", "video"},
		{"wan-3.0-yz", "video"},
		{"wan2.7-编辑", "video"},
		{"wan2.7-图生", "video"},
		{"seedance-2.0", "video"},
		{"seedance-2-5", "video"},
		{"seedance-2-0-fast", "video"},
		{"cp-seedance-2.0", "video"},
		{"cp-seedance-2.5flash", "video"},
		{"el-seedance-2.5", "video"},
		{"seedance2.5-特惠", "video"},
		{"seedance2.5-活动", "video"},
		{"jimeng-video-reverse", "video"},
		{"minimax_h3_omni", "video"},
		{"minimax_h3-文生", "video"},
		{"minimax_h3-图生", "video"},
		{"gemini-web-video", "video"},

		// 图片
		{"Nano Banana Pro", "image"},
		{"Seedream 4.5", "image"},
		{"Seedream 5.0 Flash", "image"},
		{"h3-image", "image"},
		{"gpt-image-2.5-官方", "image"},
	}
	for _, c := range cases {
		tag, isText := classifyModelTag(c.model)
		assert.False(t, isText, "模型 %s 不该被判成文本模型", c.model)
		assert.Equal(t, c.tag, tag, "模型 %s 的标签", c.model)
	}
}

func TestClassifyModelTag_文本模型不带媒体标签(t *testing.T) {
	for _, name := range []string{
		"gpt-6-luna", "gpt-6-sol", "gpt-6.1-sol", "gpt-5.6-sol",
		"claude-opus-4-8", "kimi-k3", "glm-5.3-flash", "deepseek-v4-pro",
		"qwen3.8-max", "MiniMax-M2.7", "MiniMax-M3.1", "el-gemini-1.2flash",
	} {
		tag, isText := classifyModelTag(name)
		assert.True(t, isText, "模型 %s 应判为文本模型", name)
		assert.Empty(t, tag, "模型 %s 不该带媒体标签", name)
	}
}

func TestClassifyModelTag_判不了的模型不瞎猜(t *testing.T) {
	for _, name := range []string{"", "some-random-model", "totally-unknown-xyz"} {
		tag, isText := classifyModelTag(name)
		assert.False(t, isText)
		assert.Empty(t, tag)
	}
}

func TestApplyModelTag_补齐缺失标签(t *testing.T) {
	tags, changed := applyModelTag("", "wan3.0-video-官网")
	assert.True(t, changed)
	assert.Equal(t, "video", tags)

	tags, changed = applyModelTag("", "Seedream 4.5")
	assert.True(t, changed)
	assert.Equal(t, "image", tags)
}

func TestApplyModelTag_纠正对话模型上的媒体标签(t *testing.T) {
	// 生产实况：gpt-6-* 这三个对话模型被挂成了 image
	tags, changed := applyModelTag("image", "gpt-6-luna")
	assert.True(t, changed)
	assert.Empty(t, tags)
}

func TestApplyModelTag_保留管理员写的非媒体标签(t *testing.T) {
	tags, changed := applyModelTag("推理,工具,1M", "deepseek-v4-pro")
	assert.False(t, changed, "文本模型的非媒体标签不该被动")
	assert.Equal(t, "推理,工具,1M", tags)

	// 媒体模型：媒体标签已经正确时不动，其它标签保留
	tags, changed = applyModelTag("image,推理", "Seedream 4.5")
	assert.False(t, changed, "媒体标签已正确就不该再改")
	assert.Equal(t, "image,推理", tags)

	// 媒体标签写错了才纠正，别的一律保留
	tags, changed = applyModelTag("video,推理", "Seedream 4.5")
	assert.True(t, changed)
	assert.Equal(t, "image,推理", tags)
}

func TestApplyModelTag_幂等(t *testing.T) {
	for _, name := range []string{"wan3.0-video", "Nano Banana Pro", "gpt-6-sol", "totally-unknown"} {
		first, changed := applyModelTag("", name)
		if !changed {
			continue
		}
		second, changed2 := applyModelTag(first, name)
		assert.False(t, changed2, "模型 %s 第二次同步不应再改", name)
		assert.Equal(t, first, second)
	}
}

func TestSyncModelTags_落库行为(t *testing.T) {
	setupVendorDB(t)

	require.NoError(t, DB.Create(&Model{ModelName: "wan3.0-video-官网", Status: 1}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "Nano Banana Pro", Status: 1, Tags: "image"}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "gpt-6-luna", Status: 1, Tags: "image"}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "deepseek-v4-pro", Status: 1, Tags: "推理,工具"}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "totally-unknown", Status: 1}).Error)

	updated, err := SyncModelTags()
	require.NoError(t, err)
	assert.Equal(t, 2, updated, "只改 wan3（补标签）与 gpt-6-luna（摘媒体标签）")

	var wan Model
	require.NoError(t, DB.Where("model_name = ?", "wan3.0-video-官网").First(&wan).Error)
	assert.Equal(t, "video", wan.Tags)

	var gpt Model
	require.NoError(t, DB.Where("model_name = ?", "gpt-6-luna").First(&gpt).Error)
	assert.Empty(t, gpt.Tags)

	var ds Model
	require.NoError(t, DB.Where("model_name = ?", "deepseek-v4-pro").First(&ds).Error)
	assert.Equal(t, "推理,工具", ds.Tags, "非媒体标签不动")

	var unknown Model
	require.NoError(t, DB.Where("model_name = ?", "totally-unknown").First(&unknown).Error)
	assert.Empty(t, unknown.Tags, "判不了的模型不瞎贴标签")

	// 幂等
	updated, err = SyncModelTags()
	require.NoError(t, err)
	assert.Equal(t, 0, updated)
}
