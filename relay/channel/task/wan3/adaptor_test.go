package wan3

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolveDurationUsesSecondsWhenDurationIsAbsent(t *testing.T) {
	require.Equal(t, 12, resolveDuration(relaycommon.TaskSubmitReq{Seconds: "12"}))
}

func TestResolveResolutionAcceptsTopLevelResolution(t *testing.T) {
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Resolution: "720p"}))
}

func TestEstimateBillingChargesWan3ResolutionRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Duration:   5,
		Resolution: "720P",
	})

	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    5.2 / 2.66,
	}, adaptor.EstimateBilling(context, nil))
}
