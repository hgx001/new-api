package middleware

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 参考视频挡板的测试。
//
// 背景（生产实例 2026-10-31 01:10，logs id=9379）：
// 客户传了一段 32.39 秒的参考视频 + 9:16，被改派到 wan3.0-720p-特惠，
// 提交阶段直接 400 —— 而同一段素材走 16:9（不改派）本来能跑。
// 源链路（有赞）只限参考视频**数量**，wan3 却限单条 15 秒、总量 15 秒、
// 输入+输出 30 秒。挡板必须在**改派前**生效，否则客户白等一次还看不懂原因。

// ---- 最小 MP4 构造（与 wan3 包测试同款，这里只保留 v0 mvhd）----

func refTestBox(boxType string, payload []byte) []byte {
	out := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], boxType)
	return append(out, payload...)
}

func refTestMvhd(seconds float64) []byte {
	payload := []byte{0, 0, 0, 0}         // version 0 + flags
	payload = append(payload, 0, 0, 0, 1) // creation_time
	payload = append(payload, 0, 0, 0, 2) // modification_time
	var ts [4]byte
	binary.BigEndian.PutUint32(ts[:], 1000) // timescale = 1000
	payload = append(payload, ts[:]...)
	var d [4]byte
	binary.BigEndian.PutUint32(d[:], uint32(seconds*1000))
	payload = append(payload, d[:]...)
	tail := make([]byte, 80)
	binary.BigEndian.PutUint32(tail[0:4], 0x00010000)
	tail[4] = 0x01
	binary.BigEndian.PutUint32(tail[16:20], 0x00010000)
	binary.BigEndian.PutUint32(tail[32:36], 0x00010000)
	binary.BigEndian.PutUint32(tail[48:52], 0x40000000)
	binary.BigEndian.PutUint32(tail[76:80], 1)
	return refTestBox("mvhd", append(payload, tail...))
}

func refTestMP4(seconds float64) []byte {
	out := refTestBox("ftyp", []byte("isom\x00\x00\x02\x00"))
	out = append(out, refTestBox("mdat", make([]byte, 16))...)
	out = append(out, refTestBox("moov", refTestMvhd(seconds))...)
	return out
}

// refTestVideoDataURL 造一个「时长 = seconds」的 data URL 视频。
//
// 用 data URL 而不是 httptest 服务：探测走 data 分支不经过 SSRF 校验，
// 测试不需要为了本地地址去改 fetch 设置。
func refTestVideoDataURL(seconds float64) string {
	return "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(refTestMP4(seconds))
}

// refTestBody 组装一个带参考视频的请求体。
func refTestBody(seconds string, videoSeconds ...float64) map[string]interface{} {
	media := make([]interface{}, 0, len(videoSeconds))
	for _, sec := range videoSeconds {
		media = append(media, map[string]interface{}{
			"type": "reference_video",
			"url":  refTestVideoDataURL(sec),
		})
	}
	body := map[string]interface{}{
		"model":   testSourceModel,
		"seconds": seconds,
		"metadata": map[string]interface{}{
			"ratio": "9:16",
		},
	}
	if len(media) > 0 {
		body["media"] = media
	}
	return body
}

// ---- 单测：范围判定 ----

func TestWan3ReferenceVideosOutOfRange_NoReferenceVideo(t *testing.T) {
	body := refTestBody("5")
	out, reason := wan3ReferenceVideosOutOfRange(body)
	assert.False(t, out, "没有参考视频不该挡")
	assert.Empty(t, reason)
}

func TestWan3ReferenceVideosOutOfRange_ShortVideoPasses(t *testing.T) {
	// 10 秒参考 + 5 秒生成 = 15，单条与总量都在限内。
	body := refTestBody("5", 10)
	out, reason := wan3ReferenceVideosOutOfRange(body)
	assert.False(t, out, "10 秒参考视频 + 5 秒生成应当放行")
	assert.Empty(t, reason)
}

func TestWan3ReferenceVideosOutOfRange_SingleVideoOver15s(t *testing.T) {
	// 生产实例：32.39 秒的参考视频。
	body := refTestBody("5", 32.39)
	out, reason := wan3ReferenceVideosOutOfRange(body)
	assert.True(t, out, "单条超 15 秒必须挡住改派")
	assert.Contains(t, reason, "15")
}

func TestWan3ReferenceVideosOutOfRange_TotalOver15s(t *testing.T) {
	// 单条都合法（各 8 秒），但合计 16 秒超过官方 15 秒总量限制。
	body := refTestBody("5", 8, 8)
	out, reason := wan3ReferenceVideosOutOfRange(body)
	assert.True(t, out, "参考视频合计超 15 秒必须挡住改派")
	assert.Contains(t, reason, "16")
}

func TestWan3ReferenceVideosOutOfRange_GenerationPlusReferenceOver30s(t *testing.T) {
	// 参考视频 10 秒合规，但 10 + 25 = 35 超过「输入+输出 ≤30 秒」。
	body := refTestBody("25", 10)
	out, reason := wan3ReferenceVideosOutOfRange(body)
	assert.True(t, out, "参考视频 + 生成超 30 秒必须挡住改派")
	assert.Contains(t, reason, "30")
}

func TestWan3ReferenceVideosOutOfRange_Exactly30sPasses(t *testing.T) {
	// 10 + 20 = 30 正好压线，不能因为浮点尾巴被误判。
	body := refTestBody("20", 10)
	out, reason := wan3ReferenceVideosOutOfRange(body)
	assert.False(t, out, "刚好 30 秒应当放行")
	assert.Empty(t, reason)
}

func TestWan3ReferenceVideosOutOfRange_UnprobeableVideoSkipsRoute(t *testing.T) {
	// 不是 MP4/MOV，解析不出 mvhd。无法判断会不会超限 → 保守地不改派。
	body := map[string]interface{}{
		"model":   testSourceModel,
		"seconds": "5",
		"media": []interface{}{map[string]interface{}{
			"type": "reference_video",
			"url":  "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("not an mp4")),
		}},
	}
	out, reason := wan3ReferenceVideosOutOfRange(body)
	assert.True(t, out, "探测失败必须保守地挡住改派")
	assert.Contains(t, reason, "预检不通过")
}

// ---- 单测：素材优先级 ----

func TestCollectReferenceVideos_TopLevelMediaWins(t *testing.T) {
	body := map[string]interface{}{
		"media": []interface{}{map[string]interface{}{
			"type": "reference_video", "url": "https://example.com/top.mp4",
		}},
		"metadata": map[string]interface{}{
			"media": []interface{}{map[string]interface{}{
				"type": "reference_video", "url": "https://example.com/meta.mp4",
			}},
		},
	}
	videos := collectReferenceVideos(body)
	require.Len(t, videos, 1)
	// 与 wan3 的 buildWan3Media 一致：顶层 media 非空就只看顶层。
	// 读错层会对着一个根本不会被提交的视频做校验。
	assert.Equal(t, "https://example.com/top.mp4", videos[0])
}

func TestCollectReferenceVideos_IgnoresReferenceImages(t *testing.T) {
	body := map[string]interface{}{
		"media": []interface{}{
			map[string]interface{}{"type": "reference_image", "url": "https://example.com/a.png"},
			map[string]interface{}{"type": "reference_video", "url": "https://example.com/b.mp4"},
		},
	}
	videos := collectReferenceVideos(body)
	require.Len(t, videos, 1)
	assert.Equal(t, "https://example.com/b.mp4", videos[0])
}

// ---- 集成：中间件真的不路由 ----

func TestSeedanceRouteSkipsWan3WhenReferenceVideoTooLong(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	body := fmt.Sprintf(
		`{"model":%q,"seconds":"5","metadata":{"ratio":"9:16"},"media":[{"type":"reference_video","url":%q}]}`,
		testSourceModel, refTestVideoDataURL(32.39))

	seen, originModel := runRoute(t, body)

	// 不路由的唯一正确形态：请求体**逐字节不变**，来源模型名也不设。
	assert.Equal(t, body, seen, "参考视频超限时必须原样放行")
	assert.Empty(t, originModel, "没有改派就不该记来源模型")
}

func TestSeedanceRouteRoutesWan3WithShortReferenceVideo(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	body := fmt.Sprintf(
		`{"model":%q,"seconds":"5","metadata":{"ratio":"9:16"},"media":[{"type":"reference_video","url":%q}]}`,
		testSourceModel, refTestVideoDataURL(10))

	seen, originModel := runRoute(t, body)
	out := decodeBody(t, seen)

	// 10 秒参考 + 5 秒生成 = 15，合规 → 照常改派。
	assert.Equal(t, testRatioTargetModel, out["model"])
	assert.Equal(t, testSourceModel, originModel)
}

// 下面三条锁住「解析口径必须与 buildWan3Media 一致」——
// 这是审查时发现的两个真缺陷，不锁住就会复发。

func TestCollectReferenceVideos_TopLevelImagesHideMetadataVideos(t *testing.T) {
	// 顶层 media 有图片 → buildWan3Media 只用顶层，metadata.media 里的视频
	// **根本不会被提交**。挡板若去探测它，会把合规请求误挡。
	body := map[string]interface{}{
		"media": []interface{}{map[string]interface{}{
			"type": "reference_image", "url": "https://example.com/a.png",
		}},
		"metadata": map[string]interface{}{
			"media": []interface{}{map[string]interface{}{
				"type": "reference_video", "url": refTestVideoDataURL(32),
			}},
		},
	}
	assert.Empty(t, collectReferenceVideos(body),
		"顶层 media 非空时不该再看 metadata.media —— 那段视频不会被提交")
}

func TestCollectReferenceVideos_LegacyImagesVideo(t *testing.T) {
	// 客户用 legacy images 传 .mp4，buildWan3Media 会推断成 reference_video
	// 发给上游。挡板看不见它就会漏挡，超长视频照样撞 400。
	for _, key := range []string{"images", "image", "input_reference"} {
		body := map[string]interface{}{key: "https://example.com/ref.mp4"}
		videos := collectReferenceVideos(body)
		require.Len(t, videos, 1, "%s 里的 .mp4 必须被识别成参考视频", key)
		assert.Equal(t, "https://example.com/ref.mp4", videos[0])
	}
}

func TestCollectReferenceVideos_LegacyImagesIgnoresPictures(t *testing.T) {
	body := map[string]interface{}{
		"images": []interface{}{"https://example.com/a.png", "https://example.com/b.mp4?x=1"},
	}
	videos := collectReferenceVideos(body)
	require.Len(t, videos, 1, "只挑视频，图片不算；查询串不影响后缀判定")
	assert.Equal(t, "https://example.com/b.mp4?x=1", videos[0])
}

func TestWan3ReferenceVideosOutOfRange_LegacyImagesOver15s(t *testing.T) {
	// 端到端确认 legacy 路径也参与范围判定：images 里塞一段 32 秒视频。
	body := map[string]interface{}{
		"model":   testSourceModel,
		"seconds": "5",
		"images":  []interface{}{refTestVideoDataURL(32)},
	}
	out, reason := wan3ReferenceVideosOutOfRange(body)
	assert.True(t, out, "legacy images 里的超长视频必须挡住改派")
	assert.Contains(t, reason, "15")
}
