package youzanwan3

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wan-3.0-pro（2026-11-18 接入）的契约全部照上游文档 2026-11-17 版：
//   - 时长 4-30 整数秒；
//   - 分辨率 720P / 1080P，size 只有 1280x720 / 720x1280 / 1920x1080 / 1080x1920；
//   - 画幅仅 16:9 / 9:16；
//   - 素材走 images / videos / audios 数组；
//   - 按条计费 ¥5.0（720P）/ ¥7.0（1080P），与时长无关。
//
// 这组测试钉住「分模型分支不能漏判」：漏判的表现分别是
// 5 秒被 400（落进标准版 10 秒下界）、1080P 按 720P 收（倍率读错）、
// 参考音频被拒（落进「仅 reference_image」闸门）、按条被乘成按秒。

func TestWan30ProDurationBounds(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantRej bool
	}{
		{name: "未传默认 5 秒", req: relaycommon.TaskSubmitReq{Model: wan30ProModel}, want: 5},
		{name: "显式 4 秒合法（下界）", req: relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 4}, want: 4},
		{name: "显式 30 秒合法（上界）", req: relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 30}, want: 30},
		{name: "显式 5 秒合法", req: relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 5}, want: 5},
		{name: "低于 4 秒 400", req: relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 3}, wantRej: true},
		{name: "高于 30 秒 400", req: relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 31}, wantRej: true},
		{name: "seconds 字符串写法 12 秒", req: relaycommon.TaskSubmitReq{Model: wan30ProModel, Seconds: "12"}, want: 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDurationForModel(tc.req, tc.req.Model)
			taskErr := validateExplicitDurationForModel(tc.req)
			if tc.wantRej {
				require.NotNil(t, taskErr, "越界时长必须 400，不能夹紧")
				assert.Equal(t, 400, taskErr.StatusCode)
				return
			}
			require.Nil(t, taskErr)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestWan30ProResolutionFromDocumentedSize 锁定「客户照抄文档只传 size 时，
// 1080P 不能被静默降成 720P」——normalizeResolution("1920x1080") 是空串，
// 必须走 resolutionFromWan30ProSize 反推档位。
func TestWan30ProResolutionFromDocumentedSize(t *testing.T) {
	cases := []struct {
		size string
		want string
	}{
		{"1280x720", "720P"},
		{"720x1280", "720P"},
		{"1920x1080", "1080P"},
		{"1080x1920", "1080P"},
		{" 1920X1080 ", "1080P"},
		{"1920×1080", "1080P"},
		{"", ""},
		{"1080x1080", ""}, // 1:1 上游不支持 —— 不猜档位，交给比例校验 400
		{"1600x900", ""},  // 文档没列，不认
		{"720P", ""},      // 分辨率写法不是尺寸
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, resolutionFromWan30ProSize(tc.size), "size=%q", tc.size)
	}

	req := relaycommon.TaskSubmitReq{Model: wan30ProModel, Size: "1920x1080"}
	assert.Equal(t, "1080P", resolveResolutionForModel(req, req.Model))
	req = relaycommon.TaskSubmitReq{Model: wan30ProModel, Size: "1280x720"}
	assert.Equal(t, "720P", resolveResolutionForModel(req, req.Model))
	// 没传任何尺寸信息 → 默认最低档 720P（不静默升到更贵的 1080P）。
	req = relaycommon.TaskSubmitReq{Model: wan30ProModel}
	assert.Equal(t, "720P", resolveResolutionForModel(req, req.Model))
}

// TestWan30ProBillsPerVideoNotPerSecond 锁定按条计费：
//   - 720P → size=1.0（ModelPrice = ¥5.0）
//   - 1080P → size=7/5（结果 ¥7.0）
//   - 无论 4 秒还是 30 秒，quota 完全相同
//   - 参考视频秒数不并入（按条不按秒）
func TestWan30ProBillsPerVideoNotPerSecond(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}

	billing := func(req relaycommon.TaskSubmitReq) map[string]float64 {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("task_request", req)
		if req.Duration == 0 {
			req.Duration = resolveDurationForModel(req, req.Model)
		}
		c.Set("task_request", req)
		return adaptor.EstimateBilling(c, &relaycommon.RelayInfo{OriginModelName: wan30ProModel})
	}

	req720 := relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 5, Resolution: "720P"}
	got := billing(req720)
	require.Equal(t, map[string]float64{"size": 1.0}, got, "按条 720P：只应有 size 倍率，不能带 seconds")

	req1080 := relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 5, Resolution: "1080P"}
	got = billing(req1080)
	require.Len(t, got, 1)
	assert.InDelta(t, 7.0/5.0, got["size"], 1e-9, "1080P 倍率 = 7/5")

	// 4 秒与 30 秒的倍率必须完全一样（与时长无关）。
	short := billing(relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 4, Resolution: "720P"})
	long := billing(relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 30, Resolution: "720P"})
	assert.Equal(t, short, long, "按条计费：时长不得影响倍率")

	// 参考视频秒数写进 context 也不能被读进计费。
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{Model: wan30ProModel, Duration: 5})
	c.Set(refVideoSecondsContextKey, 10)
	got = adaptor.EstimateBilling(c, &relaycommon.RelayInfo{OriginModelName: wan30ProModel})
	assert.Equal(t, map[string]float64{"size": 1.0}, got, "按条计费：参考视频秒数不得并入")

	// 价格换算：ModelPrice（720P 基准档，5/7.3 USD）× size 倍率 × 500000 quota/USD
	// = ¥5.0 / ¥7.0。quota 取整误差不超过 1 分钱。
	//
	// 这里直接读价表而不是写死 4.5/7.3：ModelPrice 与倍率表是两处配置，
	// 只改一处就会让 1080P 静默错价，读同一来源才能锁住两者一致。
	ratio_setting.InitRatioSettings()
	basePriceUSD, ok := ratio_setting.GetModelPrice(wan30ProModel, false)
	require.True(t, ok, "wan-3.0-pro 必须显式配价，否则会回落到按 token 计费")
	assert.InDelta(t, 5.0/ratio_setting.USD2RMB, basePriceUSD, 1e-9,
		"ModelPrice 的 720P 基准档必须等于 ¥5.0")
	quota720 := int(basePriceUSD * common.QuotaPerUnit)
	assert.InDelta(t, 5.0, float64(quota720)/common.QuotaPerUnit*ratio_setting.USD2RMB, 0.01)
	quota1080 := int(float64(quota720) * (7.0 / 5.0))
	assert.InDelta(t, 7.0, float64(quota1080)/common.QuotaPerUnit*ratio_setting.USD2RMB, 0.01)
}

// TestWan30ProRequestBodyFollowsUpstreamDoc 锁定请求体与上游文档逐字对齐：
// seconds 是整数、size 是固定尺寸、素材是三个顶层数组、不出现 media/ratio/resolution。
func TestWan30ProRequestBodyFollowsUpstreamDoc(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Model:      wan30ProModel,
		Prompt:     "清晨湖泊，镜头平稳推进",
		Duration:   5,
		Resolution: "1080P",
		Ratio:      "9:16",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: "https://cdn.example/a.jpg"},
			{Type: "reference_video", URL: "https://cdn.example/v.mp4"},
		},
		Audios: []string{"https://cdn.example/a.mp3"},
	}
	raw, err := adaptor.buildRequestBody(req)
	require.NoError(t, err)

	var body map[string]interface{}
	require.NoError(t, common.Unmarshal(raw, &body))

	assert.Equal(t, wan30ProModel, body["model"])
	assert.Equal(t, "清晨湖泊，镜头平稳推进", body["prompt"])
	assert.EqualValues(t, 5, body["seconds"], "文档示例 seconds 是整数")
	assert.Equal(t, "1080x1920", body["size"], "1080P + 9:16 → 1080x1920")
	assert.Equal(t, []interface{}{"https://cdn.example/a.jpg"}, body["images"])
	assert.Equal(t, []interface{}{"https://cdn.example/v.mp4"}, body["videos"])
	assert.Equal(t, []interface{}{"https://cdn.example/a.mp3"}, body["audios"], "顶层 audios 必须并入")
	for _, banned := range []string{"media", "ratio", "resolution"} {
		_, ok := body[banned]
		assert.Falsef(t, ok, "wan-3.0-pro 不应下发字段 %q（那是 wan3.0-video 的契约）", banned)
	}

	// 纯文生：不带任何素材数组。
	raw, err = adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model: wan30ProModel, Prompt: "纯文生", Duration: 4,
	})
	require.NoError(t, err)
	var textOnly map[string]interface{}
	require.NoError(t, common.Unmarshal(raw, &textOnly))
	assert.Equal(t, "1280x720", textOnly["size"], "默认 16:9")
	for _, key := range []string{"images", "videos", "audios"} {
		_, ok := textOnly[key]
		assert.Falsef(t, ok, "空素材不应下发 %q", key)
	}
}

// TestWan30ProAllowsAudioVideoReferences 锁定素材契约差异：
// pro 支持参考视频/音频（文档原话），标准版仍然不支持（2026-11-15 上游切换）。
func TestWan30ProAllowsAudioVideoReferences(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: "https://cdn.example/a.jpg"},
		{Type: "reference_video", URL: "https://cdn.example/v.mp4"},
		{Type: "reference_audio", URL: "https://cdn.example/a.mp3"},
	}
	require.NoError(t, validateMedia(media, wan30ProModel))

	// 标准版行为不能被改坏。
	err := validateMedia(media, videoModel)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference_image")
}

// TestWan30ProRejectsUnsupportedRatioAndSize 锁定画幅白名单 fail-loud：
// 显式 1:1 必须 400（不能静默给 16:9），文档外的 WxH 也必须 400。
func TestWan30ProRejectsUnsupportedRatioAndSize(t *testing.T) {
	okCases := []relaycommon.TaskSubmitReq{
		{Model: wan30ProModel, Ratio: "16:9"},
		{Model: wan30ProModel, Ratio: "9:16"},
		{Model: wan30ProModel, Size: "1280x720"},
		{Model: wan30ProModel, Size: "1920x1080"},
		{Model: wan30ProModel, Size: "720P"}, // 平台通用分辨率写法，照常接受
		{Model: wan30ProModel, Metadata: map[string]interface{}{"ratio": "9:16"}},
	}
	for _, req := range okCases {
		assert.Nil(t, validateWan30ProExplicitRatio(req), "req=%+v", req)
	}

	badCases := []relaycommon.TaskSubmitReq{
		{Model: wan30ProModel, Ratio: "1:1"},
		{Model: wan30ProModel, AspectRatio: "21:9"},
		{Model: wan30ProModel, Metadata: map[string]interface{}{"ratio": "4:3"}},
		{Model: wan30ProModel, Size: "1080x1080"},
		{Model: wan30ProModel, Size: "1600x900"},
	}
	for _, req := range badCases {
		taskErr := validateWan30ProExplicitRatio(req)
		require.NotNil(t, taskErr, "req=%+v 必须 400", req)
		assert.Equal(t, 400, taskErr.StatusCode)
	}

	// 其它模型不受影响（1:1 是标准版合法画幅）。
	assert.Nil(t, validateWan30ProExplicitRatio(relaycommon.TaskSubmitReq{
		Model: videoModel, Ratio: "1:1",
	}))
}

// TestWan30ProDefaultRatioIs16x9 锁定缺省画幅：上游不认 adaptive，
// 落 16:9 而不是其它模型的 adaptive。
func TestWan30ProDefaultRatioIs16x9(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Model: wan30ProModel}
	assert.Equal(t, "16:9", resolveRatio(req))
	// 标准版缺省仍是 adaptive。
	assert.Equal(t, defaultRatio, resolveRatio(relaycommon.TaskSubmitReq{Model: videoModel}))
}

// TestWan30ProUpstreamModelMapping 锁定公开名与上游 ID 一致，且不会误落到标准版。
func TestWan30ProUpstreamModelMapping(t *testing.T) {
	got, err := upstreamModelFor(wan30ProModel)
	require.NoError(t, err)
	assert.Equal(t, "wan-3.0-pro", got)

	// 请求级映射（含时长/分辨率判断的那层）也必须原样返回。
	got, err = upstreamModelForRequest(relaycommon.TaskSubmitReq{
		Model: wan30ProModel, Duration: 4, Resolution: "1080P",
	})
	require.NoError(t, err)
	assert.Equal(t, "wan-3.0-pro", got)
}

// TestWan30ProNotInPerCallSuppressionList 钉住一个**反直觉但必须成立**的前提：
// wan-3.0-pro 虽然按条计价（ModelPrice 一口价），却**不能**进
// constant.IsTaskPerCallModel 的名单。
//
// 原因：relay/relay_task.go 对「按次档」的处理是**清空所有倍率**
// （mergeEstimatedRatios → `PriceData.OtherRatios = nil`，applyBillingRatios
// 直接 return，resolveSubmitQuota 不重算）。那套逻辑是为 seedance2.5-活动
// 这类「单一档位、售价恒定」的模型写的 —— 清空是对的。
//
// 但 wan-3.0-pro 有 720P / 1080P 两个**不同售价**，靠的正是 EstimateBilling
// 返回的 size 倍率（1080P = 7/5）。一旦有人把它加进按次名单：
//   - 1080P 会被静默按 720P 的 ¥5.0 收（少收 ¥2，约 28.6%），账单上完全看不出来；
//   - 更糟的是没有任何报错，只能靠对账发现。
//
// 所以「按条计价」与「按次免倍率」在本项目里是**两个不同的概念**，
// 前者看 ModelPrice 是否配置，后者看 constant.IsTaskPerCallModel。
func TestWan30ProNotInPerCallSuppressionList(t *testing.T) {
	assert.False(t, constant.IsTaskPerCallModel(wan30ProModel),
		"wan-3.0-pro 靠 size 倍率区分 720P/1080P，进按次名单会让 1080P 按 ¥5.0 静默少收")

	// 对照：真正的一口价模型仍在名单里（这条防的是「为了修上面的问题把名单整个删掉」）。
	assert.True(t, constant.IsSeedance25PerCall("seedance2.5-活动"))
}
