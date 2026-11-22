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
import { PROMO_HIGHLIGHT_KEYS } from '../constants'
import {
  H3_RESOLUTION_TIER_NOTE,
  getModelDisplayName,
  getModelDescriptionKey,
  getModelHighlightKeys,
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

describe('CapCut 满血展示名', () => {
  // 「满血」只是运营口径的展示名：对外 API 名必须保持 cp-seedance-2.0 / 2.5，
  // 否则渠道、abilities、manwu adaptor 的模型白名单全部对不上。
  test('shows 满血 names for the two flagship CapCut models', () => {
    assert.equal(
      getModelDisplayName(autoDLModel('cp-seedance-2.0')),
      'cp-seedance-2.0满血'
    )
    assert.equal(
      getModelDisplayName(autoDLModel('cp-seedance-2.5')),
      'cp-seedance-2.5满血'
    )
  })

  test('keeps other CapCut tiers unchanged', () => {
    for (const modelName of [
      'cp-seedance-2.0fast',
      'cp-seedance-2.0mini',
      'cp-seedance-2.5flash',
    ]) {
      assert.equal(getModelDisplayName(autoDLModel(modelName)), modelName)
    }
  })
})

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

  // 豆包 Seedance 2.5 Flash（2026-11-22 上架，¥0.25/秒）：既不是 `seedance-2-0`
  // 前缀（那条只覆盖 2.0 两档），也不是按次的 `seedance-2-5` → 必须单列，
  // 否则广场会把每秒价挂在「/次」下（与 seedance-2-0-fast 同类的静默错价）。
  test('marks doubao seedance-2-5-flash per second', () => {
    assert.equal(isPerSecondModel(autoDLModel('seedance-2-5-flash')), true)
    // 两个近名不能被串档：按次的 2.5 本体、CapCut 的 cp- 版本
    assert.equal(isPerSecondModel(autoDLModel('seedance-2-5')), false)
    assert.equal(isPerSecondModel(autoDLModel('cp-seedance-2.5flash')), true)
  })

  // Erchun wan3.0-480p 的 EstimateBilling 返回 {"seconds": D}（见
  // relay/channel/task/erchun/adaptor.go），即按秒计费。但它的模型名既不以
  // wan3.0-video 也不以 wan3.0-smart 开头 historically 从未被本函数命中，
  // 卡片一直把 ¥0.28/秒 显示成「/ 次」。这是「硬编码前缀表漏登记」的实例，
  // 同 seedance-2-0-fast 一起修。
  test('marks erchun wan3.0-480p as per second', () => {
    assert.equal(isPerSecondModel(autoDLModel('wan3.0-480p')), true)
  })

  // SeetaCloud FastH3（2026-11-07 改版）：EstimateBilling 返回
  // {"seconds": N, "size": 分辨率倍率}，结算按秒。两个对外模型名既不以
  // wan3.0-video 开头、也不带 autodl: 前缀，漏登记就会把 ¥0.03/秒 挂在
  // 「/次」下面 —— 与 seedance-2-0-fast、wan3.0-480p 同一类静默错价。
  test('marks both SeetaCloud FastH3 models as per second', () => {
    assert.equal(isPerSecondModel(autoDLModel('minimax_h3_v2')), true)
    assert.equal(isPerSecondModel(autoDLModel('minimax_h3_omni')), true)
    // 退役旧名仍可路由，单位口径必须与新版一致。
    assert.equal(isPerSecondModel(autoDLModel('minimax_h3-文生')), true)
    assert.equal(isPerSecondModel(autoDLModel('minimax_h3-图生')), true)
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
      'wan3.0-720p-特惠', // 官网 720P 成片档（2026-10-29 起，2026-10-30 改名）
      // 有赞 wan-3.0-yz（2026-11-18 上架）：上游新独立模型，按秒计费
      // （720P ¥0.20/秒、1080P ¥0.25/秒）。
      // ⚠️ 名字以连字符开头（`wan-3.0-yz`），前缀表 `startsWith('wan3.0-video')`
      // 吃不到它 —— 不显式登记就会落进按次兜底，广场把 ¥0.23/秒 挂在
      // 「/次」下面，客户按次理解就是错价。
      'wan-3.0-yz',
      'autodl:minimax-h3-u24', // AutoDL H3
      'minimax_h3_v2', // SeetaCloud FastH3 V2（文生/首帧/首尾帧，按秒 ¥0.03 起）
      'minimax_h3_omni', // SeetaCloud FastH3-Omni（9图+3视频+3音频，按秒 ¥0.03 起）
      'minimax_h3-文生', // SeetaCloud H3 退役旧名（仍可路由）
      'minimax_h3-图生', // SeetaCloud H3 退役旧名（仍可路由）
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
  // 有赞标准版 2026-11-15 上游切换后仅 720P/1080P（480P 下架），示例取 720P；
  // 官网（DashScope）不受影响，默认 480P。r2v 只能 720p/1080p（上游要小写），
  // prime 2026-11-15 起仅 720P。
  test('returns the wan3 sample resolution per model', () => {
    assert.equal(getWan3SampleResolution('wan3.0-smart'), '720P')
    assert.equal(getWan3SampleResolution('wan3.0-video'), '720P')
    assert.equal(getWan3SampleResolution('wan3.0-video-官网'), '480P')
    assert.equal(getWan3SampleResolution('wan2.7-r2v'), '720p')
    assert.equal(getWan3SampleResolution('wan3.0-video-prime'), '720P')
    assert.equal(getWan3SampleResolution('wan3.0-video-prime-1080p'), '720P')
    // prime 的对外别名，档位与默认值必须与 prime 一致。
    assert.equal(getWan3SampleResolution('seedance2.5-特惠'), '720P')
    // 官网 720P 成片档：输出固定 720P，示例按 720P 写。
    assert.equal(getWan3SampleResolution('wan3.0-720p-特惠'), '720P')
    // 有赞 wan-3.0-pro（2026-11-18 上架）：文档示例 size=1280x720，取最低档 720P。
    assert.equal(getWan3SampleResolution('wan-3.0-pro'), '720P')
    // 有赞 wan-3.0-yz（2026-11-18 上架）：新独立模型，档位 720P/1080P，取最低档。
    //
    // ⚠️ 判定必须精确匹配：wan-3.0-pro / wan-3.0-特惠 的合法档位与本模型
    // 不同，前缀匹配会把它们的示例分辨率一起带过来。
    assert.equal(getWan3SampleResolution('wan-3.0-yz'), '720P')
    // 客美 Kemeitv 的两个万相特价变体（渠道 71）：名字以 "wan3.0-video" 开头，
    // 会落进官网分支拿 480P —— 但客美的缺省是 **720P**，且 480P 是最低价档
    // （tj ¥0.138/秒 vs 720P ¥0.25/秒），示例是客户直接照抄的东西，
    // 写 480P 会照抄出比预期便宜但分辨率更低的单子。
    assert.equal(getWan3SampleResolution('wan3.0-video-tj'), '720P')
    assert.equal(
      getWan3SampleResolution('wan3.0-video-prime-tj'),
      '720P'
    )
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

describe('hk-wan3 参考视频免费标注', () => {
  // 全站唯一不收参考视频时长的档位（客美 tj 是「输出秒数 + 参考视频秒数」）。
  // 这条卖点必须排在第一条并走醒目样式，否则会和时长/分辨率标签混在一起被忽略。
  test('免费标注排在第一条高亮', () => {
    const keys = getModelHighlightKeys(autoDLModel('hk-wan3'))
    assert.equal(keys[0], 'Reference video is free')
  })

  test('该高亮被登记进醒目样式名单', () => {
    // key 拼错或漏登记时会退回普通灰色标签，卖点就失效了。
    assert.ok(PROMO_HIGHLIGHT_KEYS.has('Reference video is free'))
  })

  test('中文有对应翻译，不会退回英文原文', () => {
    assert.equal(zh.translation['Reference video is free'], '参考视频不收费')
  })
})
