package taskcommon

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 成片地址占位符（内部协议）
//
// 这些用例锁死三件事：
//  1. 占位符能往返解析（生产方写、代理方读，两边必须用同一套实现）；
//  2. 对外出口必须把占位符换成本站代理地址 —— 客户端拿到 "cos-proxy://…"
//     是没法下载的，等于交付失败；
//  3. 普通直链与站内相对路径**原样保留**（换掉反而让客户端多经一次本机出口）。
// ─────────────────────────────────────────────────────────────────────────────

func TestBuildAndParseCOSProxyMarkerRoundTrip(t *testing.T) {
	marker := BuildCOSProxyMarker("my-bucket-1250000000", "ap-guangzhou", "mps_api_test/out/task-1.mp4")
	require.Equal(t, "cos-proxy://my-bucket-1250000000/ap-guangzhou/mps_api_test/out/task-1.mp4", marker)

	bucket, region, objectPath, ok := ParseCOSProxyMarker(marker)
	require.True(t, ok)
	require.Equal(t, "my-bucket-1250000000", bucket)
	require.Equal(t, "ap-guangzhou", region)
	require.Equal(t, "mps_api_test/out/task-1.mp4", objectPath)

	// 前导斜杠不应产生 "//"（对象路径以 / 开头时）。
	marker = BuildCOSProxyMarker("b", "r", "/out/task-2.mp4")
	require.Equal(t, "cos-proxy://b/r/out/task-2.mp4", marker)
}

func TestParseCOSProxyMarkerRejectsNonMarkers(t *testing.T) {
	for _, raw := range []string{
		"",
		"https://example.com/video.mp4",
		"/v1/videos/task_x/content",
		"cos-proxy://only-two-parts",
		"cos-proxy://",
	} {
		_, _, _, ok := ParseCOSProxyMarker(raw)
		require.False(t, ok, "raw=%q must not be treated as a marker", raw)
	}
}

func TestIsResultURLProxyMarker(t *testing.T) {
	require.True(t, IsResultURLProxyMarker(
		BuildCOSProxyMarker("my-bucket-1250000000", "ap-guangzhou", "out/task-1.mp4")))
	require.False(t, IsResultURLProxyMarker("https://example.com/video.mp4"))
	require.False(t, IsResultURLProxyMarker(""))
}

// TestPublicResultURLTranslatesMarkerToProxy 锁定「占位符 → 本站代理地址」。
//
// 这是交付链路的关键一环：库里存的是内部占位符，客户端拿到的必须是
// 能点的下载链接。漏了这一步，用户付了钱却拿到一个下不动的字符串。
func TestPublicResultURLTranslatesMarkerToProxy(t *testing.T) {
	marker := BuildCOSProxyMarker("my-bucket-1250000000", "ap-guangzhou", "out/task-1.mp4")
	got := PublicResultURL("task_abc", marker)
	require.Equal(t, BuildProxyURL("task_abc"), got)
	require.NotContains(t, got, "cos-proxy://", "内部标记不能漏给客户端")
}

// TestPublicResultURLKeepsDirectLinks 锁定「普通直链原样保留」。
//
// 直链是客户端直连上游存储/CDN 最快的路径；换成本站代理反而多经一次
// 本机公网出口（本机 3Mbps，30MB 成片要走 ~87 秒）。
func TestPublicResultURLKeepsDirectLinks(t *testing.T) {
	direct := "https://cdn.example.com/video.mp4?sig=abc"
	require.Equal(t, direct, PublicResultURL("task_abc", direct))

	// 站内相对路径（已经是代理地址）也原样保留。
	relative := "/v1/videos/task_abc/content"
	require.Equal(t, relative, PublicResultURL("task_abc", relative))
}

// TestPublicResultURLWithoutTaskIDNeverLeaksMarker 锁定「没有 taskID 时宁可不给地址」。
//
// 造不出代理地址时返回占位符等于把内部协议发给客户端 —— 宁可空着。
func TestPublicResultURLWithoutTaskIDNeverLeaksMarker(t *testing.T) {
	marker := BuildCOSProxyMarker("b", "r", "out/task-1.mp4")
	require.Empty(t, PublicResultURL("", marker))
}
