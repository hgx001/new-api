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
      // 2026-11-15 上游切换：480P 下架、最低 10 秒。
      assert.deepEqual(params.resolution.enumValues, ['720P', '1080P'])
      assert.equal(params.resolution.defaultValue, '720P')
      assert.equal(params.duration.range, '10 ~ 30')
    }
  })

  test('gives wan3.0-video-prime the fixed 720P tier and 30 seconds', () => {
    // 2026-11-15 上游切换：prime 收窄为仅 30 秒、仅 720P。
    for (const modelName of [
      'wan3.0-video-prime',
      'wan3.0-video-prime-1080p',
    ]) {
      const params = parametersByName(modelName)
      assert.deepEqual(params.resolution.enumValues, ['720P'])
      assert.equal(params.resolution.defaultValue, '720P')
      assert.equal(params.duration.range, '30')
    }
  })

  // 回归守卫：wan3.0-720p-特惠 的名字不以 `wan3.0-video` 开头，
  // 漏登记会被归类成 chat 模型（参数表整张错），或者落进通用 wan3 表
  // （把 480P/1080P 也列成可选，客户传 480P 会以为拿到 480P）。
  //
  // 2026-11-17 起该模型只支持 9:16（比例收窄）—— 参数表必须显式列出
  // aspect_ratio 的唯一枚举值，否则前端等于告诉客户「比例随便传」，
  // 而后端会对非 9:16 直接 400 拒单 → 客户拿到的是「文档说能用、实际报错」。
  test('gives wan3.0-720p-特惠 a fixed 720P tier, 10-30 seconds and 9:16 only', () => {
    const params = parametersByName('wan3.0-720p-特惠')
    assert.deepEqual(parameterNames('wan3.0-720p-特惠'), [
      'prompt',
      'duration',
      'resolution',
      'aspect_ratio',
    ])
    assert.deepEqual(params.resolution.enumValues, ['720P'])
    assert.equal(params.resolution.defaultValue, '720P')
    assert.equal(params.duration.range, '10 ~ 30')
    assert.deepEqual(params.aspect_ratio.enumValues, ['9:16'])
    assert.equal(params.aspect_ratio.defaultValue, '9:16')
  })

  // 回归守卫：wan-3.0-pro 是有赞的独立上游模型（不是 wan3.0 标准版别名），
  // 名字命中的是通用 `/wan-/` 规则。漏登记就会落进通用 VIDEO_PARAMS，
  // 显示「duration 1~60、aspect_ratio 含 1:1、fps」且**没有 resolution**
  // —— 三条都与上游文档不符，客户照抄示例传 1:1 会被后端 400。
  test('describes wan-3.0-pro with its own 4-30s / 720P-1080P / 16:9-9:16 contract', () => {
    const params = parametersByName('wan-3.0-pro')
    assert.deepEqual(parameterNames('wan-3.0-pro'), [
      'prompt',
      'duration',
      'resolution',
      'aspect_ratio',
      'images',
      'videos',
      'audios',
    ])
    // 4—30 秒整数秒，不传默认 5 秒；与时长无关的固定售价由后端计费保证。
    assert.equal(params.duration.range, '4 ~ 30')
    assert.equal(params.duration.defaultValue, 5)
    // 仅 720P / 1080P 两档，示例取最低档。
    assert.deepEqual(params.resolution.enumValues, ['720P', '1080P'])
    assert.equal(params.resolution.defaultValue, '720P')
    // 画幅仅 16:9 / 9:16 —— 通用表里的 1:1 必须消失。
    assert.deepEqual(params.aspect_ratio.enumValues, ['16:9', '9:16'])
    assert.equal(params.aspect_ratio.defaultValue, '16:9')
    // 参考素材上限与上游文档一致（合计 ≤20 项由后端校验）。
    assert.equal(params.images.range, '0 ~ 10')
    assert.equal(params.videos.range, '0 ~ 5')
    assert.equal(params.audios.range, '0 ~ 5')
    // 该模型没有 fps 参数，通用表的 fps 必须消失。
    assert.equal(params.fps, undefined)
  })

  // 回归守卫：wan-3.0-yz 是有赞 2026-11-18 新上架的**独立**上游模型
  // （与 wan3.0-video / wan3.0-video-prime 是三个并列模型，不是改名关系）。
  //
  // 它与 wan-3.0-pro 契约相似但**时长下界不同**：wan-3.0-yz 是 10—30 秒
  // （上游硬边界，实测原文 "Duration 4s is outside the allowed range
  // 10-30 seconds for model wan-3.0"），pro 是 4—30 秒。
  //
  // 漏登记就会落进通用 VIDEO_PARAMS：显示「duration 1~60、aspect_ratio 含 1:1、
  // fps」且没有 resolution —— 客户照抄传 1:1 会被后端 400。
  test('describes wan-3.0-yz with its own 10-30s / 720P-1080P / 16:9-9:16 contract', () => {
    const params = parametersByName('wan-3.0-yz')
    assert.deepEqual(parameterNames('wan-3.0-yz'), [
      'prompt',
      'duration',
      'resolution',
      'aspect_ratio',
      'images',
      'videos',
      'audios',
    ])
    // 10—30 秒整数秒（实测硬边界），不传默认 10 秒。
    assert.equal(params.duration.range, '10 ~ 30')
    assert.equal(params.duration.defaultValue, 10)
    // 仅 720P / 1080P 两档，示例取最低档。
    assert.deepEqual(params.resolution.enumValues, ['720P', '1080P'])
    assert.equal(params.resolution.defaultValue, '720P')
    // 画幅仅 16:9 / 9:16 —— 通用表里的 1:1 必须消失。
    assert.deepEqual(params.aspect_ratio.enumValues, ['16:9', '9:16'])
    assert.equal(params.aspect_ratio.defaultValue, '16:9')
    // 参考素材上限与上游文档一致（合计 ≤20 项由后端校验）。
    assert.equal(params.images.range, '0 ~ 10')
    assert.equal(params.videos.range, '0 ~ 5')
    assert.equal(params.audios.range, '0 ~ 5')
    // 该模型没有 fps 参数，通用表的 fps 必须消失。
    assert.equal(params.fps, undefined)
  })

  // 回归守卫：wan-3.0-yz 与 wan-3.0-pro 是两个**不同**商品，
  // 时长下界不同（10 vs 4）。判定必须精确匹配，不能用 startsWith('wan-3.0')，
  // 否则 pro 的参数表会被 wan-3.0-yz 覆盖（或反之），下界就错了。
  test('keeps wan-3.0-yz and wan-3.0-pro on separate duration bounds', () => {
    const wan30 = parametersByName('wan-3.0-yz')
    const pro = parametersByName('wan-3.0-pro')
    assert.equal(wan30.duration.range, '10 ~ 30')
    assert.equal(pro.duration.range, '4 ~ 30')
    assert.notEqual(wan30.duration.range, pro.duration.range)
  })

  // 回归守卫：客美 Kemeitv（渠道 71）的两个万相特价变体
  // `wan3.0-video-tj` / `wan3.0-video-prime-tj`。
  //
  // 它们的名字以 "wan3.0-video" 开头，会被 `startsWith` 命中 —— 但它们走的是
  // **客美自己的 OpenAI 兼容协议**，契约与站内 wan3 系标准版完全不同：
  //   - 时长 2—30 秒（标准版是 10—30 秒）
  //   - 分辨率 **480P / 720P / 1080P 三档齐全**（标准版只有 720P/1080P）
  //   - 画幅 5 档（含 4:3 / 3:4）
  // 落进通用表会**少显示 480P** —— 而 480P 恰好是最低价档
  // （tj ¥0.138/秒 vs 720P ¥0.25/秒），客户按表选就选不到最便宜的档位。
  test('describes wan3.0-video-tj and wan3.0-video-prime-tj with the kemeitv contract', () => {
    for (const modelName of [
      'wan3.0-video-tj',
      'wan3.0-video-prime-tj',
    ]) {
      const params = parametersByName(modelName)
      assert.deepEqual(parameterNames(modelName), [
        'prompt',
        'duration',
        'resolution',
        'aspect_ratio',
        'images',
        'videos',
        'audios',
      ])
      // 2—30 秒整数秒，缺省 5 秒。
      assert.equal(params.duration.range, '2 ~ 30')
      assert.equal(params.duration.defaultValue, 5)
      // 三档齐全，缺省 720P。
      assert.deepEqual(params.resolution.enumValues, [
        '480P',
        '720P',
        '1080P',
      ])
      assert.equal(params.resolution.defaultValue, '720P')
      // 画幅 5 档（上游无 adaptive）。
      assert.deepEqual(params.aspect_ratio.enumValues, [
        '16:9',
        '9:16',
        '4:3',
        '3:4',
        '1:1',
      ])
      assert.equal(params.aspect_ratio.defaultValue, '16:9')
      // 参考素材上限与上游文档一致。
      assert.equal(params.images.range, '0 ~ 10')
      assert.equal(params.videos.range, '0 ~ 5')
      assert.equal(params.audios.range, '0 ~ 5')
      // 该模型没有 fps 参数，通用表的 fps 必须消失。
      assert.equal(params.fps, undefined)
    }
  })

  // 回归守卫：客美两档必须**留在**通用 wan3 判定之外 ——
  // 否则站内标准版 wan3.0-video / wan3.0-video-prime 的时长下界会被改成 2 秒，
  // 而它们 2026-11-15 上游切换后最低 10 秒。
  test('keeps the kemeitv tiers from overriding the wan3 standard contract', () => {
    const standard = parametersByName('wan3.0-video')
    assert.equal(standard.duration.range, '10 ~ 30')
    assert.deepEqual(standard.resolution.enumValues, ['720P', '1080P'])
    const prime = parametersByName('wan3.0-video-prime')
    assert.equal(prime.duration.range, '30')
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
