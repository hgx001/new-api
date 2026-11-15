/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
export type PerformanceSeriesPoint = {
  ts: number
  avg_ttft_ms: number
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
}

export type PerformanceGroup = {
  group: string
  avg_ttft_ms: number
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  series: PerformanceSeriesPoint[]
}

export type PerformanceMetricsData = {
  success: boolean
  message?: string
  data: {
    model_name: string
    series_schema?: string
    groups: PerformanceGroup[]
  }
}

export type PerfModelSummary = {
  model_name: string
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  recent_success_rates?: number[]
  request_count?: number
}

export type PerfSummaryAllData = {
  success: boolean
  message?: string
  data: {
    models: PerfModelSummary[]
  }
}

// ---------------------------------------------------------------------------
// 异步任务健康度（模型广场「最近用得怎么样」面板）
// ---------------------------------------------------------------------------
//
// 与上面的 PerformanceMetricsData 分开：前者是**同步 relay** 的即时采样
// （LLM / 图片），后者是**异步任务**（视频 / 音频 / 图片任务协议）的完成统计 ——
// 任务的完成时间只有跑完才知道，不在同步采样表里。

/** 趋势里的一个小时桶。 */
export type TaskHealthBucket = {
  /** 桶起始时间戳（秒，UTC 对齐整点）。 */
  bucket_ts: number
  success_count: number
  failure_count: number
  /**
   * 该桶成功率（0-100）。null = 该时段没有已完成任务（**无样本**，不是 0%）。
   * 前端必须区分这两者：把「无样本」画成红色会被误读成「全挂」。
   */
  success_rate: number | null
  /** 该桶内成功任务的平均完成耗时（毫秒）。 */
  avg_duration_ms: number
}

/** 单个模型的健康度汇总。 */
export type TaskHealthModel = {
  /** 计费口径的模型名（客户请求的那个），与扣费口径一致。 */
  model_name: string
  /** 窗口内已完成任务总数（成功 + 失败）。 */
  request_count: number
  success_count: number
  failure_count: number
  /** 窗口内成功率（0-100）。null = 窗口内没有已完成任务。 */
  success_rate: number | null
  /** 成功任务的平均完成耗时（毫秒）。0 = 没有成功样本。 */
  avg_duration_ms: number
  /** 按时间升序的每小时趋势桶。 */
  buckets: TaskHealthBucket[]
}

export type TaskHealthSummaryData = {
  success: boolean
  message?: string
  data: {
    models: TaskHealthModel[]
    /** 实际统计窗口长度（小时）。 */
    hours: number
    /** 趋势桶宽度（秒）。 */
    bucket_seconds: number
  }
}
