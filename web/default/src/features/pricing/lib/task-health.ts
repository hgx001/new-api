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

// ---------------------------------------------------------------------------
// 任务健康度面板的纯工具函数
// ---------------------------------------------------------------------------

/**
 * 模型卡片上的健康度整块是否该渲染。
 *
 * 口径：**没有已完成样本就不显示**（2026-11-20 产品口径）。空面板只会画出
 * 24 个灰块和「暂无完成请求」，对用户没有任何信息量 —— 广场上「有的卡有
 * 成功率、有的整块空白」正是这个原因，不是统计数据丢了。
 *
 * 注意区分两种"没有"：
 *   - 窗口内零样本（request_count = 0）→ 不显示；
 *   - 有样本但全失败（success_rate = 0）→ **必须显示**，那是真实故障信号，
 *     藏起来等于把「这模型现在不可用」这条最重要的信息删掉。
 */
export function shouldShowTaskHealth<T extends { request_count: number }>(
  health: T | undefined | null
): health is T {
  if (!health) return false
  return Number.isFinite(health.request_count) && health.request_count > 0
}

/** 拆分后的耗时，用于按语言拼装单位。 */
export type TaskDurationParts = {
  hours: number
  minutes: number
  seconds: number
}

/**
 * 把毫秒拆成 时/分/秒 三段。
 *
 * 单位名交给调用方用 t() 渲染 —— 中文要「16 分 44 秒」，英文是
 * 「16 minutes 44 seconds」，硬编码英文单位会让中文界面里夹一串英文。
 *
 * 返回 null 表示没有有效耗时（0 / 负数 / 非有限值），调用方应显示占位符。
 */
export function splitTaskDuration(ms: number): TaskDurationParts | null {
  if (!Number.isFinite(ms) || ms <= 0) return null
  const totalSeconds = Math.round(ms / 1000)
  if (totalSeconds < 60) {
    return { hours: 0, minutes: 0, seconds: totalSeconds }
  }
  const totalMinutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (totalMinutes < 60) {
    return { hours: 0, minutes: totalMinutes, seconds }
  }
  return {
    hours: Math.floor(totalMinutes / 60),
    minutes: totalMinutes % 60,
    seconds,
  }
}
