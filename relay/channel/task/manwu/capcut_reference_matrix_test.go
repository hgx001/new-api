package manwu

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CapCut 参考素材支持矩阵契约（2026-11-14 补）。
//
// ## 背景
//
// 之前 CapCut 链路对参考素材的处理是**三种都不落**：
//   - 参考图：只有顶层别名（input_reference / images / …）能被解析；
//   - 参考视频：clientRequest 有 Videos/Video 字段，但 kindCapcutVideo 分支
//     **从不调用** resolveVideos → 视频被静默丢弃（建单照跑、照扣费、客户白等）；
//   - 参考音频：clientRequest **根本没有**对应字段 → 同样静默丢弃。
//
// 而 Worker 侧 `checkCapcutParamCombination` 明写：
// 「我们不收视频/音频参考素材」→ 官网素材口 accept 含 video/audio，
// 但交付链只处理图片，混进来会出现"收了但没进成片"的错账。
//
// ## 用户口径（2026-11-14 明确）
//
//	① new-api 侧**要写支持图片和音频，不支持视频**；
//	② 收到音频参数后**静默丢掉**——参数要被接受（不能 400），
//	   但**不能下发给下游**（ArcReel / Worker 感知不到音频存在）；
//	③ 参考视频**显式拒绝**——静默丢会让客户按带视频下单却拿到纯图生成的片；
//	④ 参考图上限提到 **30 张**。
//
// ## 本文件锁住的契约
//
//   - 参考图 30 张：29 张通过、30 张通过、31 张 400；
//   - 参考音频：顶层 audios / audio / media[{type:reference_audio}] 三种形态
//     **全部接受**，且建单载荷里**不得出现任何音频素材**（inputs 只剩图）；
//   - 参考视频：顶层 videos / video / media[{type:reference_video}] 三种形态
//     **全部 400**；
//   - 静默丢弃要**留痕**（日志），否则出问题时无法区分「没传」与「传了被丢」。

const capcutRefImageURL = "https://example.com/ref.png"

// capcutRefImages 造 N 个不同的参考图 URL。
func capcutRefImages(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("https://example.com/ref-%02d.png", i))
	}
	return out
}

// jsonList 把字符串数组渲染成合法 JSON 数组（%q 对 []string 会产出
// `["a" "b"]` 这种非法 JSON —— Go 的 %q 对 slice 逐元素加引号但**不加逗号**）。
func jsonList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		quoted = append(quoted, fmt.Sprintf("%q", item))
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

// buildCapcutBody 造一条 CapCut 请求体（参考素材按形态注入）。
func buildCapcutBody(extra string) string {
	return fmt.Sprintf(`{"model":%q,"prompt":"hi","seconds":5,"resolution":"480p","ratio":"16:9"%s}`,
		ModelCapcut20Mini, extra)
}

// ── ① 参考图上限 30 张 ────────────────────────────────────────────────

func TestCapcutReferenceImagesLimit30(t *testing.T) {
	for _, tc := range []struct {
		count   int
		wantErr bool
	}{
		{count: 0},
		{count: 1},
		{count: 29},
		{count: 30},
		{count: 31, wantErr: true},
	} {
		t.Run(fmt.Sprintf("images-%d", tc.count), func(t *testing.T) {
			imgs := capcutRefImages(tc.count)
			body := buildCapcutBody(`,"images":` + jsonList(imgs))

			c, info, a := postVideoCtx(t, body)
			err := a.ValidateRequestAndSetAction(c, info)
			if tc.wantErr {
				require.NotNil(t, err, "31 张参考图必须被拒")
				assert.Contains(t, err.Message, "30")
				return
			}
			require.Nil(t, err)

			reader, bErr := a.BuildRequestBody(c, info)
			require.NoError(t, bErr)
			data, rErr := io.ReadAll(reader)
			require.NoError(t, rErr)

			var payload map[string]any
			require.NoError(t, json.Unmarshal(data, &payload))
			inputs, _ := payload["inputs"].([]any)
			assert.Len(t, inputs, tc.count, "参考图必须原样透传，不能静默截断")
		})
	}
}

// ── ② 参考音频：接受但静默丢弃 ────────────────────────────────────────

func TestCapcutReferenceAudioSilentlyDropped(t *testing.T) {
	cases := []struct {
		name  string
		extra string
	}{
		{"top-level-audios", `,"audios":["https://example.com/a.mp3"]`},
		{"top-level-audio", `,"audio":"https://example.com/a.mp3"`},
		{"media-reference-audio", `,"media":[{"type":"reference_audio","url":"https://example.com/a.mp3"}]`},
		{"metadata-media-reference-audio", `,"metadata":{"media":[{"type":"reference_audio","url":"https://example.com/a.mp3"}]}`},
		{"metadata-input-media-reference-audio", `,"metadata":{"input":{"media":[{"type":"reference_audio","url":"https://example.com/a.mp3"}]}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := buildCapcutBody(tc.extra)
			c, info, a := postVideoCtx(t, body)

			// 契约①：不能报错 —— 参数要被接受。
			err := a.ValidateRequestAndSetAction(c, info)
			require.Nil(t, err, "参考音频必须被接受（静默丢弃 ≠ 拒绝）")

			reader, bErr := a.BuildRequestBody(c, info)
			require.NoError(t, bErr)
			data, rErr := io.ReadAll(reader)
			require.NoError(t, rErr)

			// 契约②：建单载荷里**不得出现任何音频素材** ——
			// 下游（ArcReel / Worker）不能知道音频存在。
			raw := string(data)
			assert.NotContains(t, raw, "reference_audio", "音频素材不得下发给下游")
			assert.NotContains(t, raw, "a.mp3", "音频 URL 不得下发给下游")

			var payload map[string]any
			require.NoError(t, json.Unmarshal(data, &payload))
			inputs, _ := payload["inputs"].([]any)
			for _, it := range inputs {
				m, _ := it.(map[string]any)
				assert.NotEqual(t, "reference_audio", m["type"])
				assert.NotEqual(t, "audio", m["type"])
			}
		})
	}
}

// 音频 + 图片混传：图片照常透传，音频静默丢。
func TestCapcutReferenceAudioMixedWithImages(t *testing.T) {
	imgs := capcutRefImages(2)
	body := buildCapcutBody(
		`,"images":` + jsonList(imgs) +
			`,"audios":["https://example.com/a.mp3"],"media":[{"type":"reference_audio","url":"https://example.com/b.mp3"}]`)

	c, info, a := postVideoCtx(t, body)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	inputs, _ := payload["inputs"].([]any)
	require.Len(t, inputs, 2, "两张参考图必须都在")

	for _, it := range inputs {
		m, _ := it.(map[string]any)
		assert.Equal(t, "reference_image", m["type"])
	}
	assert.NotContains(t, string(data), "reference_audio")
}

// ── ③ 参考视频：2026-11-14 放开（口径反转） ──────────────────────────
//
// 旧口径是「显式 400」，前提是「官网是否接受视频参考未验证」。真机标定推翻了
// 这个前提：CapCut「多模态参考」模式下参考位的 file input accept 是
// image/*,video/*,audio/*，上传 4 秒 mp4 后页面立刻出现
// data-reference-media-tag-type="video" 节点并在 6 秒内变 loaded，无报错。
//
// 现在：三种形态全部**接受**，视频进 inputs 的 reference_video 槽；
// 但上限仍是 1 个（Worker 载荷 referenceVideo 是单值），超出显式 400。

// capcutRefVideoDataURL 造一个「结构可解析」的最小 MP4 data URL。
//
// 为什么不用 https://example.com/a.mp4：计费探测真的会去读 mvhd
// （拿不到时长就 fail-loud 400，绝不按 0 秒放行），用外网地址会让单测
// 依赖网络，而 example.com 返回 HTML 必然解析失败 → 全部用例假红。
func capcutRefVideoDataURL(seconds float64) string {
	box := func(boxType string, payload []byte) []byte {
		out := make([]byte, 8, 8+len(payload))
		binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
		copy(out[4:8], boxType)
		return append(out, payload...)
	}
	// mvhd v0：version/flags + create/modify(各4) + timescale(4) + duration(4) + 其余
	payload := []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 2}
	var ts [4]byte
	binary.BigEndian.PutUint32(ts[:], 1000)
	payload = append(payload, ts[:]...)
	var dur [4]byte
	binary.BigEndian.PutUint32(dur[:], uint32(seconds*1000))
	payload = append(payload, dur[:]...)
	payload = append(payload, make([]byte, 80)...)

	mp4 := box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2"))
	mp4 = append(mp4, box("moov", box("mvhd", payload))...)
	return "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(mp4)
}

func TestCapcutReferenceVideoAccepted(t *testing.T) {
	video := capcutRefVideoDataURL(6)
	cases := []struct {
		name  string
		extra string
	}{
		{"top-level-videos", fmt.Sprintf(`,"videos":[%q]`, video)},
		{"top-level-video", fmt.Sprintf(`,"video":%q`, video)},
		{"media-reference-video", fmt.Sprintf(`,"media":[{"type":"reference_video","url":%q}]`, video)},
		{"metadata-media-reference-video", fmt.Sprintf(`,"metadata":{"media":[{"type":"reference_video","url":%q}]}`, video)},
		{"metadata-input-media-reference-video", fmt.Sprintf(`,"metadata":{"input":{"media":[{"type":"reference_video","url":%q}]}}`, video)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := buildCapcutBody(tc.extra)
			c, info, a := postVideoCtx(t, body)
			require.Nil(t, a.ValidateRequestAndSetAction(c, info), "参考视频必须被接受")

			reader, err := a.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)

			var payload struct {
				Inputs []struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"inputs"`
			}
			require.NoError(t, json.Unmarshal(data, &payload))
			require.Len(t, payload.Inputs, 1, "一个视频就是一条 reference_video")
			assert.Equal(t, "reference_video", payload.Inputs[0].Type)
			assert.Equal(t, video, payload.Inputs[0].URL)
		})
	}
}

// 视频必须排在图片**之后**：Worker 按顺序逐条上传，图先落位才不会被
// 视频的解码耗时拖住；客户端 @imageN 的位置绑定也依赖图在前。
func TestCapcutReferenceVideoGoesAfterImages(t *testing.T) {
	video := capcutRefVideoDataURL(6)
	body := buildCapcutBody(fmt.Sprintf(`,"images":[%q],"videos":[%q]`, capcutRefImageURL, video))
	c, info, a := postVideoCtx(t, body)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload struct {
		Inputs []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	require.Len(t, payload.Inputs, 2)
	assert.Equal(t, "reference_image", payload.Inputs[0].Type)
	assert.Equal(t, "reference_video", payload.Inputs[1].Type)
}

// 上限仍是 1 个：Worker 载荷 referenceVideo 是单值字段，多收的视频无处可放，
// 收钱只下发 1 个 = 错交付，必须显式 400。
func TestCapcutReferenceVideoLimitOne(t *testing.T) {
	video := capcutRefVideoDataURL(6)
	body := buildCapcutBody(fmt.Sprintf(`,"videos":[%q,%q]`, video, video))
	c, info, a := postVideoCtx(t, body)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err, "两个参考视频必须被拒")
	assert.Contains(t, err.Message, "reference video")
}

// 计费：seconds = 生成时长 + 参考视频秒数（各条四舍五入后相加，1:1 同价）。
// 白送等于每单按秒亏 —— 上游 input_video_second 与 output_video_second 同价。
func TestCapcutEstimateBillingAddsReferenceVideoSeconds(t *testing.T) {
	video := capcutRefVideoDataURL(6)
	body := buildCapcutBody(fmt.Sprintf(`,"videos":[%q]`, video))
	c, info, a := postVideoCtx(t, body)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	billing := a.EstimateBilling(c, info)
	assert.Equal(t, float64(11), billing["seconds"], "5 秒生成 + 6 秒参考视频 = 11 秒")
}

// 没有参考视频时 seconds 不得被改动（否则无视频的单被凭空加价）。
func TestCapcutEstimateBillingWithoutReferenceVideo(t *testing.T) {
	body := buildCapcutBody("")
	c, info, a := postVideoCtx(t, body)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	billing := a.EstimateBilling(c, info)
	assert.Equal(t, float64(5), billing["seconds"])
}

// ── ④ 载荷不得出现未知字段 ────────────────────────────────────────────

// 建单载荷的键集合是**封闭**的：多一个键就可能被下游当新语义解析。
func TestCapcutPayloadKeysAreClosed(t *testing.T) {
	body := buildCapcutBody(`,"audios":["https://example.com/a.mp3"],"media":[{"type":"reference_audio","url":"https://example.com/a.mp3"}]`)
	c, info, a := postVideoCtx(t, body)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))

	allowed := map[string]bool{
		"platformId": true, "outputMode": true, "prompt": true,
		"videoParams": true, "inputs": true, "idempotencyKey": true,
	}
	for k := range payload {
		assert.True(t, allowed[k], "建单载荷出现未登记的键: "+k)
	}
}
