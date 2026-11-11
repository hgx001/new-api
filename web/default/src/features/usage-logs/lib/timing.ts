/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the License,
or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { TIMING_LOG_TYPES } from '../constants'

/**
 * 耗时展示的两种口径。
 *
 * - `task`   任务真实完成时长（task_duration）：从提交到拿到成片。
 * - `submit` 提交往返耗时（use_time）：文本类请求的正常耗时，
 *            任务类请求在**还没跑完**时也显示这个（此时只知道提交花了多久）。
 */
export type TimingKind = 'task' | 'submit'

export interface TimingDisplay {
  /** 是否应该显示耗时这一列的内容 */
  displayable: boolean
  /** 要显示的秒数 */
  seconds: number
  /** 用的哪个口径 */
  kind: TimingKind
}

export interface TimingSource {
  type: number
  useTime?: number | null
  taskDuration?: number | null
}

const NOT_DISPLAYABLE: TimingDisplay = {
  displayable: false,
  seconds: 0,
  kind: 'submit',
}

/**
 * 解析一条日志的耗时展示内容。
 *
 * 为什么任务类请求要优先用 task_duration：
 * 消费日志（type=2）在**提交那一刻**就写了，那时上游只回了「已受理」，
 * use_time 记的是提交往返耗时（生产实测普遍 2.0s）。用户看到 2.0s 会以为
 * 视频 2 秒就生成了，而实际上游要跑 1~3 分钟。
 *
 * task_duration 由后端在任务进入终态时回填（FinishTime - SubmitTime），
 * 是用户真正体感的等待时间。
 *
 * 任务还在跑时 task_duration 还是 0，此时回落到 use_time ——
 * 总比整列空白好，而且语义上也不算错（提交确实花了这么久）。
 */
export function resolveTimingDisplay(source: TimingSource): TimingDisplay {
  if (!TIMING_LOG_TYPES.includes(source.type as 2 | 5)) {
    return NOT_DISPLAYABLE
  }

  const taskDuration = source.taskDuration ?? 0
  const useTime = source.useTime ?? 0

  if (taskDuration > 0) {
    return { displayable: true, seconds: taskDuration, kind: 'task' }
  }
  if (useTime > 0) {
    return { displayable: true, seconds: useTime, kind: 'submit' }
  }
  return NOT_DISPLAYABLE
}
