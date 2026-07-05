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

  test('page title is in Chinese and describes New API access', () => {
    assert.equal(title.includes('Claude'), false)
    assert.equal(title.includes('claudecode'), false)
    assert.equal(title.includes('接入'), true)
  })

  test('sections do not reference Claude Code or claudecode', () => {
    const json = JSON.stringify(sections)
    assert.equal(json.includes('Claude Code'), false)
    assert.equal(json.includes('claudecode'), false)
    assert.equal(json.includes('Anthropic'), false)
  })

  test('sections reference New API platform concepts', () => {
    const json = JSON.stringify(sections)
    assert.equal(json.includes('New API') || json.includes('平台') || json.includes('API'), true)
  })

  test('pagination titles are in Chinese and relevant', () => {
    assert.equal(pagination.prev.title.includes('Claude'), false)
    assert.equal(pagination.next.title.includes('Claude'), false)
  })

  test('doc index does not reference Claude Code', () => {
    const json = JSON.stringify(index)
    assert.equal(json.includes('Claude'), false)
    assert.equal(json.includes('claudecode'), false)
  })
})
