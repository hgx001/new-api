package manwu

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHiggsfieldSpecRegistered(t *testing.T) {
	spec, ok := SpecFor(ModelHiggsfield25)
	if !ok {
		t.Fatalf("%s 未在 modelSpecs 注册", ModelHiggsfield25)
	}
	if spec.PlatformID != PlatformIDHiggsfield {
		t.Fatalf("platformId = %q, want %q", spec.PlatformID, PlatformIDHiggsfield)
	}
	if spec.OutputMode != OutputModeVideo {
		t.Fatalf("outputMode = %q, want video", spec.OutputMode)
	}
	if spec.UpstreamModel != "seedance-2.5" {
		t.Fatalf("upstream = %q, want seedance-2.5", spec.UpstreamModel)
	}
}

func TestResolveHiggsfieldDuration(t *testing.T) {
	cases := []struct {
		name string
		req  clientRequest
		want int
		ok   bool
	}{
		{"缺省 5 秒", clientRequest{}, 5, true},
		{"seconds=4 下界", clientRequest{Seconds: json.RawMessage(`4`)}, 4, true},
		{"duration=30 上界", clientRequest{Duration: json.RawMessage(`30`)}, 30, true},
		{"duration=16 逐秒", clientRequest{Duration: json.RawMessage(`"16"`)}, 16, true},
		{"3 秒越界", clientRequest{Duration: json.RawMessage(`3`)}, 0, false},
		{"31 秒越界", clientRequest{Duration: json.RawMessage(`31`)}, 0, false},
	}
	for _, c := range cases {
		got, err := resolveHiggsfieldDuration(c.req)
		if c.ok && err != nil {
			t.Fatalf("%s: 不应报错，got err=%v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Fatalf("%s: 应显式拒绝", c.name)
		}
		if c.ok && got != c.want {
			t.Fatalf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestHiggsfieldRejectsNonFixedResolution(t *testing.T) {
	// 分辨率固定 720p：显式传 1080p 必须 400，不能静默降成 720p 交付。
	if err := validateHiggsfieldResolution("1080p"); err == nil {
		t.Fatal("1080p 应被拒绝")
	}
	if err := validateHiggsfieldResolution(""); err != nil {
		t.Fatalf("空值应通过（缺省 720p）：%v", err)
	}
	if err := validateHiggsfieldResolution("720P"); err != nil {
		t.Fatalf("720P 应通过：%v", err)
	}
}

func TestHiggsfieldDescriptionMentionsFixedSpec(t *testing.T) {
	// 描述里的参数口径与产品说明一致（防止只改定价不改描述）
	sql, err := readFileForTest("../../../../scripts/prod-upsert-sd-seedance-25-20261125.sql")
	if err != nil {
		t.Skip("SQL 脚本不在当前工作目录")
	}
	for _, want := range []string{"sd-seedance-2.5", "720p", "0.034246575342", "4–30"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("上架脚本缺少 %q", want)
		}
	}
}

// EstimateBilling 必须给 seconds —— 否则不管时长都只扣 1 秒的价。
//
// 2026-11-25 生产台账：`sd-seedance-2.5` 从 06:04 到 10:03 的 12 条任务
// 全部 quota=17123（= ¥0.25，ModelPrice 原值），4 秒的片子少收 ¥0.75、
// 30 秒的少收 ¥7.25。根因是 EstimateBilling 没有 higgsfield 分支，
// 直连请求也没有 `applyRouteBillingSeconds` 兜底。
func TestHiggsfieldEstimateBillingReturnsSeconds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantSecs float64
	}{
		{"4 秒下限", `{"model":"sd-seedance-2.5","prompt":"x","duration":4}`, 4},
		{"15 秒", `{"model":"sd-seedance-2.5","prompt":"x","duration":15}`, 15},
		{"30 秒上限", `{"model":"sd-seedance-2.5","prompt":"x","duration":30}`, 30},
		{"缺省 5 秒", `{"model":"sd-seedance-2.5","prompt":"x"}`, 5},
	} {
		c, info, a := postVideoCtx(t, tc.body)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), tc.name)
		assert.Equal(t, map[string]float64{"seconds": tc.wantSecs}, a.EstimateBilling(c, info), tc.name)
	}
	// 它绝不能进按次名单：两边一起命中会互相抵消（按次名单挡倍率、adaptor 又给 seconds）。
	assert.False(t, constant.IsTaskPerCallModel(ModelHiggsfield25),
		"sd-seedance-2.5 按秒计价，不能登记进 TaskPerCallModels")
}

// TikTok Symphony 的 sd- 两档同为按秒，但 `tk-symphony-1.5pro` 是另一件商品：
// 手写 kind 判断会把它一起乘上秒数，所以必须按**精确模型名**判断。
func TestTiktokPerSecondModelsEstimateBilling(t *testing.T) {
	for _, tc := range []struct {
		model string
		secs  float64
	}{
		{ModelTiktok20Fast, 10},
		{ModelTiktok20, 5},
	} {
		body := `{"model":"` + tc.model + `","prompt":"x","duration":` + strconv.Itoa(int(tc.secs)) + `}`
		c, info, a := postVideoCtx(t, body)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), tc.model)
		assert.Equal(t, map[string]float64{"seconds": tc.secs}, a.EstimateBilling(c, info), tc.model)
	}
	// 1.5 Pro 不在按秒子集里，必须仍是 nil（否则 ¥x/次 会被乘成 ¥x×秒）。
	assert.False(t, constant.IsTiktokPerSecond(ModelTikTokSymphony),
		"tk-symphony-1.5pro 不按秒，不能被按秒判定命中")
}

// TestHiggsfieldReferenceVideoAudioNormalized 锁住「参考视频/音频必须写进
// normalizedRequest」这条真机坑（2026-11-25）：BuildRequestBody 读的是 norm，
// 只解析 req 不写 norm 时，带 videos/audios 下单到 ArcReel 只剩 reference_image。
func TestHiggsfieldReferenceVideoAudioNormalized(t *testing.T) {
	src, err := os.ReadFile("adaptor.go")
	if err != nil {
		t.Fatalf("read adaptor.go: %v", err)
	}
	code := string(src)
	idx := strings.Index(code, "case kindHiggsfieldVideo:")
	if idx < 0 {
		t.Fatal("kindHiggsfieldVideo branch missing")
	}
	end := strings.Index(code[idx:], "case kindTikTokVideo:")
	if end < 0 {
		t.Fatal("next case missing")
	}
	branch := code[idx : idx+end]
	if !strings.Contains(branch, "norm.Videos = videos") {
		t.Error("higgsfield branch must assign norm.Videos (BuildRequestBody reads normalizedRequest)")
	}
	if !strings.Contains(branch, "norm.Audios = ") {
		t.Error("higgsfield branch must assign norm.Audios")
	}
}
