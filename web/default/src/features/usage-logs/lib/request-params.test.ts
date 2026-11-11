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

import { buildRequestParamRows } from './request-params'

describe('usage logs request params', () => {
  // 没有捕获到参数（旧日志 / 非视频请求）时不能凭空渲染空区块。
  test('returns empty rows when params are missing', () => {
    assert.deepEqual(buildRequestParamRows(undefined), [])
    assert.deepEqual(buildRequestParamRows({}), [])
  })

  // 顺序固定，方便运维按习惯扫读：模型 → 提示词 → 时长 → 画幅 → 素材。
  test('renders key params in stable order', () => {
    const rows = buildRequestParamRows({
      model: 'seedance2.5-活动',
      prompt: '一只猫',
      seconds: '10',
      duration: 8,
      resolution: '720P',
      size: '1280x720',
      ratio: '16:9',
      seed: 42,
    })

    assert.deepEqual(
      rows.map((row) => row.labelKey),
      [
        'Model',
        'Prompt',
        'Seconds',
        'Duration',
        'Resolution',
        'Size',
        'Ratio',
        'Seed',
      ]
    )
    assert.equal(rows[0].value, 'seedance2.5-活动')
    assert.equal(rows[7].value, '42')
  })

  // seed=0 是合法值，不能被当成「没传」丢掉。
  test('keeps zero seed', () => {
    const rows = buildRequestParamRows({ seed: 0 })
    assert.deepEqual(rows, [{ labelKey: 'Seed', value: '0' }])
  })

  // 参考素材是排查 422 的关键：类型 + 完整 URL 都要显示。
  test('renders media entries with type and url', () => {
    const rows = buildRequestParamRows({
      media: [
        { type: 'reference_video', url: 'https://a.example/v.mp4' },
        { type: 'reference_image', url: 'https://a.example/i.png' },
      ],
    })

    const media = rows.find((row) => row.labelKey === 'Media')
    assert.ok(media, '必须有 Media 行')
    assert.equal(
      media.value,
      'reference_video: https://a.example/v.mp4\nreference_image: https://a.example/i.png'
    )
  })

  // 只有计数（上游没回媒体明细）时也要显示「带了几条参考视频」。
  test('falls back to media counts', () => {
    const rows = buildRequestParamRows({
      media_counts: { reference_video: 2, reference_image: 1 },
    })

    const media = rows.find((row) => row.labelKey === 'Media')
    assert.ok(media)
    assert.equal(media.value, 'reference_video ×2, reference_image ×1')
  })

  // 素材字段来源：media / images 的取舍不同，排查时必须知道客户写在哪。
  test('renders media field sources', () => {
    const rows = buildRequestParamRows({
      media_fields: { media: 1, images: 2 },
    })

    const fields = rows.find((row) => row.labelKey === 'Media Fields')
    assert.ok(fields)
    assert.equal(fields.value, 'media: 1, images: 2')
  })

  test('skips empty string values', () => {
    const rows = buildRequestParamRows({ model: 'm', prompt: '   ' })
    assert.deepEqual(rows, [{ labelKey: 'Model', value: 'm' }])
  })
})
