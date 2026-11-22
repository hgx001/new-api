package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 本站在售模型的供应商归类契约表。
//
// 这张表是**生产口径**：模型名 → 供应商。改动任何一条之前先确认
// 「模型到底是谁家的」——归类错会让模型广场的供应商筛选失真。
//
// 参考口径：https://chongcheapi.com/pricing 的供应商分组（同款 new-api，
// 人家已经把 DeepSeek / Google / MiniMax / 字节跳动 分好了，我们这边补齐
// 视频与图片模型的归类）。
func TestClassifyModelVendor_在售模型契约表(t *testing.T) {
	cases := []struct {
		model  string
		vendor string
	}{
		// 字节跳动（即梦 / 豆包 / Seedance / Seedream）
		{"seedance-2.0", "字节跳动"},
		{"seedance-2-5", "字节跳动"},
		{"seedance-2-0-fast", "字节跳动"},
		{"cp-seedance-2.0", "字节跳动"},
		{"cp-seedance-2.0fast", "字节跳动"},
		{"cp-seedance-2.0mini", "字节跳动"},
		{"cp-seedance-2.5", "字节跳动"},
		{"cp-seedance-2.5flash", "字节跳动"},
		{"el-seedance-2.0mini", "字节跳动"},
		{"el-seedance-2.5", "字节跳动"},
		{"seedance2.5-特惠", "字节跳动"},
		{"seedance2.5-活动", "字节跳动"},
		{"jimeng-video-reverse", "字节跳动"},
		{"Seedream 4.5", "字节跳动"},
		{"Seedream 5.0 Flash", "字节跳动"},
		{"doubao-pro-32k", "字节跳动"},

		// 阿里巴巴（万相 Wan 系 + 通义）
		{"wan3.0-video", "阿里巴巴"},
		{"wan3.0-video-官网", "阿里巴巴"},
		{"wan3.0-video-tj", "阿里巴巴"},
		{"wan3.0-video-prime-tj", "阿里巴巴"},
		{"wan3.0-video-prime", "阿里巴巴"},
		{"wan3.0-720p-官网", "阿里巴巴"},
		{"wan3.0-720p-特惠", "阿里巴巴"},
		{"wan3.0-720p-按次", "阿里巴巴"},
		{"wan3.0-720p-特惠-内链", "阿里巴巴"},
		{"wan3.0-smart", "阿里巴巴"},
		{"wan3.0-班车", "阿里巴巴"},
		{"hk-wan3", "阿里巴巴"},
		{"wan-3.0-pro", "阿里巴巴"},
		{"wan-3.0-yz", "阿里巴巴"},
		{"wan2.7-编辑", "阿里巴巴"},
		{"wan2.7-图生", "阿里巴巴"},
		{"qwen3.8-max", "阿里巴巴"},

		// MiniMax
		{"minimax_h3_omni", "MiniMax"},
		{"minimax_h3-文生", "MiniMax"},
		{"minimax_h3-图生", "MiniMax"},
		{"minimax_h3_v2", "MiniMax"},
		{"h3-image", "MiniMax"},
		{"MiniMax-M2.7", "MiniMax"},
		{"MiniMax-M3", "MiniMax"},
		{"MiniMax-M3.1", "MiniMax"},
		{"abab6.5-chat", "MiniMax"},

		// Google
		{"Nano Banana Pro", "Google"},
		{"gemini-web-video", "Google"},
		{"gemini-3-pro-preview", "Google"},
		{"el-gemini-1.2flash", "Google"},
		{"veo-3.1-pro", "Google"},

		// OpenAI / Anthropic / 其它
		{"gpt-6-luna", "OpenAI"},
		{"gpt-6.1-sol", "OpenAI"},
		{"gpt-5.6-sol", "OpenAI"},
		{"gpt-image-2.5-官方", "OpenAI"},
		{"o1-preview", "OpenAI"},
		{"o3-mini", "OpenAI"},
		{"claude-opus-4-8", "Anthropic"},
		{"kimi-k3", "Moonshot"},
		{"glm-5.3-flash", "智谱"},
		{"deepseek-v4-pro", "DeepSeek"},
		{"ernie-4.5-turbo", "百度"},
		{"spark-max", "讯飞"},
		{"hunyuan-turbos", "腾讯"},
		{"grok-4.6", "xAI"},
		{"llama-4-maverick", "Meta"},
		{"mistral-large-3", "Mistral"},
		{"kling-v2-turbo", "快手"},
		{"vidu-q1", "Vidu"},
		{"yi-lightning", "零一万物"},
	}

	for _, c := range cases {
		vendor, ok := classifyModelVendor(c.model)
		assert.True(t, ok, "模型 %s 应当能归类", c.model)
		assert.Equal(t, c.vendor, vendor, "模型 %s 的供应商", c.model)
	}
}

func TestClassifyModelVendor_未知模型不硬猜(t *testing.T) {
	for _, name := range []string{"", "   ", "some-random-model", "totally-unknown-xyz"} {
		vendor, ok := classifyModelVendor(name)
		assert.False(t, ok, "模型 %q 不该被硬猜成供应商", name)
		assert.Empty(t, vendor)
	}
}

// 规则表是有序的：短片段必须用前缀匹配，否则会误伤别的模型。
func TestClassifyModelVendor_短片段不误伤(t *testing.T) {
	// "yi" 只在开头才算零一万物；"tongyi" 是通义（阿里巴巴），不能按 "yi" 算
	vendor, ok := classifyModelVendor("tongyi-vision")
	assert.True(t, ok)
	assert.Equal(t, "阿里巴巴", vendor)

	vendor, ok = classifyModelVendor("yi-lightning")
	assert.True(t, ok)
	assert.Equal(t, "零一万物", vendor)

	// "o3" 不能命中 claude-3-opus 这类模型名
	vendor, ok = classifyModelVendor("claude-3-opus")
	assert.True(t, ok)
	assert.Equal(t, "Anthropic", vendor)

	// "360" 不应命中含 360 的无关模型名之外的特例
	vendor, ok = classifyModelVendor("gpt-360-vision")
	assert.True(t, ok)
	assert.Equal(t, "OpenAI", vendor)
}

// 同一个模型名每次判定结果必须一致（旧版 map 规则是随机顺序，不可复现）。
func TestClassifyModelVendor_判定可复现(t *testing.T) {
	name := "minimax-m3.1"
	first, _ := classifyModelVendor(name)
	for i := 0; i < 50; i++ {
		got, _ := classifyModelVendor(name)
		assert.Equal(t, first, got, "同一模型名的归类必须稳定")
	}
}
