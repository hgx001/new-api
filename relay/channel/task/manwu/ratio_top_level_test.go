package manwu

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 2026-11-10 审查发现：manwu（type=66，生产在售渠道 20/30）的 clientRequest
// **没有 Metadata 字段**，resolveRatio 只读 size → ratio → aspect_ratio。
//
// 后果：
//  1. 客户端用站内已验证的传法 metadata.ratio = "9:16" → **被完全丢弃**，
//     落到默认 16:9 → 用户要的竖屏出成横屏；
//  2. 各分支（豆包视频 / 豆包图片 / Picsart）各写一遍 firstNonEmpty，
//     口径和其余 7 个适配器完全不一致。
//
// 修复方向：接入全站统一入口 relaycommon.ResolveTaskRatio，口径唯一。

// 元数据比例必须被承接 —— 这是站内已验证的传法。
func TestResolveRatioReadsMetadataRatio(t *testing.T) {
	req := clientRequest{
		Metadata: map[string]any{"ratio": "9:16"},
	}
	ratio, err := resolveRatio(req)
	require.NoError(t, err)
	require.Equal(t, "9:16", ratio)
}

func TestResolveRatioReadsMetadataAspectRatio(t *testing.T) {
	req := clientRequest{
		Metadata: map[string]any{"aspect_ratio": "9:16"},
	}
	ratio, err := resolveRatio(req)
	require.NoError(t, err)
	require.Equal(t, "9:16", ratio)
}

// metadata 优先于顶层（站内已验证路径保持最高优先级）。
func TestResolveRatioMetadataWinsOverTopLevel(t *testing.T) {
	req := clientRequest{
		Ratio:    "16:9",
		Metadata: map[string]any{"ratio": "9:16"},
	}
	ratio, err := resolveRatio(req)
	require.NoError(t, err)
	require.Equal(t, "9:16", ratio)
}

// 顶层 ratio 优先于顶层 aspect_ratio。
func TestResolveRatioTopLevelRatioWinsOverAspectRatio(t *testing.T) {
	req := clientRequest{Ratio: "9:16", AspectRatio: "16:9"}
	ratio, err := resolveRatio(req)
	require.NoError(t, err)
	require.Equal(t, "9:16", ratio)
}

// 顶层 aspect_ratio 单独传也要生效。
func TestResolveRatioTopLevelAspectRatio(t *testing.T) {
	req := clientRequest{AspectRatio: "9:16"}
	ratio, err := resolveRatio(req)
	require.NoError(t, err)
	require.Equal(t, "9:16", ratio)
}

// 非法比例必须报错（manwu 原口径：比例是硬约束，不静默降级）。
func TestResolveRatioRejectsInvalid(t *testing.T) {
	req := clientRequest{Ratio: "99:1"}
	_, err := resolveRatio(req)
	require.Error(t, err)
}

// 什么都没传 → 缺省 16:9。
func TestResolveRatioDefault(t *testing.T) {
	req := clientRequest{}
	ratio, err := resolveRatio(req)
	require.NoError(t, err)
	require.Equal(t, "16:9", ratio)
}

// 豆包系图片路线：metadata 比例必须被承接，并归一到官网档位。
func TestResolveRawRatioSeedreamImageReadsMetadata(t *testing.T) {
	req := clientRequest{
		Metadata: map[string]any{"ratio": "9:16"},
	}
	ratio, err := resolveRawRatio(req, seedreamImageRatioCandidates(), normalizeSeedreamImageRatio)
	require.NoError(t, err)
	require.Equal(t, "9:16", ratio)
}

// 豆包系图片路线：「自动」档的各种写法都要归一到「自动」。
func TestResolveRawRatioSeedreamImageNormalizesAuto(t *testing.T) {
	req := clientRequest{Metadata: map[string]any{"ratio": "auto"}}
	ratio, err := resolveRawRatio(req, seedreamImageRatioCandidates(), normalizeSeedreamImageRatio)
	require.NoError(t, err)
	require.Equal(t, "自动", ratio)
}

// 豆包系图片路线：非法档位必须报错。
func TestResolveRawRatioSeedreamImageRejectsInvalid(t *testing.T) {
	req := clientRequest{Metadata: map[string]any{"ratio": "99:1"}}
	_, err := resolveRawRatio(req, seedreamImageRatioCandidates(), normalizeSeedreamImageRatio)
	require.Error(t, err)
}

// Picsart 路线：metadata 比例必须被承接。
func TestResolveRawRatioPicsartReadsMetadata(t *testing.T) {
	req := clientRequest{
		Metadata: map[string]any{"ratio": "9:16"},
	}
	ratio, err := resolveRawRatio(req, picsartRatioCandidates(), normalizePicsartRatio)
	require.NoError(t, err)
	require.Equal(t, "9:16", ratio)
}

// Picsart 路线：没指定比例时返回空串（不补默认值，官网用 adaptive）。
func TestResolveRawRatioAllowsEmpty(t *testing.T) {
	req := clientRequest{}
	ratio, err := resolveRawRatio(req, picsartRatioCandidates(), normalizePicsartRatio)
	require.NoError(t, err)
	require.Equal(t, "", ratio)
}

// 豆包视频路线：auto 档原样透传。
func TestResolveRawRatioDoubaoKeepsAuto(t *testing.T) {
	req := clientRequest{Metadata: map[string]any{"ratio": "auto"}}
	ratio, err := resolveRawRatio(req, doubaoRatioCandidates(), nil)
	require.NoError(t, err)
	require.Equal(t, "auto", ratio)
}
