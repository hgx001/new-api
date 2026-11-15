package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetTaskHealthSummary 返回各模型的异步任务健康度（成功率 / 平均完成耗时 / 趋势）。
//
// 这是模型广场的"每个模型最近用得怎么样"面板的数据源：
// 异步任务（视频、音频、图片任务协议）的完成时间只有在任务跑完才知道，
// 不在 perf_metrics 那张同步采样表里，必须回 tasks 表统计。
//
// 口径说明（与 model.GetTaskHealthSummary 一致）：
//   - 只统计已完成任务（SUCCESS / FAILURE），在途任务不进分母
//   - 平均完成耗时只统计成功任务
//   - 模型名按计费口径归一（改派过的任务算回用户请求的源模型）
//
// hours 参数默认 24，上限 30 天（与 perf-metrics 一致，避免慢查询）。
func GetTaskHealthSummary(c *gin.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}
	if hours <= 0 {
		hours = 24
	}
	if hours > 24*30 {
		hours = 24 * 30
	}

	endTs := time.Now().Unix()
	startTs := endTs - int64(hours)*3600

	summary, err := model.GetTaskHealthSummary(model.TaskHealthQueryParams{
		StartTs:       startTs,
		EndTs:         endTs,
		BucketSeconds: 3600,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if summary == nil {
		summary = &model.TaskHealthSummary{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    summary,
	})
}
