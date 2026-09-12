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

import type { PricingModel } from '../types'
import { getModelDisplayName } from './model-helpers'
import {
  getResolutionTierNoteKey,
  getResolutionTieredModels,
} from './model-helpers'

function autoDLModel(model_name: string): PricingModel {
  return {
    id: 1,
    model_name,
    quota_type: 1,
    model_ratio: 0,
    model_price: 0.01369863,
    completion_ratio: 0,
    enable_groups: ['default'],
    supported_endpoint_types: ['openai-video'],
  }
}

describe('AutoDL H3 resolution tier note', () => {
  test('lists the four H3 workflows that share the resolution tier note', () => {
    assert.deepEqual(getResolutionTieredModels(), [
      'autodl:minimax-h3-text-to-video',
      'autodl:minimax-h3-lightx2v-v5',
      'autodl:minimax-h3-lightx2v-v5-15s',
      'autodl:minimax-h3-b99-12s',
    ])
  })

  test('returns the per-model note key only for the four H3 workflows', () => {
    assert.equal(
      getResolutionTierNoteKey(autoDLModel('autodl:minimax-h3-text-to-video')),
      '480p ¥0.1/s · 768p档 ¥0.12/s · 1080p ¥0.2/s'
    )
    assert.equal(
      getResolutionTierNoteKey(autoDLModel('autodl:minimax-h3-lightx2v-v5')),
      '480p ¥0.1/s · 768p档 ¥0.12/s · 1080p ¥0.2/s'
    )
    assert.equal(
      getResolutionTierNoteKey(autoDLModel('autodl:minimax-h3-lightx2v-v5-15s')),
      '480p ¥0.1/s · 768p档 ¥0.12/s · 1080p ¥0.2/s'
    )
    assert.equal(
      getResolutionTierNoteKey(autoDLModel('autodl:minimax-h3-b99-12s')),
      '480p ¥0.1/s · 768p档 ¥0.12/s · 1080p ¥0.2/s'
    )
  })

  test('returns null for non-H3 autodl workflows and non-autodl models', () => {
    assert.equal(
      getResolutionTierNoteKey(autoDLModel('autodl:minimax-h3-u24')),
      null
    )
    assert.equal(
      getResolutionTierNoteKey(autoDLModel('autodl:minimax-h3-image-audio-10s')),
      null
    )
    assert.equal(getResolutionTierNoteKey(autoDLModel('wan3.0-video')), null)
    assert.equal(getResolutionTierNoteKey(autoDLModel('seedance2.0特惠版')), null)
  })

  test('keeps the existing display-name override behavior', () => {
    assert.equal(
      getModelDisplayName(autoDLModel('autodl:minimax-h3-lightx2v-v5')),
      'minimax-h3-lightx2v-v5'
    )
  })
})