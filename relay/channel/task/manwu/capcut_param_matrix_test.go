package manwu

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CapCut 参数传递矩阵契约（2026-11-14 补）。
//
// 背景：本轮把「new-api → ArcReel → Worker」三端参数传递逐段核对时发现，
// CapCut 侧 Worker 端**没有专属归一**，掉进万相兜底（产出 Wan2_7 + requireFreeMode），
// 恢复路径更是只剩 { generateWithCredits: true }，model/duration/resolution 全丢。
// 网关侧当时看着「正常」，说明**只测单点会漏**：
// 真正的防线是「五档 × 时长边界 × 分辨率 × 比例」全矩阵 ——
// 任何一档、任何一边界被改坏，矩阵里那一格必须红。
//
// 口径（与 Worker / ArcReel 三方一致）：
//   - 五档：cp-seedance-2.5 / -2.5flash / -2.0 / -2.0fast / -2.0mini；
//   - 时长：2.5 与 2.5 Flash = 4–30 秒，其余 4–15 秒，整数秒原样透传、不吸附不回落；
//   - 分辨率只有 480p / 720p（其它显式 400，不静默降级）；
//   - 比例白名单 16:9 / 9:16 / 1:1 / 3:4 / 4:3 / 21:9。

// 五档公开名 → 官网真名 + 时长上界 + 720p 倍率
var capcutMatrix = []struct {
	publicModel   string
	upstreamModel string
	maxDuration   int
	size720Ratio  float64
}{
	{ModelCapcut25, capcutUpstreamModel25, 30, 2.0},
	{ModelCapcut25Flash, capcutUpstreamModel25Flash, 30, 2.0},
	{ModelCapcut20, capcutUpstreamModel20, 15, 2.0},
	{ModelCapcut20Fast, capcutUpstreamModel20Fast, 15, 1.5},
	{ModelCapcut20Mini, capcutUpstreamModel20Mini, 15, 1.5},
}

// 每档 × 每分辨率 × 每比例：建单载荷必须**逐字段**等于请求值。
func TestCapcutParamMatrixAllModelsResolutionsRatios(t *testing.T) {
	ratios := []string{"16:9", "9:16", "1:1", "3:4", "4:3", "21:9"}
	resolutions := []string{"480p", "720p"}

	for _, m := range capcutMatrix {
		for _, res := range resolutions {
			for _, ratio := range ratios {
				name := fmt.Sprintf("%s/%s/%s", m.publicModel, res, ratio)
				t.Run(name, func(t *testing.T) {
					c, info, a := postVideoCtx(t, fmt.Sprintf(
						`{"model":%q,"prompt":"hi","seconds":5,"ratio":%q,"resolution":%q}`,
						m.publicModel, ratio, res))
					require.Nil(t, a.ValidateRequestAndSetAction(c, info))

					reader, err := a.BuildRequestBody(c, info)
					require.NoError(t, err)
					data, err := io.ReadAll(reader)
					require.NoError(t, err)

					var payload map[string]any
					require.NoError(t, json.Unmarshal(data, &payload))
					vp, ok := payload["videoParams"].(map[string]any)
					require.True(t, ok, name)

					assert.Equal(t, m.upstreamModel, vp["model"], name)
					assert.Equal(t, float64(5), vp["duration"], name)
					assert.Equal(t, ratio, vp["ratio"], name)
					assert.Equal(t, res, vp["resolution"], name)
				})
			}
		}
	}
}

// 时长边界矩阵：上界与上界+1 两侧必须给出**相反**的结果，
// 且边界值原样透传（不吸附成 15/30、不回落默认 5）。
func TestCapcutDurationBoundaryMatrix(t *testing.T) {
	for _, m := range capcutMatrix {
		t.Run(m.publicModel, func(t *testing.T) {
			// 上界：必须通过，且原样透传
			c, info, a := postVideoCtx(t, fmt.Sprintf(
				`{"model":%q,"prompt":"hi","seconds":%d,"resolution":"480p"}`,
				m.publicModel, m.maxDuration))
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			reader, err := a.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(data, &payload))
			vp := payload["videoParams"].(map[string]any)
			assert.Equal(t, float64(m.maxDuration), vp["duration"],
				"%s 上界 %d 必须原样透传", m.publicModel, m.maxDuration)

			// 上界+1：必须显式失败（不吸附、不回落）
			c2, info2, a2 := postVideoCtx(t, fmt.Sprintf(
				`{"model":%q,"prompt":"hi","seconds":%d,"resolution":"480p"}`,
				m.publicModel, m.maxDuration+1))
			err2 := a2.ValidateRequestAndSetAction(c2, info2)
			require.NotNil(t, err2, "%s 超上界必须显式失败", m.publicModel)
			assert.Contains(t, err2.Message, "seconds",
				"%s 超上界的错误必须点名 seconds", m.publicModel)
		})
	}
}

// 下界矩阵：4 秒必须通过；3 秒与 0 秒必须显式失败。
func TestCapcutDurationLowerBoundaryMatrix(t *testing.T) {
	for _, m := range capcutMatrix {
		t.Run(m.publicModel, func(t *testing.T) {
			c, info, a := postVideoCtx(t, fmt.Sprintf(
				`{"model":%q,"prompt":"hi","seconds":4,"resolution":"480p"}`, m.publicModel))
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			reader, err := a.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(data, &payload))
			vp := payload["videoParams"].(map[string]any)
			assert.Equal(t, float64(4), vp["duration"])

			for _, bad := range []int{3, 0, -1} {
				c2, info2, a2 := postVideoCtx(t, fmt.Sprintf(
					`{"model":%q,"prompt":"hi","seconds":%d,"resolution":"480p"}`, m.publicModel, bad))
				err2 := a2.ValidateRequestAndSetAction(c2, info2)
				require.NotNil(t, err2, "%s seconds=%d 必须显式失败", m.publicModel, bad)
			}
		})
	}
}

// 参考图张数矩阵：9 张通过，10 张显式 400（不静默截断）。
func TestCapcutReferenceImageCountBoundary(t *testing.T) {
	for _, m := range capcutMatrix {
		t.Run(m.publicModel, func(t *testing.T) {
			imgs := make([]string, 0, 10)
			for i := 0; i < 10; i++ {
				imgs = append(imgs, fmt.Sprintf(`"https://example.com/%s/%d.png"`, m.publicModel, i))
			}
			// 9 张：通过，且参考图数量原样下发
			body9 := fmt.Sprintf(
				`{"model":%q,"prompt":"hi","seconds":5,"resolution":"480p","images":[%s]}`,
				m.publicModel, strings.Join(imgs[:9], ","))
			c, info, a := postVideoCtx(t, body9)
			require.Nil(t, a.ValidateRequestAndSetAction(c, info))
			reader, err := a.BuildRequestBody(c, info)
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, json.Unmarshal(data, &payload))
			inputs, ok := payload["inputs"].([]any)
			require.True(t, ok, "inputs 必须下发")
			assert.Len(t, inputs, 9, "%s 9 张参考图必须原样下发", m.publicModel)

			// 10 张：显式失败
			body10 := fmt.Sprintf(
				`{"model":%q,"prompt":"hi","seconds":5,"resolution":"480p","images":[%s]}`,
				m.publicModel, strings.Join(imgs, ","))
			c2, info2, a2 := postVideoCtx(t, body10)
			err2 := a2.ValidateRequestAndSetAction(c2, info2)
			require.NotNil(t, err2, "%s 10 张参考图必须显式失败", m.publicModel)
		})
	}
}

// 分辨率矩阵：只卖 480p / 720p；1080p / 4K / 空串以外的串必须显式失败，
// 且**不得**静默降级成 480p（降级 = 客户按 1080p 付钱拿到 480p 成片）。
func TestCapcutResolutionMatrixRejectsUnpriced(t *testing.T) {
	for _, m := range capcutMatrix {
		for _, bad := range []string{"1080p", "4k", "720", "480"} {
			t.Run(fmt.Sprintf("%s/%s", m.publicModel, bad), func(t *testing.T) {
				c, info, a := postVideoCtx(t, fmt.Sprintf(
					`{"model":%q,"prompt":"hi","seconds":5,"resolution":%q}`, m.publicModel, bad))
				err := a.ValidateRequestAndSetAction(c, info)
				require.NotNil(t, err, "%s resolution=%s 必须显式失败", m.publicModel, bad)
				assert.Contains(t, err.Message, "resolution",
					"%s 未定价分辨率的错误必须点名 resolution", m.publicModel)
			})
		}
	}
}

// 比例白名单矩阵：白名单外比例必须显式失败（官网会静默改值）。
func TestCapcutRatioMatrixRejectsUnknown(t *testing.T) {
	for _, m := range capcutMatrix {
		for _, bad := range []string{"7:5", "5:4", "2:1", "nonsense"} {
			t.Run(fmt.Sprintf("%s/%s", m.publicModel, bad), func(t *testing.T) {
				c, info, a := postVideoCtx(t, fmt.Sprintf(
					`{"model":%q,"prompt":"hi","seconds":5,"ratio":%q}`, m.publicModel, bad))
				err := a.ValidateRequestAndSetAction(c, info)
				require.NotNil(t, err, "%s ratio=%s 必须显式失败", m.publicModel, bad)
			})
		}
	}
}

// 未定价模型不能被当成 CapCut 档位接单（防止拼错模型名落到别的平台）。
func TestCapcutUnknownModelNotRoutedToCapcut(t *testing.T) {
	for _, bad := range []string{"cp-seedance-3.0", "cp-seedance-2.0mini-fast", "seedance_2.0_mini"} {
		t.Run(bad, func(t *testing.T) {
			if _, ok := SpecFor(bad); ok {
				spec, _ := SpecFor(bad)
				assert.NotEqual(t, PlatformIDCapcut, spec.PlatformID,
					"%s 不该被路由到 capcut", bad)
			}
		})
	}
}
