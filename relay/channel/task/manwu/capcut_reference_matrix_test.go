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

// CapCut 参考素材支持矩阵契约（2026-11-15 v2：音频/视频全量透传）。
//
// ## 背景
//
// 2026-11-14 的旧契约是「图片透传 / 音频静默丢 / 视频只收 1 个」。用户口径在
// 2026-11-15 反转为「音视频都要真的进成片」：CapCut 官网「多模态参考」本来就
// 收 image/video/audio 三类素材，网关继续丢音频 = 客户按带音频下单却拿到
// 没有音频参考的片（错交付，与 2026-10-22 丢图事故同型）。
//
// ## 现行契约（本文件锁住的东西）
//
//	① 参考图 30 张：29 / 30 通过，31 → 400；
//	② 参考音频 5 条：顶层 audios / audio / media / metadata 多种形态**全部透传**，
//	   inputs 里必须出现 reference_audio 且**排在最后**；
//	③ 参考视频 5 条，同样透传，排在图片之后、音频之前；
//	④ 缺 URL 的素材（只有 path）→ 400，不静默跳过；
//	⑤ 视频**合计** ≤15 秒（未取整浮点判），音频**合计** ≤15 秒；
//	⑥ 音频时长测不出 → **放行**（不参与计费，宁放不误拒）；
//	⑦ 计费只加参考视频秒数，音频一分不加。
//
// ## 为什么这些边界必须有测试
//
// 上限/顺序/计费三类判据的错法都是**静默的**：上限写宽了不会报错，顺序排错了
// 只会串脸，音频混进计费只会多收钱 —— 事后都查不出来。所以一条判据一条用例。

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

// buildCapcutBodyFor 指定模型造请求体（时长上界随模型变，上限用例要用 2.0 档）。
func buildCapcutBodyFor(model string, extra string) string {
	return fmt.Sprintf(`{"model":%q,"prompt":"hi","seconds":5,"resolution":"480p","ratio":"16:9"%s}`,
		model, extra)
}

// capcutInput 是建单载荷里的一条参考素材。
type capcutInput struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// readCapcutInputs 走 Validate → BuildRequestBody，取出 inputs 列表。
func readCapcutInputs(t *testing.T, body string) []capcutInput {
	t.Helper()
	c, info, a := postVideoCtx(t, body)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info), "请求必须被接受")
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, rErr := io.ReadAll(reader)
	require.NoError(t, rErr)

	var payload struct {
		Inputs []capcutInput `json:"inputs"`
	}
	require.NoError(t, json.Unmarshal(data, &payload))
	return payload.Inputs
}

// capcutInputTypes 把 inputs 压成 type 序列，用于顺序断言。
func capcutInputTypes(inputs []capcutInput) []string {
	out := make([]string, 0, len(inputs))
	for _, item := range inputs {
		out = append(out, item.Type)
	}
	return out
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

// ── ② 参考音频：全量透传（2026-11-15 口径反转） ──────────────────────
//
// 旧契约是「接受但静默丢」（`TestCapcutReferenceAudioSilentlyDropped`）。
// 现在反过来：五种请求形态都必须把音频**原样**放进 inputs。

func TestCapcutReferenceAudioPassedThrough(t *testing.T) {
	audio := capcutRefAudioDataURL(3)
	cases := []struct {
		name  string
		extra string
	}{
		{"top-level-audios", fmt.Sprintf(`,"audios":[%q]`, audio)},
		{"top-level-audio", fmt.Sprintf(`,"audio":%q`, audio)},
		{"media-reference-audio", fmt.Sprintf(`,"media":[{"type":"reference_audio","url":%q}]`, audio)},
		{"metadata-media-reference-audio", fmt.Sprintf(`,"metadata":{"media":[{"type":"reference_audio","url":%q}]}`, audio)},
		{"metadata-input-media-reference-audio", fmt.Sprintf(`,"metadata":{"input":{"media":[{"type":"reference_audio","url":%q}]}}`, audio)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inputs := readCapcutInputs(t, buildCapcutBody(tc.extra))
			require.Len(t, inputs, 1, "音频必须下发，不能被丢弃")
			assert.Equal(t, "reference_audio", inputs[0].Type)
			assert.Equal(t, audio, inputs[0].URL, "URL 必须原样透传，不能改写")
		})
	}
}

// 顺序契约：image → video → audio。
//
// 顺序不是审美问题：Worker 按 inputs 顺序逐条上传，素材节点顺序决定
// `@imageN/@videoN/@audioN` 的槽位基准，排错了就是串脸（本次事故的成因）。
func TestCapcutReferenceOrderImageVideoAudio(t *testing.T) {
	video := capcutRefVideoDataURL(3)
	body := buildCapcutBody(fmt.Sprintf(
		`,"images":[%q,%q],"videos":[%q],"audios":[%q,%q]`,
		capcutRefImages(2)[0], capcutRefImages(2)[1], video,
		capcutRefAudioDataURL(3), capcutRefAudioDataURL(4)))

	inputs := readCapcutInputs(t, body)
	require.Len(t, inputs, 5)
	assert.Equal(t, []string{
		"reference_image", "reference_image",
		"reference_video",
		"reference_audio", "reference_audio",
	}, capcutInputTypes(inputs))
}

// 上限 5 条：第 6 条显式 400，不静默截断（收 6 条只交 5 条 = 错交付）。
func TestCapcutReferenceAudioLimitFive(t *testing.T) {
	audios := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		audios = append(audios, capcutRefAudioDataURL(3))
	}
	inputs := readCapcutInputs(t, buildCapcutBody(`,"audios":`+jsonList(audios)))
	require.Len(t, inputs, 5, "5 条音频必须全部透传")

	tooMany := append(audios, capcutRefAudioDataURL(1))
	c, info, a := postVideoCtx(t, buildCapcutBody(`,"audios":`+jsonList(tooMany)))
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err, "6 条音频必须被拒")
	assert.Contains(t, err.Message, "5")
}

// 上限 5 条：第 6 个视频显式 400。
func TestCapcutReferenceVideoLimitFive(t *testing.T) {
	videos := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		videos = append(videos, capcutRefVideoDataURL(3))
	}
	inputs := readCapcutInputs(t, buildCapcutBody(`,"videos":`+jsonList(videos)))
	require.Len(t, inputs, 5, "5 个视频（合计 15 秒）必须全部透传")

	tooMany := append(videos, capcutRefVideoDataURL(1))
	c, info, a := postVideoCtx(t, buildCapcutBody(`,"videos":`+jsonList(tooMany)))
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err, "6 个视频必须被拒")
	assert.Contains(t, err.Message, "5")
}

// 只有 path 没有 url 的素材必须 400：new-api 只下发 URL，
// 静默跳过 = 客户按「带音频」下单却拿到没有音频的片。
func TestCapcutReferenceAudioMissingURL(t *testing.T) {
	body := buildCapcutBody(`,"media":[{"type":"reference_audio","path":"/tmp/a.mp3"}]`)
	c, info, a := postVideoCtx(t, body)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err, "只有 path 的音频必须被拒")
	assert.Contains(t, err.Message, "audio")
	assert.Contains(t, err.Message, "url")
}

func TestCapcutReferenceVideoMissingURL(t *testing.T) {
	body := buildCapcutBody(`,"media":[{"type":"reference_video","path":"/tmp/a.mp4"}]`)
	c, info, a := postVideoCtx(t, body)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err, "只有 path 的视频必须被拒")
	assert.Contains(t, err.Message, "video")
	assert.Contains(t, err.Message, "url")
}

// ── ③ 时长上限：视频「合计」≤15 秒 ──────────────────────────────────
//
// ⚠️ 这是**破坏性收紧**：旧口径是「逐条 ≤15s」，2×8s 一直能跑；
// 新口径合计 16s 开始 400。两个边界都要显式锁定，否则回归时看不出来。

func TestCapcutReferenceVideoTotalSecondsLimit(t *testing.T) {
	// 边界：单条 15 秒仍放行（合计 = 15）。
	single := readCapcutInputs(t, buildCapcutBody(fmt.Sprintf(`,"videos":[%q]`, capcutRefVideoDataURL(15))))
	require.Len(t, single, 1, "单条 15 秒是闭区间上界，不能误伤")

	// 边界：2×7s = 14s 放行。
	pair := readCapcutInputs(t, buildCapcutBody(
		fmt.Sprintf(`,"videos":[%q,%q]`, capcutRefVideoDataURL(7), capcutRefVideoDataURL(7))))
	require.Len(t, pair, 2, "合计 14 秒必须放行")

	// 超限：2×8s = 16s 拒。
	c, info, a := postVideoCtx(t, buildCapcutBody(
		fmt.Sprintf(`,"videos":[%q,%q]`, capcutRefVideoDataURL(8), capcutRefVideoDataURL(8))))
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err, "合计 16 秒必须被拒")
	assert.Contains(t, err.Message, "16")
}

// ── ④ 时长上限：音频「合计」≤15 秒 ──────────────────────────────────

func TestCapcutReferenceAudioTotalSecondsLimit(t *testing.T) {
	// 边界：单条 15 秒放行。
	single := readCapcutInputs(t, buildCapcutBody(
		fmt.Sprintf(`,"audios":[%q]`, capcutRefAudioDataURL(15))))
	require.Len(t, single, 1, "单条 15 秒是闭区间上界，不能误伤")

	// 边界：5×3s = 15s 放行。
	audios := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		audios = append(audios, capcutRefAudioDataURL(3))
	}
	inputs := readCapcutInputs(t, buildCapcutBody(`,"audios":`+jsonList(audios)))
	require.Len(t, inputs, 5, "5 条各 3 秒合计 15 秒必须放行")

	// 超限：2×8s = 16s 拒。
	c, info, a := postVideoCtx(t, buildCapcutBody(
		fmt.Sprintf(`,"audios":[%q,%q]`, capcutRefAudioDataURL(8), capcutRefAudioDataURL(8))))
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err, "音频合计 16 秒必须被拒")
	assert.Contains(t, err.Message, "16")
}

// 音频时长**测不出**时放行（不参与计费 → 宁可放）。
//
// 与视频的 fail-loud 刻意相反：客户素材地址可能只对上游可达（IP 白名单 /
// 私有签名），我们抓不到不代表超长；一刀切拒单会把本来能跑的请求打死。
func TestCapcutReferenceAudioUnmeasurableIsLenient(t *testing.T) {
	// 不是 RIFF / ID3 / MPEG 帧同步 → 解析不出时长。
	garbage := "data:audio/aac;base64," + base64.StdEncoding.EncodeToString([]byte("not-audio-at-all"))
	inputs := readCapcutInputs(t, buildCapcutBody(fmt.Sprintf(`,"audios":[%q]`, garbage)))
	require.Len(t, inputs, 1, "解析不出时长不能拒单")
	assert.Equal(t, "reference_audio", inputs[0].Type)
}

// ── ⑤ 计费：视频计入、音频不计入 ─────────────────────────────────────

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

// 参考音频**不参与计费**：同一请求带/不带音频，seconds 必须相等。
//
// 为什么不写成「= 5」：那样只锁住了当前数字，锁不住「音频被顺手加进去」。
// 对照写法能直接指出是哪一边变了。
func TestCapcutEstimateBillingIgnoresReferenceAudio(t *testing.T) {
	without := buildCapcutBody("")
	c1, info1, a1 := postVideoCtx(t, without)
	require.Nil(t, a1.ValidateRequestAndSetAction(c1, info1))
	base := a1.EstimateBilling(c1, info1)

	with := buildCapcutBody(fmt.Sprintf(`,"audios":[%q]`, capcutRefAudioDataURL(10)))
	c2, info2, a2 := postVideoCtx(t, with)
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, info2))
	got := a2.EstimateBilling(c2, info2)

	assert.Equal(t, base["seconds"], got["seconds"], "音频是内容参考，不进计费秒")
	assert.Equal(t, float64(5), got["seconds"], "音频不应改写 seconds")
}

// ── ⑥ 载荷不得出现未知字段 ────────────────────────────────────────────

// 建单载荷的键集合是**封闭**的：多一个键就可能被下游当新语义解析。
func TestCapcutPayloadKeysAreClosed(t *testing.T) {
	body := buildCapcutBody(fmt.Sprintf(
		`,"audios":[%q],"images":[%q]`, capcutRefAudioDataURL(3), capcutRefImageURL))
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

// ── 测试素材构造 ──────────────────────────────────────────────────────

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

// capcutRefAudioDataURL 造一个「结构可解析」的最小 WAV data URL（8kHz/单声道/8bit）。
//
// data chunk 的**长度字段**按目标秒数写，实际只带 4 个字节 ——
// parseWavSeconds 只读 chunk 头算时长（dataBytes ÷ byteRate），不解码采样，
// 所以不需要真的塞满几十万字节（15 秒的 8kHz/8bit 就是 120KB）。
func capcutRefAudioDataURL(seconds float64) string {
	const byteRate = 8000
	payload := make([]byte, 0, 48)
	payload = append(payload, "RIFF"...)
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], uint32(36+seconds*byteRate))
	payload = append(payload, word[:]...)
	payload = append(payload, "WAVE"...)
	payload = append(payload, "fmt "...)
	binary.LittleEndian.PutUint32(word[:], 16)
	payload = append(payload, word[:]...)

	var fmtChunk [16]byte
	binary.LittleEndian.PutUint16(fmtChunk[0:2], 1)         // PCM
	binary.LittleEndian.PutUint16(fmtChunk[2:4], 1)         // 单声道
	binary.LittleEndian.PutUint32(fmtChunk[4:8], byteRate)  // 采样率
	binary.LittleEndian.PutUint32(fmtChunk[8:12], byteRate) // 字节率（时长就靠它算）
	binary.LittleEndian.PutUint16(fmtChunk[12:14], 1)       // block align
	binary.LittleEndian.PutUint16(fmtChunk[14:16], 8)       // 位深
	payload = append(payload, fmtChunk[:]...)

	payload = append(payload, "data"...)
	binary.LittleEndian.PutUint32(word[:], uint32(seconds*byteRate))
	payload = append(payload, word[:]...)
	payload = append(payload, 0, 0, 0, 0)

	return "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(payload)
}
