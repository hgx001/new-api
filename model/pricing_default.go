package model

import (
	"strings"
)

// vendorRule 是「模型名 → 供应商」的一条匹配规则。
//
// 两个字段二选一：
//   - pattern：模型名（小写）**包含**它即命中；
//   - prefix：模型名（小写）必须**以它开头**才命中（用于 "o1"/"o3"/"yi" 这类
//     短片段，用包含会把无关模型误伤）。
//
// ⚠️ 规则表是**有序**的，先命中先算 —— 顺序变了分类结果就变，别随手排序。
type vendorRule struct {
	pattern string
	prefix  string
	vendor  string
}

// defaultVendorRules 是默认供应商归类规则表。
//
// 为什么要重写：旧版是 map[string]string，Go 的 map 迭代顺序随机 ——
// 同一份模型名在不同进程里可能被归到不同供应商（"yi" 命中 "minimax-m3.1"、
// "o3" 命中 "claude-3-opus" 之类），这是**不可复现**的分类。
//
// 覆盖范围（本站在售模型的真实清单见 pricing_default_test.go 的契约表）：
//   - 字节跳动：即梦 / 豆包 / Seedance / Seedream 系
//   - 阿里巴巴：万相 Wan 系 + 通义 Qwen
//   - MiniMax：H3 / M 系 / abab
//   - Google：Gemini / Nano Banana / Veo
//   - OpenAI / Anthropic / Moonshot / 智谱 / DeepSeek 等
//
// 归类口径是**模型出身**，不是我们用哪条渠道跑：模型广场的供应商筛选是给
// 客户看「这个模型是谁家的」，不是给运维看「我们从谁那儿进货」。
var defaultVendorRules = []vendorRule{
	// ── 字节跳动（即梦 / 豆包 / Seedance / Seedream）──
	{pattern: "seedream", vendor: "字节跳动"},
	{pattern: "seedance", vendor: "字节跳动"},
	{pattern: "doubao", vendor: "字节跳动"},
	{pattern: "jimeng", vendor: "字节跳动"},

	// ── 阿里巴巴（万相 Wan 系 + 通义 Qwen）──
	// 排在 "h3" 之前：万相模型名里也带 "3"，别让 MiniMax 规则抢走。
	{pattern: "wan3", vendor: "阿里巴巴"},
	{pattern: "wan-3.0", vendor: "阿里巴巴"},
	{pattern: "wan2.7", vendor: "阿里巴巴"},
	{pattern: "qwen", vendor: "阿里巴巴"},
	{pattern: "tongyi", vendor: "阿里巴巴"},

	// ── MiniMax（H3 / M 系 / abab）──
	{pattern: "minimax", vendor: "MiniMax"},
	{pattern: "h3", vendor: "MiniMax"},
	{pattern: "abab", vendor: "MiniMax"},

	// ── Google（Gemini / Nano Banana / Veo）──
	{pattern: "gemini", vendor: "Google"},
	{pattern: "nano banana", vendor: "Google"},
	{pattern: "veo", vendor: "Google"},

	// ── OpenAI ──
	{pattern: "gpt", vendor: "OpenAI"},
	{pattern: "dall-e", vendor: "OpenAI"},
	{pattern: "whisper", vendor: "OpenAI"},
	{pattern: "sora", vendor: "OpenAI"},

	// ── Anthropic ──
	{pattern: "claude", vendor: "Anthropic"},

	// ── 其它模型家族 ──
	{pattern: "moonshot", vendor: "Moonshot"},
	{pattern: "kimi", vendor: "Moonshot"},
	{pattern: "chatglm", vendor: "智谱"},
	{pattern: "glm-", vendor: "智谱"},
	{pattern: "deepseek", vendor: "DeepSeek"},
	{pattern: "ernie", vendor: "百度"},
	{pattern: "spark", vendor: "讯飞"},
	{pattern: "hunyuan", vendor: "腾讯"},
	{pattern: "command", vendor: "Cohere"},
	{pattern: "@cf/", vendor: "Cloudflare"},
	{pattern: "360", vendor: "360"},
	{pattern: "jina", vendor: "Jina"},
	{pattern: "mistral", vendor: "Mistral"},
	{pattern: "grok", vendor: "xAI"},
	{pattern: "llama", vendor: "Meta"},
	{pattern: "kling", vendor: "快手"},
	{pattern: "vidu", vendor: "Vidu"},

	// ── 短片段一律走前缀匹配 ──
	{prefix: "yi", vendor: "零一万物"},
	{prefix: "o1", vendor: "OpenAI"},
	{prefix: "o3", vendor: "OpenAI"},
}

// 供应商默认图标映射
var defaultVendorIcons = map[string]string{
	"OpenAI":     "OpenAI",
	"Anthropic":  "Claude.Color",
	"Google":     "Gemini.Color",
	"Moonshot":   "Moonshot",
	"智谱":         "Zhipu.Color",
	"阿里巴巴":       "Qwen.Color",
	"DeepSeek":   "DeepSeek.Color",
	"MiniMax":    "Minimax.Color",
	"百度":         "Wenxin.Color",
	"讯飞":         "Spark.Color",
	"腾讯":         "Hunyuan.Color",
	"Cohere":     "Cohere.Color",
	"Cloudflare": "Cloudflare.Color",
	"360":        "Ai360.Color",
	"零一万物":       "Yi.Color",
	"Jina":       "Jina",
	"Mistral":    "Mistral.Color",
	"xAI":        "XAI",
	"Meta":       "Ollama",
	"字节跳动":       "Doubao.Color",
	"快手":         "Kling.Color",
	"即梦":         "Jimeng.Color",
	"Vidu":       "Vidu",
	"微软":         "AzureAI",
	"Microsoft":  "AzureAI",
	"Azure":      "AzureAI",
}

// classifyModelVendor 按规则表判定模型属于哪个供应商。
//
// 第二个返回值为 false 表示**没有规则命中**（未知模型），调用方应保持
// 未归类状态，不要瞎猜 —— 猜错的供应商比不显示更糟。
func classifyModelVendor(modelName string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	if lower == "" {
		return "", false
	}
	for _, rule := range defaultVendorRules {
		if rule.prefix != "" {
			if strings.HasPrefix(lower, rule.prefix) {
				return rule.vendor, true
			}
			continue
		}
		if rule.pattern != "" && strings.Contains(lower, rule.pattern) {
			return rule.vendor, true
		}
	}
	return "", false
}

// initDefaultVendorMapping 简化的默认供应商映射
func initDefaultVendorMapping(metaMap map[string]*Model, vendorMap map[int]*Vendor, enableAbilities []AbilityWithChannel) {
	for _, ability := range enableAbilities {
		modelName := ability.Model
		if existing, exists := metaMap[modelName]; exists {
			// 已登记的模型：如果还没归类，用规则补上（先在内存里生效，落库由
			// SyncModelVendors 负责）。已经归类的**一律不动** —— 管理员手工
			// 指定的供应商不能被自动规则覆盖。
			if existing.VendorID == 0 {
				if vendorName, ok := classifyModelVendor(modelName); ok {
					existing.VendorID = getOrCreateVendor(vendorName, vendorMap)
				}
			}
			// 标签同理：没标签的按规则补上（媒体模型必须带 video/image，
			// 否则模型广场的标签筛选搜不到它）。已有的标签不动 ——
			// 写错/写漏的纠正交给 SyncModelTags 落库时处理。
			if strings.TrimSpace(existing.Tags) == "" {
				if tag, isText := classifyModelTag(modelName); tag != "" && !isText {
					existing.Tags = tag
				}
			}
			continue
		}

		// 匹配供应商
		vendorID := 0
		if vendorName, ok := classifyModelVendor(modelName); ok {
			vendorID = getOrCreateVendor(vendorName, vendorMap)
		}
		// 匹配标签
		tags := ""
		if tag, isText := classifyModelTag(modelName); tag != "" && !isText {
			tags = tag
		}

		// 创建模型元数据
		metaMap[modelName] = &Model{
			ModelName: modelName,
			VendorID:  vendorID,
			Tags:      tags,
			Status:    1,
			NameRule:  NameRuleExact,
		}
	}
}

// 查找或创建供应商
func getOrCreateVendor(vendorName string, vendorMap map[int]*Vendor) int {
	// 查找现有供应商
	for id, vendor := range vendorMap {
		if vendor.Name == vendorName {
			return id
		}
	}

	// 创建新供应商
	newVendor := &Vendor{
		Name:   vendorName,
		Status: 1,
		Icon:   getDefaultVendorIcon(vendorName),
	}

	if err := newVendor.Insert(); err != nil {
		return 0
	}

	vendorMap[newVendor.Id] = newVendor
	return newVendor.Id
}

// 获取供应商默认图标
func getDefaultVendorIcon(vendorName string) string {
	if icon, exists := defaultVendorIcons[vendorName]; exists {
		return icon
	}
	return ""
}
