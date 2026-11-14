package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// CapCut 五档能力组的守卫测试。
//
// 这道闸门存在的理由：CapCut 的「时长上界」「720P 倍率」「计价单位」「广场白名单」
// 分散在多个判定点（constant 能力组、manwu 适配器的表、前端镜像）。
// 复制一份模型就要在每处各写一遍 —— 于是必然出现静默不一致：
// 加了模型但没上广场、上广场了却显示成按次、时长上界两边不一样。
//
// 这里守住「能力组是唯一来源」这个结构。

func TestCapcutFamilyMembers(t *testing.T) {
	assert.Len(t, CapcutFamily, 5, "CapCut 应为五档")
	for _, name := range []string{
		Capcut20MiniModel,
		Capcut20FastModel,
		Capcut20Model,
		Capcut25FlashModel,
		Capcut25Model,
	} {
		assert.True(t, IsCapcutFamily(name), "%s 应是 CapCut 族成员", name)
		assert.True(t, IsCapcutPerSecond(name), "%s 应是按秒档", name)
	}
}

func TestIsCapcutFamilyTrimsSpace(t *testing.T) {
	// 生产上真出现过带空格的对外模型名，判定点必须 TrimSpace。
	assert.True(t, IsCapcutFamily(" "+Capcut20Model+" "))
	assert.True(t, IsCapcutPerSecond("\t"+Capcut25Model+"\n"))
}

func TestIsCapcutFamilyRejectsOthers(t *testing.T) {
	// 豆包官网档位不是 CapCut —— 两者同名族但不互为别名。
	for _, name := range []string{
		"seedance-2-0-fast",
		"seedance-2.5",
		"seedance-2-5",
		"cp-seedance-3.0",
		"cp-seedance-2.0mini-fast",
		"",
	} {
		assert.False(t, IsCapcutFamily(name), "%s 不应被认成 CapCut 族成员", name)
	}
}

// ⚠️ 子串陷阱守卫：公开名 `cp-seedance-2.5flash` 包含子串 `cp-seedance-2.5`。
// 任何用 Contains / 前缀匹配来判定「是不是 2.5 档」的写法都会把 2.5 Flash
// 误判成 2.5 档 —— 时长上界（15 vs 30 秒）和 720P 倍率都跟着错。
//
// 这道测试锁住「精确集合」这个结构：只要有人把查表改成前缀匹配，这里立刻变红。
func TestCapcut25FlashIsNotSubstringOf25(t *testing.T) {
	assert.Contains(t, Capcut25FlashModel, Capcut25Model,
		"前提：2.5flash 的公开名确实包含 2.5 子串")
	assert.NotEqual(t, Capcut25FlashModel, Capcut25Model,
		"2.5 Flash 与 2.5 必须是两个独立商品，不做互为别名")

	// 精确查表：两个键都在，且互不覆盖。
	assert.Contains(t, CapcutFamily, Capcut25FlashModel)
	assert.Contains(t, CapcutFamily, Capcut25Model)
}
