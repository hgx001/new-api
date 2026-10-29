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

import en from '../../../i18n/locales/en.json'
import fr from '../../../i18n/locales/fr.json'
import ja from '../../../i18n/locales/ja.json'
import ru from '../../../i18n/locales/ru.json'
import vi from '../../../i18n/locales/vi.json'
import zh from '../../../i18n/locales/zh.json'
import type { PricingModel } from '../types'
import {
  H3_RESOLUTION_TIER_NOTE,
  getModelDisplayName,
  getModelDescriptionKey,
  getResolutionTierNoteKey,
  getResolutionTieredModels,
  getWan3SampleResolution,
  isPerSecondModel,
} from './model-helpers'

const locales: Record<string, { translation: Record<string, string> }> = {
  en,
  zh,
  fr,
  ja,
  ru,
  vi,
}

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

  test('returns the tier note key only for the four H3 workflows', () => {
    for (const modelName of getResolutionTieredModels()) {
      assert.equal(
        getResolutionTierNoteKey(autoDLModel(modelName)),
        H3_RESOLUTION_TIER_NOTE
      )
    }
  })

  test('returns null for non-H3 autodl workflows and non-autodl models', () => {
    assert.equal(
      getResolutionTierNoteKey(autoDLModel('autodl:minimax-h3-u24')),
      null
    )
    assert.equal(
      getResolutionTierNoteKey(
        autoDLModel('autodl:minimax-h3-image-audio-10s')
      ),
      null
    )
    assert.equal(getResolutionTierNoteKey(autoDLModel('wan3.0-video')), null)
    assert.equal(
      getResolutionTierNoteKey(autoDLModel('seedance2.0特惠版')),
      null
    )
  })

  test('keeps the existing display-name override behavior', () => {
    assert.equal(
      getModelDisplayName(autoDLModel('autodl:minimax-h3-lightx2v-v5')),
      'minimax-h3-lightx2v-v5'
    )
  })

  test('shows wan2.7-r2v as a per-second reference-to-video model', () => {
    const model = autoDLModel('wan2.7-r2v')
    assert.equal(isPerSecondModel(model), true)
    assert.equal(
      getModelDescriptionKey(model),
      'Reference-to-video generation with image, video, and audio references. Supports 720P/1080P and 2-15s output; reference-video requests support 2-10s.'
    )
  })

  // 2026-10-28 起有赞两个在售模型都按秒计费（wan3.0-video-prime 由「按次」改为
  // 「按分辨率分档按秒」，EstimateBilling 已返回 seconds）。
  test('marks wan3.0-video and wan3.0-video-prime both per second', () => {
    assert.equal(isPerSecondModel(autoDLModel('wan3.0-smart')), true)
    assert.equal(isPerSecondModel(autoDLModel('wan3.0-video')), true)
    assert.equal(isPerSecondModel(autoDLModel('wan3.0-video-prime')), true)
    assert.equal(
      isPerSecondModel(autoDLModel('wan3.0-video-prime-1080p')),
      true
    )
    // 官方渠道标准版仍是按秒。
    assert.equal(isPerSecondModel(autoDLModel('wan3.0-video-官网')), true)
  })

  // 豆包官网两个 Seedance 计价方式不同（2026-10-12）：
  // seedance-2-0-fast 按秒（EstimateBilling 返回 seconds），卡片必须显示 /秒；
  // seedance-2-5 按次（返回 nil），显示 /次。名字只差分隔符，不能用前缀判断。
  test('marks doubao seedance-2-0-fast per second and seedance-2-5 per request', () => {
    assert.equal(isPerSecondModel(autoDLModel('seedance-2-0-fast')), true)
    assert.equal(isPerSecondModel(autoDLModel('seedance-2-5')), false)
    // dola 两个模型一直是按次，不能被连字符新名带偏。
    assert.equal(isPerSecondModel(autoDLModel('seedance-2.0')), false)
    assert.equal(isPerSecondModel(autoDLModel('seedance-2.5')), false)
  })

  // Erchun wan3.0-480p 的 EstimateBilling 返回 {"seconds": D}（见
  // relay/channel/task/erchun/adaptor.go），即按秒计费。但它的模型名既不以
  // wan3.0-video 也不以 wan3.0-smart 开头 historically 从未被本函数命中，
  // 卡片一直把 ¥0.28/秒 显示成「/ 次」。这是「硬编码前缀表漏登记」的实例，
  // 同 seedance-2-0-fast 一起修。
  test('marks erchun wan3.0-480p as per second', () => {
    assert.equal(isPerSecondModel(autoDLModel('wan3.0-480p')), true)
  })

  // 穷举守卫：广场当前在售的每一个视频/图片模型，其计价单位必须逐条写明。
  // 新增按秒 adaptor 时，这个表会强迫作者显式登记，避免「后端按秒、前端按次」
  // 这种静默错价再次发生（已经出过两次：seedance-2-0-fast、wan3.0-480p）。
  test('classifies every model currently sold by billing unit', () => {
    const perSecond = [
      'seedance-2-0-fast', // manwu 豆包 2.0 Fast
      'wan3.0-480p', // Erchun（仅 480P）
      'wan3.0-smart', // 有赞智能调度（历史别名，等价 wan3.0-video）
      'wan3.0-video', // 有赞标准版
      'wan3.0-video-prime', // 有赞 Prime（720P/1080P 分档按秒）
      'seedance2.5-特惠', // 有赞 Prime 的对外别名，与 prime 同价按秒（2026-10-29 起）
      'wan3.0-video-官网', // 官网标准版
      'wan3.0-720p-官网', // 官网 720P 成片档（2026-10-29 起）
      'autodl:minimax-h3-u24', // AutoDL H3
    ]
    const perRequest = [
      'gpt-6-luna', // 文本
      'gpt-6-sol', // 文本
      'gpt-image-2.5-官方', // 图片按张
      'Nano Banana Pro', // 图片按张（n 倍率）
      'gemini-web-video', // 漫屋 Veo 固定 10s
      'jimeng-video-reverse', // 文本出参
      'seedance-2-5', // 豆包 2.5 按次
      'seedance-2.0', // dola 2.0 按次
      'seedance-2.5', // dola 2.5 按次（已下架）
      'wan-3.0', // MegaAI 渠道：按次 + 分辨率档（无 seconds 倍率）
      // 漫屋 → Picsart 特惠档：固定 30 秒、按次 480P ¥1.50 / 720P ¥2.00
      // （720P 走 size 倍率，不是 seconds 倍率）。
      // 名字是连字符 `wan-3.0-特惠`，与上面点号前缀的 wan3.0-video* 系列不同族，
      // 不能被 `startsWith('wan3.0-video')` 吃掉。
      'wan-3.0-特惠',
    ]

    for (const name of perSecond) {
      assert.equal(
        isPerSecondModel(autoDLModel(name)),
        true,
        `${name} 应显示 / 秒`
      )
    }
    for (const name of perRequest) {
      assert.equal(
        isPerSecondModel(autoDLModel(name)),
        false,
        `${name} 应显示 / 次`
      )
    }
  })

  // 有赞/官网 wan3 系模型都接受 resolution 参数，示例值按模型给：
  // video/官网 默认 480P，r2v 只能 720p/1080p（上游要小写），
  // prime 只有 720P/1080P，默认取最低档 720P。
  test('returns the wan3 sample resolution per model', () => {
    assert.equal(getWan3SampleResolution('wan3.0-smart'), '480P')
    assert.equal(getWan3SampleResolution('wan3.0-video'), '480P')
    assert.equal(getWan3SampleResolution('wan3.0-video-官网'), '480P')
    assert.equal(getWan3SampleResolution('wan2.7-r2v'), '720p')
    assert.equal(getWan3SampleResolution('wan3.0-video-prime'), '720P')
    assert.equal(getWan3SampleResolution('wan3.0-video-prime-1080p'), '720P')
    // prime 的对外别名，档位与默认值必须与 prime 一致。
    assert.equal(getWan3SampleResolution('seedance2.5-特惠'), '720P')
    // 官网 720P 成片档：输出固定 720P，示例按 720P 写。
    assert.equal(getWan3SampleResolution('wan3.0-720p-官网'), '720P')
    // 非 wan3 系模型没有该参数，示例里不应出现 resolution。
    assert.equal(getWan3SampleResolution('autodl:minimax-h3-u24'), null)
    assert.equal(getWan3SampleResolution('gpt-6-luna'), null)
  })

  // Regression guard: i18n keys MUST live under the `translation` namespace
  // object. Keys added at the JSON root are bundled but invisible to t(),
  // which then renders the raw key (this exact bug shipped once).
  for (const locale of Object.keys(locales)) {
    test(`${locale}: tier note key resolves under the translation namespace`, () => {
      const value = locales[locale].translation[H3_RESOLUTION_TIER_NOTE]
      assert.ok(
        value && value !== H3_RESOLUTION_TIER_NOTE,
        `${locale} should translate ${H3_RESOLUTION_TIER_NOTE}`
      )
    })
  }
})
