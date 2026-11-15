package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 任务健康度聚合（模型广场面板数据源）
// ---------------------------------------------------------------------------

// seedTaskHealth 插一条任务。createdAt 显式指定，用来控制统计窗口。
func seedTaskHealth(t *testing.T, taskID string, status TaskStatus, createdAt int64,
	submitTime int64, finishTime int64, props Properties, priv TaskPrivateData) {
	t.Helper()
	task := &Task{
		TaskID:      taskID,
		Status:      status,
		SubmitTime:  submitTime,
		StartTime:   submitTime,
		FinishTime:  finishTime,
		Properties:  props,
		PrivateData: priv,
		Data:        []byte(`{}`),
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
	require.NoError(t, DB.Create(task).Error)
}

func findHealthModel(summary *TaskHealthSummary, name string) *TaskHealthModel {
	if summary == nil {
		return nil
	}
	for i := range summary.Models {
		if summary.Models[i].ModelName == name {
			return &summary.Models[i]
		}
	}
	return nil
}

func TestTaskHealthModelName_BillingPriority(t *testing.T) {
	truncateTables(t)

	// 改派过的任务：route_origin_model 优先（计费口径 = 用户请求的源模型）。
	assert.Equal(t, "seedance2.5-特惠", taskHealthModelName(&Task{
		PrivateData: TaskPrivateData{RouteOriginModel: "seedance2.5-特惠"},
		Properties:  Properties{OriginModelName: "wan3.0-video"},
	}))

	// 未改派但有计费上下文：取 billing_context.origin_model_name。
	assert.Equal(t, "billing-model", taskHealthModelName(&Task{
		PrivateData: TaskPrivateData{
			BillingContext: &TaskBillingContext{OriginModelName: "billing-model"},
		},
		Properties: Properties{OriginModelName: "properties-model"},
	}))

	// 兜底：properties.origin_model_name。
	assert.Equal(t, "properties-model", taskHealthModelName(&Task{
		Properties: Properties{OriginModelName: "properties-model"},
	}))

	// 全空 → 空串（聚合时会被跳过）。
	assert.Equal(t, "", taskHealthModelName(&Task{}))
	assert.Equal(t, "", taskHealthModelName(nil))
}

func TestGetTaskHealthSummary_SuccessRateExcludesInFlight(t *testing.T) {
	truncateTables(t)

	// 确定性基准时间：相对时间会随跑测时刻跨整点，导致桶归属不稳定。
	base := int64(1_700_000_000)
	base -= base % 3600
	now := base + 3600
	start := base

	// 已完成：3 成功 1 失败 → 成功率 75%
	seedTaskHealth(t, "h1", TaskStatusSuccess, now-100, now-100, now-60,
		Properties{OriginModelName: "video-a"}, TaskPrivateData{})
	seedTaskHealth(t, "h2", TaskStatusSuccess, now-100, now-100, now-50,
		Properties{OriginModelName: "video-a"}, TaskPrivateData{})
	seedTaskHealth(t, "h3", TaskStatusSuccess, now-100, now-100, now-40,
		Properties{OriginModelName: "video-a"}, TaskPrivateData{})
	seedTaskHealth(t, "h4", TaskStatusFailure, now-100, now-100, now-30,
		Properties{OriginModelName: "video-a"}, TaskPrivateData{})

	// 在途任务：不进分母
	seedTaskHealth(t, "h5", TaskStatusQueued, now-100, now-100, 0,
		Properties{OriginModelName: "video-a"}, TaskPrivateData{})
	seedTaskHealth(t, "h6", TaskStatusInProgress, now-100, now-100, 0,
		Properties{OriginModelName: "video-a"}, TaskPrivateData{})
	seedTaskHealth(t, "h7", TaskStatusSubmitted, now-100, now-100, 0,
		Properties{OriginModelName: "video-a"}, TaskPrivateData{})

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)

	model := findHealthModel(summary, "video-a")
	require.NotNil(t, model)
	assert.EqualValues(t, 4, model.RequestCount, "在途任务不应计入请求数")
	assert.EqualValues(t, 3, model.SuccessCount)
	assert.EqualValues(t, 1, model.FailureCount)
	require.NotNil(t, model.SuccessRate)
	assert.InDelta(t, 75.0, *model.SuccessRate, 0.001)
}

func TestGetTaskHealthSummary_AvgDurationOnlySuccess(t *testing.T) {
	truncateTables(t)

	// 确定性基准时间：相对时间会随跑测时刻跨整点，导致桶归属不稳定。
	base := int64(1_700_000_000)
	base -= base % 3600
	now := base + 3600
	start := base

	// 成功任务耗时 100s 和 300s → 均值 200s = 200000ms
	seedTaskHealth(t, "d1", TaskStatusSuccess, now-500, now-500, now-400,
		Properties{OriginModelName: "video-b"}, TaskPrivateData{})
	seedTaskHealth(t, "d2", TaskStatusSuccess, now-500, now-500, now-200,
		Properties{OriginModelName: "video-b"}, TaskPrivateData{})

	// 失败任务耗时 1s：混进均值会得到 133s，明显偏低，必须排除
	seedTaskHealth(t, "d3", TaskStatusFailure, now-500, now-500, now-499,
		Properties{OriginModelName: "video-b"}, TaskPrivateData{})

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)

	model := findHealthModel(summary, "video-b")
	require.NotNil(t, model)
	assert.EqualValues(t, 200000, model.AvgDurationMs, "平均耗时只能统计成功任务")
}

func TestGetTaskHealthSummary_RouteOriginModelAttribution(t *testing.T) {
	truncateTables(t)

	// 确定性基准时间：相对时间会随跑测时刻跨整点，导致桶归属不稳定。
	base := int64(1_700_000_000)
	base -= base % 3600
	now := base + 3600
	start := base

	// 改派任务：properties 记的是改派后模型，private_data 记的是用户请求的源模型。
	seedTaskHealth(t, "r1", TaskStatusSuccess, now-100, now-100, now-50,
		Properties{OriginModelName: "wan3.0-video"},
		TaskPrivateData{RouteOriginModel: "seedance2.5-特惠"})

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)

	// 应当算在用户请求的源模型上，而不是改派后的模型
	assert.NotNil(t, findHealthModel(summary, "seedance2.5-特惠"))
	assert.Nil(t, findHealthModel(summary, "wan3.0-video"))
}

func TestGetTaskHealthSummary_BucketAlignment(t *testing.T) {
	truncateTables(t)

	// 用**确定性**的整点时间戳：相对时间会随跑测时刻跨整点，导致两个样本落进同一个桶。
	base := int64(1_700_000_000)
	base -= base % 3600 // 对齐整点
	end := base + 3*3600
	start := base - 3600

	// 两个不同小时的桶
	seedTaskHealth(t, "b1", TaskStatusSuccess, base, base, base+100,
		Properties{OriginModelName: "video-c"}, TaskPrivateData{})
	seedTaskHealth(t, "b2", TaskStatusFailure, base+2*3600, base+2*3600, base+2*3600+30,
		Properties{OriginModelName: "video-c"}, TaskPrivateData{})

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: end, BucketSeconds: 3600,
	})
	require.NoError(t, err)

	model := findHealthModel(summary, "video-c")
	require.NotNil(t, model)
	require.Len(t, model.Buckets, 2, "两个不同时段的样本应落在两个桶里")

	// 桶按时间升序
	assert.Less(t, model.Buckets[0].BucketTs, model.Buckets[1].BucketTs)

	// 桶时间对齐整点
	assert.Equal(t, int64(0), model.Buckets[0].BucketTs%3600)
	assert.Equal(t, int64(0), model.Buckets[1].BucketTs%3600)

	// 每桶成功率：成功桶 100%，失败桶 0%
	require.NotNil(t, model.Buckets[0].SuccessRate)
	assert.InDelta(t, 100.0, *model.Buckets[0].SuccessRate, 0.001)
	require.NotNil(t, model.Buckets[1].SuccessRate)
	assert.InDelta(t, 0.0, *model.Buckets[1].SuccessRate, 0.001)

	// 桶内平均耗时单位是毫秒：成功任务耗时 100s → 100000ms
	assert.EqualValues(t, 100000, model.Buckets[0].AvgDurationMs)
	// 失败桶没有成功样本 → 0
	assert.EqualValues(t, 0, model.Buckets[1].AvgDurationMs)
}

func TestGetTaskHealthSummary_NoSamplesYieldsNilRate(t *testing.T) {
	truncateTables(t)

	// 确定性基准时间：相对时间会随跑测时刻跨整点，导致桶归属不稳定。
	base := int64(1_700_000_000)
	base -= base % 3600
	now := base + 3600
	start := base

	// 只有在途任务 → 没有任何已完成样本
	seedTaskHealth(t, "n1", TaskStatusInProgress, now-100, now-100, 0,
		Properties{OriginModelName: "video-d"}, TaskPrivateData{})

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)

	// 模型名有值但没有已完成任务：不应出现在结果里（没有可展示的健康度）
	assert.Nil(t, findHealthModel(summary, "video-d"))
	assert.Empty(t, summary.Models)
}

func TestGetTaskHealthSummary_EmptyWindow(t *testing.T) {
	truncateTables(t)

	now := time.Now().Unix()

	// 窗口非法 → 返回 nil，不报错
	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: now, EndTs: now - 100, BucketSeconds: 3600,
	})
	require.NoError(t, err)
	assert.Nil(t, summary)

	// 窗口合法但无数据 → 返回空 summary，不返回 nil
	summary, err = GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: now - 3600, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Empty(t, summary.Models)
	assert.Equal(t, 1, summary.Hours)
	assert.EqualValues(t, 3600, summary.BucketSeconds)
}

func TestGetTaskHealthSummary_SortsByRequestCountDesc(t *testing.T) {
	truncateTables(t)

	// 确定性基准时间：相对时间会随跑测时刻跨整点，导致桶归属不稳定。
	base := int64(1_700_000_000)
	base -= base % 3600
	now := base + 3600
	start := base

	// hot 模型 2 条，cold 模型 1 条
	seedTaskHealth(t, "s1", TaskStatusSuccess, now-100, now-100, now-50,
		Properties{OriginModelName: "hot"}, TaskPrivateData{})
	seedTaskHealth(t, "s2", TaskStatusSuccess, now-100, now-100, now-50,
		Properties{OriginModelName: "hot"}, TaskPrivateData{})
	seedTaskHealth(t, "s3", TaskStatusSuccess, now-100, now-100, now-50,
		Properties{OriginModelName: "cold"}, TaskPrivateData{})

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)
	require.Len(t, summary.Models, 2)
	assert.Equal(t, "hot", summary.Models[0].ModelName)
	assert.Equal(t, "cold", summary.Models[1].ModelName)
}

func TestTaskHealthWindowStart(t *testing.T) {
	// hours <= 0 时兜底成 24 小时
	now := time.Now().Unix()
	assert.InDelta(t, now-24*3600, TaskHealthWindowStart(0), 5)
	assert.InDelta(t, now-24*3600, TaskHealthWindowStart(-5), 5)
	assert.InDelta(t, now-12*3600, TaskHealthWindowStart(12), 5)
}

// ---------------------------------------------------------------------------
// 同步请求（logs）合并统计
// ---------------------------------------------------------------------------

// seedSyncLog 插一条同步请求日志。taskID 为空表示同步请求，非空表示任务提交日志。
func seedSyncLog(t *testing.T, logType int, modelName string, taskID string, useTime int, createdAt int64) {
	t.Helper()
	require.NoError(t, DB.Create(&Log{
		UserId:    1,
		CreatedAt: createdAt,
		Type:      logType,
		ModelName: modelName,
		UseTime:   useTime,
		TaskId:    taskID,
		Other:     "{}",
	}).Error)
}

func TestGetTaskHealthSummary_MergesSyncRequestLogs(t *testing.T) {
	truncateTables(t)

	// 确定性基准时间：相对时间会随跑测时刻跨整点，导致桶归属不稳定。
	base := int64(1_700_000_000)
	base -= base % 3600
	now := base + 3600
	start := base

	// 图片模型走同步接口：logs 里 3 成功（type=2，带耗时）+ 1 失败（type=5）
	seedSyncLog(t, LogTypeConsume, "h3-image", "", 12, now-300)
	seedSyncLog(t, LogTypeConsume, "h3-image", "", 30, now-200)
	seedSyncLog(t, LogTypeConsume, "h3-image", "", 18, now-100)
	seedSyncLog(t, LogTypeError, "h3-image", "", 2, now-50)

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)

	model := findHealthModel(summary, "h3-image")
	require.NotNil(t, model, "同步图片模型也应出现在健康度统计里")
	assert.EqualValues(t, 4, model.RequestCount)
	assert.EqualValues(t, 3, model.SuccessCount)
	assert.EqualValues(t, 1, model.FailureCount)
	require.NotNil(t, model.SuccessRate)
	assert.InDelta(t, 75.0, *model.SuccessRate, 0.001)
	// 平均耗时 = (12+30+18)/3 = 20s = 20000ms，失败那条不计耗时
	assert.EqualValues(t, 20000, model.AvgDurationMs)
}

func TestGetTaskHealthSummary_ExcludesTaskBoundLogs(t *testing.T) {
	truncateTables(t)

	base := int64(1_700_000_000)
	base -= base % 3600
	now := base + 3600
	start := base

	// 任务本身：1 成功 1 失败
	seedTaskHealth(t, "t1", TaskStatusSuccess, now-100, now-100, now-40,
		Properties{OriginModelName: "video-d"}, TaskPrivateData{})
	seedTaskHealth(t, "t2", TaskStatusFailure, now-100, now-100, now-30,
		Properties{OriginModelName: "video-d"}, TaskPrivateData{})

	// 任务提交时写的消费日志（带 task_id）：成败已由 tasks 表统计，必须排除，
	// 否则同一单会被重复计数（3 成功 2 失败）。
	seedSyncLog(t, LogTypeConsume, "video-d", "task_x1", 2, now-100)
	seedSyncLog(t, LogTypeConsume, "video-d", "task_x2", 2, now-100)

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)

	model := findHealthModel(summary, "video-d")
	require.NotNil(t, model)
	assert.EqualValues(t, 2, model.RequestCount, "带 task_id 的日志不应重复计数")
	assert.EqualValues(t, 1, model.SuccessCount)
	assert.EqualValues(t, 1, model.FailureCount)
}

func TestGetTaskHealthSummary_MergesTaskSubmitFailures(t *testing.T) {
	truncateTables(t)

	base := int64(1_700_000_000)
	base -= base % 3600
	now := base + 3600
	start := base

	// 视频模型：任务表 2 成功；提交阶段另有 2 条失败（没建出 task 行，只有 error log）
	seedTaskHealth(t, "m1", TaskStatusSuccess, now-100, now-100, now-60,
		Properties{OriginModelName: "video-e"}, TaskPrivateData{})
	seedTaskHealth(t, "m2", TaskStatusSuccess, now-100, now-100, now-20,
		Properties{OriginModelName: "video-e"}, TaskPrivateData{})
	seedSyncLog(t, LogTypeError, "video-e", "", 1, now-300)
	seedSyncLog(t, LogTypeError, "video-e", "", 1, now-250)

	summary, err := GetTaskHealthSummary(TaskHealthQueryParams{
		StartTs: start, EndTs: now, BucketSeconds: 3600,
	})
	require.NoError(t, err)

	model := findHealthModel(summary, "video-e")
	require.NotNil(t, model)
	assert.EqualValues(t, 4, model.RequestCount, "提交阶段失败也应计入分母")
	assert.EqualValues(t, 2, model.SuccessCount)
	assert.EqualValues(t, 2, model.FailureCount)
	assert.InDelta(t, 50.0, *model.SuccessRate, 0.001)
}
