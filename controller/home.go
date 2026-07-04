package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HomeMetricsResponse struct {
	SystemStatus   string `json:"system_status"`
	Availability   string `json:"availability"`
	Throughput     string `json:"throughput"`
	ThroughputUnit string `json:"throughput_unit"`
	Latency        string `json:"latency"`
	Encryption     string `json:"encryption"`
	Certification  string `json:"certification"`
}

func GetHomeMetrics(c *gin.Context) {
	// TODO: replace static values with real aggregated metrics from performance/perf_metrics/uptime_kuma
	data := HomeMetricsResponse{
		SystemStatus:   "active",
		Availability:   "99.99%",
		Throughput:     "1.2M+",
		ThroughputUnit: "RPM",
		Latency:        "24ms",
		Encryption:     "AES-256 Enterprise Standard",
		Certification:  "ISO 27001",
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}
