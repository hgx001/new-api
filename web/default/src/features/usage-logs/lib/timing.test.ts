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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { resolveTimingDisplay } from './timing'

describe('usage logs timing display', () => {
  // 核心行为：任务类请求要显示**真实完成时长**，不是提交往返耗时。
  //
  // 为什么这条重要：消费日志在提交那一刻就写了，那时上游只回了「已受理」，
  // use_time 记的是提交往返（生产实测普遍 2.0s）。用户看到 2.0s 会以为
  // 视频 2 秒就生成了，实际上游要跑 1~3 分钟。task_duration 是任务终态时
  // 回填的真实等待时间。
  test('task log prefers real task duration over submit roundtrip', () => {
    const display = resolveTimingDisplay({
      type: 2,
      useTime: 2,
      taskDuration: 143,
    })
    assert.equal(display.seconds, 143)
    assert.equal(display.kind, 'task')
  })

  // 任务还在跑（没回填时长）时，回落到提交耗时 —— 总比什么都不显示强。
  test('pending task falls back to submit time', () => {
    const display = resolveTimingDisplay({
      type: 2,
      useTime: 2,
      taskDuration: 0,
    })
    assert.equal(display.seconds, 2)
    assert.equal(display.kind, 'submit')
  })

  // 文本类请求没有 task_duration，永远用 use_time。
  test('text request uses use time', () => {
    const display = resolveTimingDisplay({
      type: 2,
      useTime: 4,
      taskDuration: 0,
    })
    assert.equal(display.seconds, 4)
    assert.equal(display.kind, 'submit')
  })

  // 非消费/非错误日志不显示耗时。
  test('non timing log type is not displayable', () => {
    const display = resolveTimingDisplay({
      type: 1, // 充值
      useTime: 0,
      taskDuration: 0,
    })
    assert.equal(display.displayable, false)
  })

  // 异常值不能显示成 NaN / undefined。
  test('handles missing and negative values', () => {
    const missing = resolveTimingDisplay({
      type: 2,
      useTime: undefined,
      taskDuration: undefined,
    })
    assert.equal(missing.displayable, false)

    const negative = resolveTimingDisplay({
      type: 2,
      useTime: -5,
      taskDuration: -1,
    })
    assert.equal(negative.displayable, false)
  })
})
