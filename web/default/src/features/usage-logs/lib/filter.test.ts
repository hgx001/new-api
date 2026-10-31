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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { buildSearchParams } from './filter'
import type { CommonLogFilters } from '../types'

const start = new Date('2026-11-01T00:00:00+08:00')
const end = new Date('2026-11-01T08:32:00+08:00')

function build(filters: Partial<CommonLogFilters>) {
  return buildSearchParams(
    { startTime: start, endTime: end, ...filters } as CommonLogFilters,
    'common'
  )
}

describe('usage logs filter params', () => {
  // 回归守卫：粘贴进来的首尾空格不能带进 URL。
  //
  // 线上问题（2026-11-01）：模型名输入框里粘贴了「 seedance2.5-特惠」，
  // URL 变成 `model=+seedance2.5-特惠`，后端等值匹配命中 0 条，
  // 页面显示「未查询到日志」—— 而输入框里那个前导空格根本看不出来。
  test('trims pasted whitespace from model name', () => {
    assert.equal(build({ model: ' seedance2.5-特惠' }).model, 'seedance2.5-特惠')
    assert.equal(build({ model: 'seedance2.5-特惠 ' }).model, 'seedance2.5-特惠')
    assert.equal(
      build({ model: '  seedance2.5-特惠  ' }).model,
      'seedance2.5-特惠'
    )
  })

  // 全是空格等于没筛：不能让 URL 里出现 `model=+`，否则后端按空串以外的值匹配
  test('drops whitespace-only filters instead of sending them', () => {
    const params = build({ model: '   ', token: '\t', group: ' ' })
    assert.equal('model' in params, false)
    assert.equal('token' in params, false)
    assert.equal('group' in params, false)
  })

  test('trims every text filter, not just the model name', () => {
    const params = build({
      token: ' key-1 ',
      group: ' default ',
      username: ' alice ',
      requestId: ' req-1 ',
      upstreamRequestId: ' up-1 ',
    })
    assert.equal(params.token, 'key-1')
    assert.equal(params.group, 'default')
    assert.equal(params.username, 'alice')
    assert.equal(params.requestId, 'req-1')
    assert.equal(params.upstreamRequestId, 'up-1')
  })

  test('keeps normal values and the time range untouched', () => {
    const params = build({ model: 'wan3.0-video' })
    assert.equal(params.model, 'wan3.0-video')
    assert.equal(params.startTime, start.getTime())
    assert.equal(params.endTime, end.getTime())
  })

  // 部分名要能搜（后端按包含匹配），前端不能自作主张改写成通配符
  test('passes partial names through so the backend can match by contains', () => {
    assert.equal(build({ model: 'seedance' }).model, 'seedance')
  })
})
