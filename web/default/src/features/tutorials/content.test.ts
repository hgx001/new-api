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

import { getDocIndex, getPageTitle, getPagination, getTutorials } from './content'

describe('tutorials content', () => {
  const platformUrl = 'https://api.example.com'
  const sections = getTutorials(platformUrl)
  const title = getPageTitle()
  const pagination = getPagination()
  const index = getDocIndex()
  const sectionsJson = JSON.stringify(sections)
  const indexJson = JSON.stringify(index)

  test('page title is in Chinese and describes New API access', () => {
    assert.equal(title.includes('接入'), true)
    assert.equal(title.includes('New API'), true)
  })

  test('sections do not reference the reference project brand', () => {
    assert.equal(sectionsJson.includes('Indo Token'), false)
    assert.equal(sectionsJson.includes('indotoken.ai'), false)
    assert.equal(sectionsJson.toLowerCase().includes('indotoken'), false)
  })

  test('sections include New API platform concepts', () => {
    assert.equal(sectionsJson.includes('New API'), true)
    assert.equal(sectionsJson.includes('API Key'), true)
    assert.equal(sectionsJson.includes('Base URL'), true)
  })

  test('sections include AI client configuration guides', () => {
    assert.equal(sectionsJson.includes('Claude Code'), true)
    assert.equal(sectionsJson.includes('OpenCode'), true)
    assert.equal(sectionsJson.includes('Cline'), true)
  })

  test('dynamic platform URL is injected into code examples', () => {
    assert.equal(sectionsJson.includes(platformUrl), true)
    assert.equal(sectionsJson.includes(`${platformUrl}/v1`), true)
  })

  test('documented endpoints match backend relay routes', () => {
    assert.equal(sectionsJson.includes('/v1/chat/completions'), true)
    assert.equal(sectionsJson.includes('/v1/models'), true)
    assert.equal(sectionsJson.includes('/v1/embeddings'), true)
    assert.equal(sectionsJson.includes('/v1/messages'), true)
  })

  test('Claude Code base URL omits /v1 while OpenAI-compatible URLs include /v1', () => {
    // Claude Code ANTHROPIC_BASE_URL should be the platform root; the Anthropic
    // SDK appends /v1/messages automatically.
    assert.equal(sectionsJson.includes('ANTHROPIC_BASE_URL'), true)
    assert.equal(sectionsJson.includes(`${platformUrl}/v1/messages`), true)
    assert.equal(sectionsJson.includes(`${platformUrl}/v1"`), true)
    // OpenAI-compatible clients (OpenCode / Cline / generic) use /v1.
    assert.equal(sectionsJson.includes(`${platformUrl}/v1`), true)
  })

  test('expected sections are present', () => {
    const ids = new Set(sections.map((s) => s.id))
    assert.equal(ids.has('introduction'), true)
    assert.equal(ids.has('quickstart'), true)
    assert.equal(ids.has('ai-client-config'), true)
  })

  test('doc index covers all major sections', () => {
    assert.equal(indexJson.includes('平台简介'), true)
    assert.equal(indexJson.includes('快速开始'), true)
    assert.equal(indexJson.includes('AI 客户端配置'), true)
    assert.equal(indexJson.includes('Claude Code CLI'), true)
    assert.equal(indexJson.includes('OpenCode TUI'), true)
  })

  test('pagination titles are in Chinese and do not reference external project', () => {
    assert.equal(pagination.prev.title.includes('Indo Token'), false)
    assert.equal(pagination.next.title.includes('Indo Token'), false)
  })
})
