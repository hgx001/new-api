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

import { getDocIndex, getTutorials } from './content'

const platformUrl = 'https://api.example.com'
const sections = getTutorials(platformUrl)
const sectionsJson = JSON.stringify(sections)

/** 收集 sections 里全部锚点 id（含 level 3 标题）。 */
function collectAnchorIds(): Set<string> {
  const ids = new Set<string>()
  for (const section of sections) {
    ids.add(section.id)
    for (const node of section.content ?? []) {
      if (node.type === 'heading') ids.add(node.id)
    }
  }
  return ids
}

describe('tutorials model guide content', () => {
  test('exposes the downstream model guide sections', () => {
    const ids = new Set(sections.map((section) => section.id))
    for (const expected of [
      'models-overview',
      'text-api',
      'image-api',
      'video-api',
      'video-model-params',
      'video-media-rules',
      'billing-refund',
      'api-errors',
      'integration-faq',
    ]) {
      assert.equal(ids.has(expected), true, `missing section ${expected}`)
    }
  })

  test('documents every model currently sold with its price', () => {
    for (const expected of [
      'MiniMax-M3',
      'gpt-6-luna',
      'gpt-6-sol',
      'gpt-image-2.5-官方',
      'wan3.0-smart',
      'wan3.0-video-官网',
      'wan3.0-video-prime-1080p',
      'wan2.7-r2v',
      'autodl:minimax-h3-u24',
      'seedance-2.0',
      'seedance-2.5',
      '¥0.15 / 张',
      '¥8.00 / 次',
      '¥0.28/s',
      '¥0.85/s',
      '¥0.10/s',
      '¥1.50 / 次',
      '¥1.00 / 次',
    ]) {
      assert.equal(sectionsJson.includes(expected), true, `missing ${expected}`)
    }
  })

  test('documents the three relay endpoints used by downstream clients', () => {
    for (const endpoint of [
      '/v1/chat/completions',
      '/v1/images/generations',
      '/v1/videos',
    ]) {
      assert.equal(
        sectionsJson.includes(`${platformUrl}${endpoint}`),
        true,
        `missing ${endpoint}`
      )
    }
  })

  test('injects the platform URL into video submit and content examples', () => {
    assert.equal(sectionsJson.includes(`${platformUrl}/v1/videos/task_xxx`), true)
    assert.equal(
      sectionsJson.includes(`${platformUrl}/v1/videos/task_xxx/content`),
      true
    )
  })

  test('keeps per-model hard limits documented', () => {
    for (const expected of [
      '仅 5 或 10',
      '固定 1080P',
      '固定 30',
      '必填，1–3 张参考图',
      '必填，1–9 张',
      '首尾帧不能与普通参考图/视频/音频混用',
    ]) {
      assert.equal(sectionsJson.includes(expected), true, `missing ${expected}`)
    }
  })

  // 回归守卫：seconds 在网关里是字符串字段，JSON 示例里写数字会直接报 invalid_json。
  test('json examples use duration instead of numeric seconds', () => {
    const codeBlocks: string[] = []
    for (const section of sections) {
      for (const node of section.content ?? []) {
        if (node.type === 'codeBlock') codeBlocks.push(node.value)
      }
    }
    const code = codeBlocks.join('\n')
    assert.equal(code.includes('"seconds": 5'), false)
    assert.equal(code.includes('"duration": 5'), true)
  })

  // 配额不足在网关里是 403 insufficient_user_quota，不是 402。
  // seedance-2.0 / seedance-2.5 与 gemini-web-video 均按次计费，文档不能再出现按秒写法。
  test('documents remote video models as per-request billing', () => {
    assert.equal(sectionsJson.includes('¥2.50/s'), false)
    assert.equal(sectionsJson.includes('¥1.50 / 次'), true)
    assert.equal(sectionsJson.includes('¥1.00 / 次'), true)
    assert.equal(sectionsJson.includes('固定 30 秒、720P'), true)
  })

  test('error table documents 403 for insufficient quota', () => {
    assert.equal(sectionsJson.includes('insufficient_user_quota'), true)
    assert.equal(sectionsJson.includes('402：'), false)
  })

  test('every doc index anchor resolves to a real section or heading id', () => {
    const anchorIds = collectAnchorIds()
    for (const entry of getDocIndex()) {
      const id = entry.href.replace(/^#/, '')
      assert.equal(anchorIds.has(id), true, `dangling anchor ${entry.href}`)
    }
  })
})
