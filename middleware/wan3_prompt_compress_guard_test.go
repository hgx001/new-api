package middleware

import (
	"strings"
	"testing"
)

// 压缩结果四条硬不变量的测试。
//
// 这些不变量是「压缩」这件事唯一的安全边界：模型改写的是客户的创作描述，
// 一旦丢东西，成片与提示词不符**而且任务照样成功、账单照收** ——
// 没有任何下游信号能发现，只能在改写的入口挡住。

// 引用标记的多重集必须完全一致（少一个 = 少一张素材，多一个 = 凭空引用）。
func TestValidateCompressedPromptCitationMarkers(t *testing.T) {
	cases := []struct {
		name       string
		original   string
		compressed string
		wantErr    bool
	}{
		{
			name:       "原样保留通过",
			original:   "@图片1 @图片2 一只橘猫在窗边打盹",
			compressed: "@图片1 @图片2 橘猫在窗边打盹，阳光缓慢移动",
		},
		{
			name:       "丢一个标记判失败",
			original:   "@图片1 @图片2 一只橘猫在窗边打盹",
			compressed: "@图片1 橘猫在窗边打盹，阳光缓慢移动",
			wantErr:    true,
		},
		{
			name:       "改了编号判失败",
			original:   "@图片1 @图片2 一只橘猫在窗边打盹",
			compressed: "@图片1 @图片3 橘猫在窗边打盹",
			wantErr:    true,
		},
		{
			name:       "重复两次压成一次判失败",
			original:   "@图片1 起手 @图片1 收尾 一只橘猫在窗边打盹",
			compressed: "@图片1 起手收尾，一只橘猫在窗边打盹",
			wantErr:    true,
		},
		{
			name:       "原文没有标记就不校验",
			original:   "一只橘猫在窗边打盹，阳光缓慢移动",
			compressed: "橘猫在窗边打盹，阳光缓慢移动",
		},
		{
			name:       "英文 image 大小写视为同一标记",
			original:   "@Image2 一只橘猫在窗边打盹",
			compressed: "@image2 橘猫在窗边打盹，阳光缓慢移动",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateCitationMarkers(testCase.original, testCase.compressed)
			if testCase.wantErr && err == nil {
				t.Fatal("期望校验失败，实际通过")
			}
			if !testCase.wantErr && err != nil {
				t.Fatalf("期望通过，实际失败：%v", err)
			}
		})
	}
}

// 负向尾坠必须逐字保留：压掉「禁止出现：字幕」就会冒出字幕。
func TestValidateCompressedPromptNegativeTail(t *testing.T) {
	original := "一只橘猫在窗边打盹，镜头轻微推进，暖色调。禁止出现：BGM、文字字幕、水印。"
	keep := "橘猫在窗边打盹，暖色调。禁止出现：BGM、文字字幕、水印。"
	lose := "橘猫在窗边打盹，暖色调。"

	if err := validateNegativeTail(original, keep); err != nil {
		t.Fatalf("尾坠保留却判失败：%v", err)
	}
	if err := validateNegativeTail(original, lose); err == nil {
		t.Fatal("尾坠被删却通过了校验")
	}

	// 原文没有尾坠时不要求 —— 非 ArcReel 客户不会写这句。
	if err := validateNegativeTail("一只橘猫在窗边打盹", "橘猫打盹"); err != nil {
		t.Fatalf("原文没有负向约束，不该校验：%v", err)
	}
}

// 保真下限：过度摘要（只剩 20%）必须判失败。
func TestValidateCompressedPromptFidelity(t *testing.T) {
	original := strings.Repeat("细节", 3000) // 6000 字符
	if err := validateCompressedPrompt(original, strings.Repeat("细节", 1200)); err != nil {
		t.Fatalf("压到 40%% 应通过：%v", err)
	}
	tooShort := strings.Repeat("细节", 100) // 200 字符，约 3%
	if err := validateCompressedPrompt(original, tooShort); err == nil {
		t.Fatal("只剩 3% 却通过了校验（六要素必然已被压没）")
	}
}

// 元话语前缀必须判失败：那是模型的解释，不是提示词正文。
func TestValidateCompressedPromptRejectsMetaPrefix(t *testing.T) {
	original := strings.Repeat("细节", 3000)
	prefixed := "以下是压缩后的提示词：" + strings.Repeat("细节", 2000)
	if err := validateCompressedPrompt(original, prefixed); err == nil {
		t.Fatal("带「以下是压缩后的提示词」前缀却通过了校验")
	}
}

// 超过 5000 的压缩结果必须判失败（哪怕其它不变量都满足）。
func TestValidateCompressedPromptRejectsOverLimit(t *testing.T) {
	original := strings.Repeat("字", 9000)
	if err := validateCompressedPrompt(original, strings.Repeat("字", promptRouteLimitRunes+1)); err == nil {
		t.Fatal("压缩后仍超 5000 却通过了校验")
	}
}

// 清洗：代码围栏、首尾引号、空白。
func TestCleanCompressedOutput(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"  正文内容  ", "正文内容"},
		{"```text\n正文内容\n```", "正文内容"},
		{"```\n正文内容\n```", "正文内容"},
		{"「正文内容」", "正文内容"},
		{"“正文内容”", "正文内容"},
	}
	for _, testCase := range cases {
		if got := cleanCompressedOutput(testCase.raw); got != testCase.want {
			t.Fatalf("cleanCompressedOutput(%q) = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}

// 计数口径：按 Unicode 字符，不按字节。
func TestCountRunesNotBytes(t *testing.T) {
	if got := countRunes(strings.Repeat("字", promptRouteLimitRunes)); got != promptRouteLimitRunes {
		t.Fatalf("countRunes = %d, want %d", got, promptRouteLimitRunes)
	}
}
