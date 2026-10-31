package doubao

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// metadataVideoURLs 必须同时认两种写法：
//   - 官方契约 {"type":"video_url","video_url":{"url":"..."}}
//   - 客户端手搓 {"type":"video_url","video_url":"https://..."}
//
// 只认一种会漏掉一半流量，15 秒闸门等于没装。
func TestMetadataVideoURLsBothShapes(t *testing.T) {
	metadata := map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "把这段视频改成夜景"},
			map[string]interface{}{
				"type":      "video_url",
				"video_url": map[string]interface{}{"url": "https://cdn.example/a.mp4"},
			},
			map[string]interface{}{
				"type":      "video_url",
				"video_url": "https://cdn.example/b.mp4",
			},
			map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": "https://cdn.example/c.png"},
			},
		},
	}

	require.Equal(t, []string{"https://cdn.example/a.mp4", "https://cdn.example/b.mp4"},
		metadataVideoURLs(metadata))
}

func TestMetadataVideoURLsIgnoresNonVideoAndEmpty(t *testing.T) {
	require.Empty(t, metadataVideoURLs(nil))
	require.Empty(t, metadataVideoURLs(map[string]interface{}{}))

	// content 里全是图片/文本
	require.Empty(t, metadataVideoURLs(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "hi"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": "https://e.com/i.png"}},
		},
	}))

	// video_url 存在但地址为空：不能当成一条待探测的视频
	require.Empty(t, metadataVideoURLs(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "video_url", "video_url": map[string]interface{}{"url": "  "}},
			map[string]interface{}{"type": "video_url", "video_url": ""},
			map[string]interface{}{"type": "video_url"},
		},
	}))
}

// content 不是数组（客户端传了对象或字符串）时不能 panic。
func TestMetadataVideoURLsToleratesWrongTypes(t *testing.T) {
	require.Empty(t, metadataVideoURLs(map[string]interface{}{"content": "not-an-array"}))
	require.Empty(t, metadataVideoURLs(map[string]interface{}{
		"content": []interface{}{"string-item", 42, nil},
	}))
	require.Empty(t, metadataVideoURLs(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "video_url", "video_url": 42},
		},
	}))
}

// 探测失败必须放行：假域名解析不了，不能因此拒单。
//
// 这条锁的是「闸门只拦实测超长」这个口径 —— 客户的素材地址可能只对上游可达。
func TestCheckMetadataReferenceVideoSecondsPassesUnreachable(t *testing.T) {
	require.NoError(t, checkMetadataReferenceVideoSeconds(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{
				"type":      "video_url",
				"video_url": map[string]interface{}{"url": "https://a.example/x.mp4"},
			},
		},
	}))
}

// 没有参考视频时不应该产生任何网络动作。
func TestCheckMetadataReferenceVideoSecondsNoVideo(t *testing.T) {
	require.NoError(t, checkMetadataReferenceVideoSeconds(nil))
	require.NoError(t, checkMetadataReferenceVideoSeconds(map[string]interface{}{
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": "纯文生视频"},
		},
	}))
}
