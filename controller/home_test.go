package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetHomeMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/home/metrics", nil)

	GetHomeMetrics(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)

	var payload struct {
		Success bool                `json:"success"`
		Data    HomeMetricsResponse `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)

	assert.Equal(t, "active", payload.Data.SystemStatus)
	assert.Equal(t, "99.99%", payload.Data.Availability)
	assert.Equal(t, "1.2M+", payload.Data.Throughput)
	assert.Equal(t, "RPM", payload.Data.ThroughputUnit)
	assert.Equal(t, "24ms", payload.Data.Latency)
	assert.Equal(t, "AES-256 Enterprise Standard", payload.Data.Encryption)
	assert.Equal(t, "ISO 27001", payload.Data.Certification)
}
