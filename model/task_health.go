package model

import (
	"sort"
	"time"
)

// ---------------------------------------------------------------------------
// 任务健康度聚合（模型广场用）
// ---------------------------------------------------------------------------
//
// 目标：让用户在模型广场看到每个模型「最近一段时间的请求成功率 + 平均完成耗时」。
//
// 为什么不用 perf_metrics 那张表：
//
//	perf_metrics 记的是**同步 relay**（LLM / 图片）的即时采样，异步任务（视频、音频、
// 图片任务协议）根本不写这张表 —— 它的完成时间是"任务跑完才知道"，不是一次调用返回。
// 异步模型的成功率必须回到 tasks 表看，否则模型广场会把视频模型显示成"零请求"。
//
// 为什么在 Go 层聚合而不是写 SQL：
//
//	tasks.properties / tasks.private_data 是 JSON 列，模型名要按**计费口径**取
//	（改派过的任务用 private_data.route_origin_model，没改派的用
//	 properties.origin_model_name / billing_context.origin_model_name）。
//	SQLite / MySQL / PostgreSQL 三库的 JSON 取值函数互不相同
//	（json_extract / JSON_EXTRACT / ->>），没有一条 SQL 能同时跑通三库，
//	所以这里取行后在 Go 里算，取值口径直接复用 service.taskBillingModelName 的规则。
//
// 口径约定：
//
//   - **成功** = status = SUCCESS；**失败** = status = FAILURE。
//     仍在排队/生成中的（QUEUED / SUBMITTED / IN_PROGRESS）**既不算成功也不算失败**，
//     只计入"未完成"，不进分母 —— 否则成功率会被在途任务稀释，且数字会一直跳动。
//   - **平均完成耗时** = finish_time - submit_time，只统计**成功**的任务。
//     失败任务的耗时没有可比性（多半是秒回），混进来会拉低均值、误导用户。
//     submit_time / finish_time 都是秒级时间戳，相减得到的是**秒**，输出时统一乘 1000 换成毫秒。
//   - **趋势桶**按小时切，桶内成功率 = 该桶成功数 / (成功数 + 失败数)。
//     没有已完成任务的桶输出 null，前端画成灰色"无样本"，不画成 0%（0% 会被误读成全挂）。
//
// 数据源有两路，合并进同一套口径：
//
//  1. tasks 表：异步任务（视频 / 音频 / 任务协议图片）的最终成败与完成耗时。
//  2. logs 表：**同步请求**（chat、/v1/images/generations 这类一次调用返回的）
//     走 type=2（消费成功）与 type=5（错误）日志，use_time 即耗时；
//     以及异步任务**提交阶段**就失败的请求 —— 它们没建出 task 行，只有 error log，
//     不算进来会把视频模型的提交失败漏掉。
//     logs 里带 task_id 的行是任务提交时写的消费日志，其成败已由 tasks 表统计，
//     必须排除，否则同一单会重复计数。

// TaskHealthBucket 是趋势里的一个小时桶。
type TaskHealthBucket struct {
	// BucketTs 是桶起始时间戳（秒，UTC 对齐整点）。
	BucketTs int64 `json:"bucket_ts"`
	// SuccessCount / FailureCount 是该桶内的成功、失败数。
	SuccessCount int64 `json:"success_count"`
	FailureCount int64 `json:"failure_count"`
	// SuccessRate 是该桶成功率（0-100）。样本不足时为 nil（无样本 ≠ 0%）。
	SuccessRate *float64 `json:"success_rate"`
	// AvgDurationMs 是该桶内成功任务的平均完成耗时（毫秒）。
	AvgDurationMs int64 `json:"avg_duration_ms"`
	// durationSumSec / durationCount 是桶内成功任务耗时（秒）的累加与计数，
	// 只用于聚合过程，不对外输出。
	durationSumSec int64
	durationCount  int64
}

// TaskHealthModel 是单个模型的健康度汇总。
type TaskHealthModel struct {
	// ModelName 是**计费口径**的模型名（客户请求的那个），与扣费口径一致。
	ModelName string `json:"model_name"`
	// RequestCount 是统计窗口内已完成任务总数（成功 + 失败）。
	RequestCount int64 `json:"request_count"`
	// SuccessCount / FailureCount 分别是成功、失败数。
	SuccessCount int64 `json:"success_count"`
	FailureCount int64 `json:"failure_count"`
	// SuccessRate 是窗口内成功率（0-100）。没有已完成任务时为 nil。
	SuccessRate *float64 `json:"success_rate"`
	// AvgDurationMs 是成功任务的平均完成耗时（毫秒）。没有成功任务时为 0。
	AvgDurationMs int64 `json:"avg_duration_ms"`
	// Buckets 是按时间升序排列的每小时趋势桶。
	Buckets []TaskHealthBucket `json:"buckets"`
}

// TaskHealthSummary 是本次查询的全部结果。
type TaskHealthSummary struct {
	// Models 按请求量降序排列。
	Models []TaskHealthModel `json:"models"`
	// Hours 是实际统计窗口长度（小时）。
	Hours int `json:"hours"`
	// BucketSeconds 是趋势桶宽度（秒）。
	BucketSeconds int64 `json:"bucket_seconds"`
}

// TaskHealthQueryParams 描述一次任务健康度查询。
type TaskHealthQueryParams struct {
	// StartTs / EndTs 是统计窗口的起止时间戳（秒）。
	StartTs int64
	EndTs   int64
	// BucketSeconds 是趋势桶宽度（秒）；<= 0 时用 3600。
	BucketSeconds int64
}

// healthAgg 是单个模型在聚合过程中的累加器。
type healthAgg struct {
	modelName      string
	successCount   int64
	failureCount   int64
	durationSumSec int64
	durationCount  int64
	buckets        map[int64]*TaskHealthBucket
}

// GetTaskHealthSummary 统计窗口内的任务健康度。
//
// 只取「已完成」的任务（SUCCESS / FAILURE）：在途任务没有最终结论，计进分母会
// 让成功率随在途量漂移。模型名按计费口径归一，改派过的任务算回用户请求的源模型。
func GetTaskHealthSummary(params TaskHealthQueryParams) (*TaskHealthSummary, error) {
	bucketSeconds := params.BucketSeconds
	if bucketSeconds <= 0 {
		bucketSeconds = 3600
	}
	if params.EndTs <= params.StartTs {
		return nil, nil
	}

	var tasks []Task
	err := DB.Model(&Task{}).
		Select("status, submit_time, finish_time, properties, private_data").
		Where("status IN ?", []TaskStatus{TaskStatusSuccess, TaskStatusFailure}).
		Where("created_at >= ? AND created_at <= ?", params.StartTs, params.EndTs).
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}

	// 同步请求（chat / 图片直出等）与任务提交失败的记录走 logs：
	// type=2 消费成功、type=5 错误；带 task_id 的行是任务提交时写的消费日志，
	// 其成败已由 tasks 表统计，排除以免重复计数。
	var logs []Log
	err = LOG_DB.Model(&Log{}).
		Select("type, model_name, use_time, created_at").
		Where("type IN ?", []int{LogTypeConsume, LogTypeError}).
		Where("(task_id IS NULL OR task_id = '')").
		Where("created_at >= ? AND created_at <= ?", params.StartTs, params.EndTs).
		Find(&logs).Error
	if err != nil {
		return nil, err
	}

	summary := &TaskHealthSummary{
		Models:        make([]TaskHealthModel, 0, len(tasks)+len(logs)),
		Hours:         int((params.EndTs - params.StartTs) / 3600),
		BucketSeconds: bucketSeconds,
	}
	if len(tasks) == 0 && len(logs) == 0 {
		return summary, nil
	}

	aggByModel := map[string]*healthAgg{}
	record := func(modelName string, isSuccess bool, durationSec int64, eventTs int64) {
		if modelName == "" {
			return
		}
		current, ok := aggByModel[modelName]
		if !ok {
			current = &healthAgg{
				modelName: modelName,
				buckets:   map[int64]*TaskHealthBucket{},
			}
			aggByModel[modelName] = current
		}

		if isSuccess {
			current.successCount++
		} else {
			current.failureCount++
		}

		// 完成耗时只统计成功请求：失败多是秒回，混进均值会拉低数字、误导用户。
		// tasks 的 submit/finish 与 logs 的 use_time 都是**秒级**，最后统一换算成毫秒。
		if isSuccess && durationSec > 0 {
			current.durationSumSec += durationSec
			current.durationCount++
		}

		bucketTs := eventTs - (eventTs % bucketSeconds)
		if bucketTs < params.StartTs {
			bucketTs = params.StartTs - (params.StartTs % bucketSeconds)
		}
		bucket, ok := current.buckets[bucketTs]
		if !ok {
			bucket = &TaskHealthBucket{BucketTs: bucketTs}
			current.buckets[bucketTs] = bucket
		}
		if isSuccess {
			bucket.SuccessCount++
			if durationSec > 0 {
				bucket.durationSumSec += durationSec
				bucket.durationCount++
			}
		} else {
			bucket.FailureCount++
		}
	}

	for _, task := range tasks {
		durationSec := task.FinishTime - task.SubmitTime
		if durationSec < 0 {
			durationSec = 0
		}
		record(taskHealthModelName(&task), task.Status == TaskStatusSuccess, durationSec, task.SubmitTime)
	}
	for _, entry := range logs {
		record(entry.ModelName, entry.Type == LogTypeConsume, int64(entry.UseTime), entry.CreatedAt)
	}

	for _, current := range aggByModel {
		model := TaskHealthModel{
			ModelName:    current.modelName,
			SuccessCount: current.successCount,
			FailureCount: current.failureCount,
			RequestCount: current.successCount + current.failureCount,
		}
		if current.durationCount > 0 {
			model.AvgDurationMs = current.durationSumSec * 1000 / current.durationCount
		}

		completed := current.successCount + current.failureCount
		if completed > 0 {
			rate := float64(current.successCount) / float64(completed) * 100
			model.SuccessRate = &rate
		}

		bucketTimestamps := make([]int64, 0, len(current.buckets))
		for ts := range current.buckets {
			bucketTimestamps = append(bucketTimestamps, ts)
		}
		sort.Slice(bucketTimestamps, func(i, j int) bool {
			return bucketTimestamps[i] < bucketTimestamps[j]
		})
		model.Buckets = make([]TaskHealthBucket, 0, len(bucketTimestamps))
		for _, ts := range bucketTimestamps {
			bucket := current.buckets[ts]
			bucketDone := bucket.SuccessCount + bucket.FailureCount
			if bucketDone > 0 {
				rate := float64(bucket.SuccessCount) / float64(bucketDone) * 100
				bucket.SuccessRate = &rate
			}
			if bucket.durationCount > 0 {
				// 桶内平均耗时 = 该桶成功任务耗时之和 / 有效样本数（秒 → 毫秒）。
				bucket.AvgDurationMs = bucket.durationSumSec * 1000 / bucket.durationCount
			}
			model.Buckets = append(model.Buckets, *bucket)
		}

		summary.Models = append(summary.Models, model)
	}

	sort.Slice(summary.Models, func(i, j int) bool {
		if summary.Models[i].RequestCount != summary.Models[j].RequestCount {
			return summary.Models[i].RequestCount > summary.Models[j].RequestCount
		}
		return summary.Models[i].ModelName < summary.Models[j].ModelName
	})

	return summary, nil
}

// taskHealthModelName 按**计费口径**取任务的模型名。
//
// 与 service.taskBillingModelName 的规则保持一致：
//  1. 条件路由改派过的任务 → private_data.route_origin_model（用户请求的源模型）
//  2. 其次 private_data.billing_context.origin_model_name
//  3. 最后 properties.origin_model_name
//
// 这里刻意不复用 service 包（model 不能 import service，会成环），而是按同一顺序
// 重写一遍；改口径时两处要一起改。
func taskHealthModelName(task *Task) string {
	if task == nil {
		return ""
	}
	if task.PrivateData.RouteOriginModel != "" {
		return task.PrivateData.RouteOriginModel
	}
	if billingContext := task.PrivateData.BillingContext; billingContext != nil && billingContext.OriginModelName != "" {
		return billingContext.OriginModelName
	}
	return task.Properties.OriginModelName
}

// TaskHealthWindowStart 返回统计窗口起点。
func TaskHealthWindowStart(hours int) int64 {
	if hours <= 0 {
		hours = 24
	}
	return time.Now().Add(-time.Duration(hours) * time.Hour).Unix()
}
