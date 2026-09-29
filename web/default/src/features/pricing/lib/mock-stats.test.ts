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
import { buildSupportedParameters } from './mock-stats'
import { getModelDisplayName } from './model-helpers'

function autoDLModel(model_name: string): PricingModel {
  return {
    id: 1,
    model_name,
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: ['default'],
  }
}

function parameterNames(modelName: string): string[] {
  return buildSupportedParameters(autoDLModel(modelName)).map(
    (parameter) => parameter.name
  )
}

function parametersByName(modelName: string) {
  return Object.fromEntries(
    buildSupportedParameters(autoDLModel(modelName)).map((parameter) => [
      parameter.name,
      parameter,
    ])
  )
}

describe('AutoDL Model Square API parameters', () => {
  test('shows the renamed workflow identifier without its channel namespace', () => {
    assert.equal(
      getModelDisplayName(autoDLModel('autodl:minimax-h3-lightx2v-v5')),
      'minimax-h3-lightx2v-v5'
    )
  })

  test('uses the renamed identifiers for the original four workflows', () => {
    assert.deepEqual(parameterNames('autodl:minimax-h3-text-to-video'), [
      'prompt',
      'duration',
      'resolution',
    ])
    assert.deepEqual(parameterNames('autodl:minimax-h3-lightx2v-v5'), [
      'prompt',
      'duration',
      'resolution',
      'images',
      'seed',
    ])
    assert.deepEqual(parameterNames('autodl:minimax-h3-lightx2v-v5-15s'), [
      'prompt',
      'duration',
      'resolution',
      'images',
      'seed',
    ])
    assert.deepEqual(parameterNames('autodl:minimax-h3-b99-12s'), [
      'prompt',
      'duration',
      'resolution',
      'images',
      'seed',
    ])
  })

  test('routes every AutoDL workflow to its model-specific parameter table', () => {
    assert.deepEqual(parameterNames('autodl:minimax-h3-u24'), [
      'prompt',
      'duration',
      'resolution',
      'images',
      'audios',
      'seed',
    ])
    assert.deepEqual(parameterNames('autodl:minimax-h3-u08'), [
      'prompt',
      'duration',
      'resolution',
      'images',
      'audios',
      'seed',
    ])
    assert.deepEqual(parameterNames('autodl:minimax-h3-image-audio-10s'), [
      'prompt',
      'duration',
      'resolution',
      'images',
      'audios',
      'seed',
    ])
    assert.deepEqual(parameterNames('autodl:minimax-h3-image-audio-15s'), [
      'prompt',
      'duration',
      'resolution',
      'images',
      'audios',
      'seed',
    ])
    assert.deepEqual(parameterNames('autodl:minimax-h3-lipsync'), [
      'audio_duration',
      'resolution',
      'images',
      'audios',
    ])
  })

  test('exposes the documented differences between the workflows', () => {
    const u24 = parametersByName('autodl:minimax-h3-u24')
    assert.equal(u24.duration.range, '1 ~ 15')
    assert.equal(u24.images.range, '1 ~ 9')
    assert.equal(u24.images.required, true)
    assert.equal(u24.audios.range, '0 ~ 3')
    assert.equal(u24.seed.range, '0 ~ 999999999999999')

    const tenSecond = parametersByName('autodl:minimax-h3-image-audio-10s')
    assert.equal(tenSecond.duration.range, '1 ~ 10')
    assert.equal(tenSecond.images.required, undefined)
    assert.equal(tenSecond.images.range, '0 ~ 9')
    assert.equal(tenSecond.resolution.enumValues?.includes('1080p横'), true)
    assert.equal(tenSecond.resolution.enumValues?.includes('1080p(1:1)'), false)

    const fifteenSecond = parametersByName('autodl:minimax-h3-image-audio-15s')
    assert.equal(fifteenSecond.duration.range, '1 ~ 15')
    assert.equal(
      fifteenSecond.resolution.enumValues?.includes('1080p横'),
      false
    )

    const lipSync = parametersByName('autodl:minimax-h3-lipsync')
    assert.equal(lipSync.prompt, undefined)
    assert.equal(lipSync.audio_duration.range, '1 ~ 15')
    assert.equal(lipSync.images.range, '1 ~ 1')
    assert.equal(lipSync.images.required, true)
    assert.equal(lipSync.audios.range, '1 ~ 1')
    assert.equal(lipSync.audios.required, true)
    assert.equal(lipSync.resolution.enumValues?.includes('1080p横'), true)
  })
})

describe('Wan3 Model Square API parameters', () => {
  test('lists the resolution tiers for smart and the official channel', () => {
    for (const modelName of ['wan3.0-smart', 'wan3.0-video-官网']) {
      const params = parametersByName(modelName)
      assert.deepEqual(parameterNames(modelName), [
        'prompt',
        'duration',
        'resolution',
      ])
      assert.deepEqual(params.resolution.enumValues, ['480P', '720P', '1080P'])
      assert.equal(params.resolution.defaultValue, '480P')
      assert.equal(params.duration.range, '2 ~ 30')
    }
  })

  test('pins wan3.0-video-prime-1080p to 1080P and 30 seconds', () => {
    const params = parametersByName('wan3.0-video-prime-1080p')
    assert.deepEqual(params.resolution.enumValues, ['1080P'])
    assert.equal(params.resolution.defaultValue, '1080P')
    assert.equal(params.duration.range, '30')
  })

  test('describes wan2.7-r2v limits including the required reference images', () => {
    const params = parametersByName('wan2.7-r2v')
    assert.deepEqual(parameterNames('wan2.7-r2v'), [
      'prompt',
      'duration',
      'resolution',
      'ratio',
      'images',
    ])
    assert.equal(params.duration.range, '5 / 10')
    assert.deepEqual(params.resolution.enumValues, ['720p', '1080p'])
    assert.deepEqual(params.ratio.enumValues, ['16:9', '9:16', '1:1'])
    assert.equal(params.images.range, '1 ~ 3')
    assert.equal(params.images.required, true)
  })

  test('keeps generic video parameters for non-wan3 video models', () => {
    assert.deepEqual(parameterNames('sora-2'), [
      'prompt',
      'duration',
      'aspect_ratio',
      'fps',
    ])
  })
})
