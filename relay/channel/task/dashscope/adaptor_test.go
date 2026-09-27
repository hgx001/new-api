package dashscope

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolveDurationUsesSecondsWhenDurationIsAbsent(t *testing.T) {
	adaptor := &TaskAdaptor{}

	require.Equal(t, 12, adaptor.resolveDuration(relaycommon.TaskSubmitReq{Seconds: "12"}))
	require.Equal(t, 2, adaptor.resolveDuration(relaycommon.TaskSubmitReq{Seconds: "1"}))
	require.Equal(t, 30, adaptor.resolveDuration(relaycommon.TaskSubmitReq{Seconds: "31"}))
}

func TestResolveResolutionAcceptsTopLevelResolution(t *testing.T) {
	adaptor := &TaskAdaptor{}

	require.Equal(t, "720P", adaptor.resolveResolution(relaycommon.TaskSubmitReq{Resolution: "720p"}))
	require.Equal(t, "720P", adaptor.resolveResolution(relaycommon.TaskSubmitReq{
		Resolution: "invalid",
		Metadata:   map[string]interface{}{"resolution": "720P"},
	}))
}

func TestResolveResolutionAccepts1080P(t *testing.T) {
	adaptor := &TaskAdaptor{}

	require.Equal(t, "1080P", adaptor.resolveResolution(relaycommon.TaskSubmitReq{Resolution: "1080p"}))
	require.Equal(t, "1080P", adaptor.resolveResolution(relaycommon.TaskSubmitReq{Size: "1920x1080"}))
	require.Equal(t, "1080P", adaptor.resolveResolution(relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{"resolution": "1080P"},
	}))
}

func TestEstimateBillingChargesResolutionTiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	cases := []struct {
		resolution string
		wantSize   float64
	}{
		// 对外售价 480P ¥0.27/秒、720P ¥0.53/秒、1080P ¥0.85/秒，基准价为 480P。
		{"480P", 1.0},
		{"720P", 5.2 / 2.66},
		{"1080P", 0.85 / 0.27},
	}
	for _, tc := range cases {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", relaycommon.TaskSubmitReq{
			Duration:   5,
			Resolution: tc.resolution,
		})
		require.Equal(t, map[string]float64{
			"seconds": 5,
			"size":    tc.wantSize,
		}, adaptor.EstimateBilling(context, &relaycommon.RelayInfo{}), "resolution=%s", tc.resolution)
	}
}
