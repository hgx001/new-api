package youzanwan3

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cache-buster 是整个修复的基石：CDN 会把 404 缓存住，而 arcmedia 的参考
// 素材 key 按内容 sha256 生成（同一张图永远是同一个 key），于是「key 在对象
// 还没落盘时被访问过」就会留下 404 负缓存，之后 put 进去了也拉不到。
//
// **只有换缓存键才能穿透负缓存** —— 裸 URL 再怎么预热也还是 404
// （2026-11-21 源站实测）。所以每次提交都要给素材 URL 加一个新键。
func TestAddReferenceCacheBuster(t *testing.T) {
	const stamp = int64(1763664000)

	t.Run("裸地址加参数", func(t *testing.T) {
		got := addReferenceCacheBuster("https://arcmedia.heibaidao.cn/p/a.jpg", stamp)
		assert.Equal(t, "https://arcmedia.heibaidao.cn/p/a.jpg?wz=1763664000", got)
	})

	t.Run("已有 query 时追加", func(t *testing.T) {
		got := addReferenceCacheBuster(
			"https://arcmedia.heibaidao.cn/p/a.jpg?imageMogr2/thumbnail/1280x1280%3E/quality/85", stamp)
		assert.Contains(t, got, "imageMogr2",
			"已有的 CI 参数必须原样保留 —— 丢了就等于没压缩")
		assert.Contains(t, got, "wz=1763664000")
	})

	t.Run("重复调用不叠加两个 wz=", func(t *testing.T) {
		once := addReferenceCacheBuster("https://arcmedia.heibaidao.cn/p/a.jpg", stamp)
		twice := addReferenceCacheBuster(once, stamp)
		assert.Equal(t, once, twice, "同一次提交里不该出现两个 wz=")
		assert.Equal(t, 1, countOccurrences(twice, "wz="))
	})

	t.Run("外部域名不动", func(t *testing.T) {
		raw := "https://example.com/a.jpg"
		assert.Equal(t, raw, addReferenceCacheBuster(raw, stamp))
	})

	t.Run("带签名不动（签名链多一个参数就失效）", func(t *testing.T) {
		raw := "https://arcmedia.heibaidao.cn/a.jpg?sign=abc"
		assert.Equal(t, raw, addReferenceCacheBuster(raw, stamp))
	})

	t.Run("空串与非法 URL 原样返回", func(t *testing.T) {
		assert.Equal(t, "", addReferenceCacheBuster("", stamp))
		assert.Equal(t, "://bad", addReferenceCacheBuster("://bad", stamp))
	})
}

// cache-buster 必须落到**请求体**里，两种素材形态都要覆盖。
func TestAddReferenceCacheBusterToBody(t *testing.T) {
	const stamp = int64(1763664000)

	t.Run("media 契约", func(t *testing.T) {
		body := []byte(`{"media":[{"type":"reference_image","url":"https://arcmedia.heibaidao.cn/a.jpg"},` +
			`{"type":"reference_audio","url":"https://arcmedia.heibaidao.cn/a.mp3"}]}`)
		out, ok := addReferenceCacheBusterToBody(body, stamp)
		require.True(t, ok)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(out, &payload))
		list := payload["media"].([]any)
		img := list[0].(map[string]any)
		aud := list[1].(map[string]any)
		assert.Contains(t, img["url"], "wz=1763664000", "参考图要加")
		assert.NotContains(t, aud["url"], "wz=", "音频不加（体积与回源特征不同）")
	})

	t.Run("顶层 images 契约", func(t *testing.T) {
		body := []byte(`{"images":["https://arcmedia.heibaidao.cn/a.jpg"],` +
			`"videos":["https://arcmedia.heibaidao.cn/v.mp4"]}`)
		out, ok := addReferenceCacheBusterToBody(body, stamp)
		require.True(t, ok)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(out, &payload))
		imgs := payload["images"].([]any)
		assert.Contains(t, imgs[0], "wz=1763664000")
		vids := payload["videos"].([]any)
		assert.NotContains(t, vids[0], "wz=")
	})

	t.Run("无参考图时不改写", func(t *testing.T) {
		body := []byte(`{"prompt":"hello"}`)
		out, ok := addReferenceCacheBusterToBody(body, stamp)
		assert.False(t, ok)
		assert.Equal(t, body, out)
	})

	t.Run("非法 JSON 原样返回", func(t *testing.T) {
		body := []byte(`not json`)
		out, ok := addReferenceCacheBusterToBody(body, stamp)
		assert.False(t, ok)
		assert.Equal(t, body, out)
	})
}

// cache-buster 改写后，收集 URL 必须能拿到**改写后的**那个地址 ——
// 预热必须和上游请求用同一条键。
func TestCacheBuster后收集到的URL带新参数(t *testing.T) {
	const stamp = int64(1763664000)
	body := []byte(`{"media":[{"type":"reference_image","url":"https://arcmedia.heibaidao.cn/a.jpg"}]}`)
	out, ok := addReferenceCacheBusterToBody(body, stamp)
	require.True(t, ok)
	urls := collectReferenceImageURLsFromBody(out)
	require.Len(t, urls, 1)
	assert.Contains(t, urls[0], "wz=1763664000")
}

func countOccurrences(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			n++
		}
	}
	return n
}
