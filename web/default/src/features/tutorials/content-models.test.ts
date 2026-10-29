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

/**
 * 取出某个 level 3 小节的正文（该 heading 起，到下一个 heading 之前）。
 * 用于「只针对某一个模型」的断言 —— 在整份 sectionsJson 上做 includes 会被
 * 别的模型同名价格串扰（例如别名与 prime 同价）。
 */
function getModelSectionText(anchorId: string): string {
  for (const section of sections) {
    const nodes = section.content ?? []
    const start = nodes.findIndex((node) => node.type === 'heading' && node.id === anchorId)
    if (start === -1) continue
    const rest = nodes.slice(start + 1)
    const end = rest.findIndex((node) => node.type === 'heading')
    const body = end === -1 ? rest : rest.slice(0, end)
    return JSON.stringify([nodes[start], ...body])
  }
  throw new Error(`missing model section ${anchorId}`)
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
      'quota-query',
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
      'gpt-6.1-sol',
      'gpt-image-2.5-官方',
      'wan3.0-video',
      'wan3.0-video-prime',
      'wan3.0-video-官网',
      'autodl:minimax-h3-u24',
      'seedance-2.0',
      'seedance-2-0-fast',
      'seedance-2-5',
      'MiniMax-M3.1',
      '¥0.15 / 张',
      '¥0.23/秒',
      '¥0.10/s',
      '¥1.00 / 次',
      '¥3.00 / 次',
    ]) {
      assert.equal(sectionsJson.includes(expected), true, `missing ${expected}`)
    }
  })

  // 价格守卫：上面的用例只检查「某个价格字符串存在」，不检查「价格对不对」，
  // 所以 2026-10-13 之前 wan3.0-smart 与 wan3.0-video-官网 的错价（分别写成
  // ¥0.32/¥0.40 与 ¥0.53/¥0.85）一直没被拦住，官网 1080P 比实际低 27%。
  // 这里把每个分辨率档的实际单价钉死，依据：
  //   - youzanwan3/constants.go videoResolutionSizeRatio{480P:1.0,720P:1.0,1080P:1.0}
  //     基准 ¥0.23/秒（上游 0.18 + ¥0.05）→ 0.23 / 0.23 / 0.23
  //   - youzanwan3/constants.go primeResolutionSizeRatio{720P:1.0,1080P:1.2}
  //     每档加价 ¥0.10 → 0.25 / 0.30（2026-10-29 第二轮：720P 由 ¥0.20 上调）
  //   - seedance2.5-特惠 独立定价（720P ¥0.20 / 1080P ¥0.30，不跟 prime 联动），
  //     且**文案里不许出现 prime / wan3**（见「对外文案不暴露真实货源」用例）
  //   - wan3/constants.go resolutionSizeRatio{480P:1.0,720P:2.0,1080P:4.0}
  //     基准 ¥0.27/秒 → 0.27 / 0.54 / 1.08
  //   - autodl/constants.go u24 的 Resolutions 只含 480p/768p → 0.10 / 0.12，**没有 1080p**
  // 调价时必须同步改这里，否则测试会红。
  test('prices match the resolution tiers the adaptors actually charge', () => {
    const expected = [
      // 索引区与详情区都要写对，且两处必须一致
      '480P / 720P / 1080P 同价 ¥0.23/秒', // wan3.0-video 索引
      '按秒 ¥0.23（480P / 720P / 1080P 三档同价）', // wan3.0-video 详情
      '720P ¥0.25/s、1080P ¥0.30/s', // wan3.0-video-prime 索引
      '按秒 ¥0.25（720P）/ ¥0.30（1080P）', // wan3.0-video-prime 详情（别名同价，见下方专属用例）
      '480P ¥0.20/s、720P ¥0.45/s、1080P ¥0.80/s', // wan3.0-video-官网 索引
      '¥0.20 / ¥0.45 / ¥0.80（480P / 720P / 1080P）', // wan3.0-video-官网 详情
      '480p ¥0.10/s、768p ¥0.12/s（仅此两档）', // autodl H3 索引
      '480p ¥0.10、768p ¥0.12（仅这两档', // autodl H3 详情
    ]
    for (const price of expected) {
      assert.equal(
        sectionsJson.includes(price),
        true,
        `missing or stale price line: ${price}`
      )
    }

    // 已下线的旧价格必须不再出现，避免同时展示两套价。
    for (const stale of ['¥0.32 / ¥0.40', '¥0.53 / ¥0.85', '¥0.85/s', '¥0.28 / ¥0.28 / ¥0.30']) {
      assert.equal(
        sectionsJson.includes(stale),
        false,
        `stale price still documented: ${stale}`
      )
    }

    // 2026-10-26：wan-3.0 / wan-3.0-特惠 两个模型已下架
    // （渠道删除 + 卡片/定价清理），文档里不得再出现这两个名字。
    // ⚠️ seedance2.5-特惠 曾同在本次下架清单里，但 2026-10-29 它作为
    // wan3.0-video-prime 的对外别名重新上架（改路由到有赞 prime + 按秒计价），
    // 故从本守卫移除、重新计入下面的「广场在售模型」清单。
    for (const gone of ['wan-3.0｜', 'wan-3.0-特惠', 'model-wan-3-0']) {
      assert.equal(sectionsJson.includes(gone), false, `still documented: ${gone}`)
    }

    // autodl:minimax-h3-u24 的分辨率白名单（constants.go Resolutions）只有
    // 480p/768p 竖横/(1:1)，传 1080p 会被 invalid_resolution 拦下。
    // h3ResolutionRatios 里虽有 1080p 档，但那是给 u08/lightx2v 等其它模型用的，
    // u24 走不到 —— 写进文档就成了永远收不到钱的死价格。
    assert.equal(
      sectionsJson.includes('1080p ¥0.20'),
      false,
      'autodl u24 does not accept 1080p; documenting its price sends clients into invalid_resolution'
    )
  })

  // 回归守卫：seedance2.5-特惠 对外是**独立商品**，文案里不得出现真实货源。
  // 2026-10-29 首次上架时曾把它写成「wan3.0-video-prime 的对外别名，同一模型」，
  // 等于把上游直接告诉客户 —— 前面日志脱敏（删 upstream_model_name、抹错误文案、
  // 成片走本站代理）全部白做，客户拿文案一对就知道该盯哪个模型。
  // 价格仍必须与 prime 一致（同货不同名不能不同价），由本用例单独钉死。
  test('seedance2.5-特惠 的对外文案不暴露真实货源', () => {
    const section = getModelSectionText('model-seedance25-tehui')
    const indexEntry = sectionsJson.match(/"seedance2\.5-特惠｜[^"]*"/)?.[0] ?? ''
    assert.notEqual(indexEntry, '', '模型总览里缺少 seedance2.5-特惠 一行')

    for (const [label, text] of [
      ['模型总览', indexEntry],
      ['教程小节', section],
    ] as const) {
      for (const leaked of ['wan3', 'wan-3', 'prime', '有赞', 'youzan', 'aliyun', '万相']) {
        assert.equal(
          text.toLowerCase().includes(leaked),
          false,
          `${label} 暴露了真实上游关键词「${leaked}」：${text}`
        )
      }
    }

    // 价格：按秒分档，**独立定价**（2026-10-29 老板明确：不跟 prime 联动）。
    // 720P ¥0.20 / 1080P ¥0.30，与 prime 现在的 ¥0.25 / ¥0.30 只差 720P 一档。
    assert.equal(section.includes('按秒 ¥0.20（720P）/ ¥0.30（1080P）'), true)
    assert.equal(indexEntry.includes('720P ¥0.20/s、1080P ¥0.30/s'), true)
  })

  test('documents the quota query endpoint, fields, limits, and HTTP semantics', () => {
    const quotaSection = sections.find((section) => section.id === 'quota-query')
    assert.ok(quotaSection, 'missing quota-query section')
    for (const expected of [
      'GET /v1/dashboard/billing/quota',
      'Authorization: Bearer <API_KEY>',
      'effective_remain',
      'remain_token',
      'Cache-Control: no-store',
      '每用户 6 次 / 60 秒',
      '429 不带 Retry-After',
      '不扣费、不记模型调用日志',
    ]) {
      assert.equal(sectionsJson.includes(expected), true, `missing ${expected}`)
    }
    assert.equal(collectAnchorIds().has('quota-query'), true)
    assert.equal(
      getDocIndex().some((entry) => entry.href === '#quota-query'),
      true,
      'quota query section must appear in the tutorial index'
    )
    assert.equal(
      sectionsJson.includes(`${platformUrl}/v1/dashboard/billing/quota`),
      true,
      'quota curl example must use the configured platform URL'
    )
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
      // '仅 5 或 10' / '必填，1–3 张参考图' 原本来自 wan2.7-r2v 的参数表，
      // '固定 1080P' 原本来自 wan3.0-video-prime-1080p 的参数表，
      // '固定 30' 原本来自 wan-3.0-特惠 / seedance2.5-特惠 的参数表。
      // 这几个模型先后下架后随章节一并移除（见下方 de-listed 守卫）。
      '必填，1–9 张',
      '首尾帧不能与普通参考图/视频/音频混用',
    ]) {
      assert.equal(sectionsJson.includes(expected), true, `missing ${expected}`)
    }
  })

  // 回归守卫：wan3.0-video-官网 曾被文档描述成「仅支持文生视频，传入参考图/视频/
  // 音频会被忽略（不会报错）」，这是错的。wan3/adaptor.go 的 buildWan3Media +
  // validateWan3Media 走的是和 youzanwan3 完全相同的素材解析与校验，并经
  // Media 字段真实下发上游，限额为图 ≤10 / 视频 ≤5 / 音频 ≤5。
  // 按旧文档接入的客户会白白放弃已经可用的图生视频与多模态参考能力。
  test('does not claim the official wan3 channel ignores reference media', () => {
    // 不能只断言「会被忽略」四个字：「传其它值会被忽略」这类描述对固定档位的模型是
    // 合法的（原先的 wan3.0-video-prime-1080p 就是这种，已于 2026-10-25 下架）。
    // 要断的是官网那条具体的错误断言。
    assert.equal(sectionsJson.includes('仅支持文生视频'), false)
    assert.equal(
      sectionsJson.includes('需要图生视频或多模态参考请改用 wan3.0-smart'),
      false,
      'wan3.0-video-官网 supports reference media; do not steer clients away from it'
    )
    assert.equal(
      sectionsJson.includes('本渠道同样支持图生视频与多模态参考'),
      true
    )
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

  // 回归守卫：wan3.0-video-官网 带参考视频时按「生成秒数 + 参考视频总秒数（向上取整）」
  // 同分辨率单价计费（wan3/adaptor.go EstimateBilling + video_duration.go 探测），
  // 解析不出时长直接 400 invalid_input_reference（不建单不扣费）。文档必须如实描述，
  // 否则客户会按纯生成秒数理解账单。
  test('documents official wan3 reference-video surcharge billing', () => {
    assert.equal(
      sectionsJson.includes('生成秒数 + 参考视频总秒数（四舍五入）'),
      true
    )
    assert.equal(
      sectionsJson.includes('0.20 × 20 = ¥4.00'),
      true
    )
    assert.equal(
      sectionsJson.includes('无法解析时长的参考视频会被拒绝'),
      true
    )
  })

  // 配额不足在网关里是 403 insufficient_user_quota，不是 402。
  // seedance-2.0 与 gemini-web-video 均按次计费，文档不能再出现按秒写法。
  test('documents remote video models as per-request billing', () => {
    assert.equal(sectionsJson.includes('¥2.50/s'), false)
    assert.equal(sectionsJson.includes('¥1.00 / 次'), true)
  })

  // seedance-2.0 时长口径：5–15 秒区间逐秒透传（2026-10-24 曾锁定 10 秒，
  // 2026-10-26 恢复为区间）。文档必须写对区间，否则客户按 5 秒提交会被网关
  // 400（invalid_duration）后以为是自己写错了参数。
  test('documents seedance-2.0 as a 5-15 second per-second model', () => {
    assert.equal(
      sectionsJson.includes('seedance-2.0｜视频（按次）｜¥1.00 / 次，支持 5–15 秒逐秒'),
      true,
      'the model overview must state that seedance-2.0 supports 5-15 seconds'
    )
    assert.equal(
      sectionsJson.includes('seconds / duration：5–15 秒逐秒，默认 15'),
      true,
      'the seedance-2.0 parameter section must state the 5-15 second range'
    )
    assert.equal(
      sectionsJson.includes('固定 10 秒'),
      false,
      'seedance-2.0 must no longer be documented as fixed at 10 seconds'
    )
  })

  // 回归守卫：模型广场上架的模型必须逐个写进接入教程。
  // 2026-10-25 核对发现广场有 3 个模型教程里没写（MiniMax-M3.1 / wan-3.0 /
  // seedance2.5-特惠），客户按教程找不到入口。清单按生产 /api/pricing 当前返回逐个列出，
  // 以后再上新模型必须同步补本文件 + content-models.ts，否则这里会红。
  test('documents every model currently listed in the model square', () => {
    const squareModels = [
      'MiniMax-M3',
      'MiniMax-M3.1',
      'Nano Banana Pro',
      'autodl:minimax-h3-u24',
      'gemini-web-video',
      'gpt-6-luna',
      'gpt-6-sol',
      'gpt-6.1-sol',
      'gpt-image-2.5-官方',
      'seedance-2-0-fast',
      'seedance-2-5',
      'seedance-2.0',
      'seedance2.5-特惠',
      'wan2.7-图生',
      'wan2.7-编辑',
      'wan3.0-video',
      'wan3.0-video-prime',
      'wan3.0-video-官网',
    ]
    for (const model of squareModels) {
      assert.equal(
        sectionsJson.includes(`${model}｜`),
        true,
        `model square lists ${model} but the integration guide never mentions it`
      )
    }
  })

  // 回归守卫：wan2.7 免灵感值双模型 2026-10-16 上架（图生 / 编辑，¥0.30/次）。
  // 文档必须写出慢队列交付预期（「通常 3 小时左右出一条」），否则客户会按分钟级
  // 预期接入、白白等待；也必须在索引与 FAQ 实测耗时两处都写，单写一处不一致。
  test('documents the wan2.7 relax models with their 3-hour delivery expectation', () => {
    for (const expected of ['wan2.7-图生', 'wan2.7-编辑', '¥0.30 / 次']) {
      assert.equal(sectionsJson.includes(expected), true, `missing ${expected}`)
    }
    assert.equal(
      sectionsJson.includes('免灵感值慢队列，通常 3 小时左右出一条'),
      true,
      'wan2.7 relax models must document the ~3h queue expectation in the overview'
    )
    assert.equal(
      sectionsJson.includes('wan2.7-图生 / wan2.7-编辑 需 3 小时左右'),
      true,
      'wan2.7 relax models must appear in the FAQ measured-latency line'
    )
    // 上游真名不得出现在对外文档：公开名只存在于 adaptor 的 modelSpecs。
    for (const hidden of ['Wan2_7', 'wan2.7-i2v', 'wan2.7-edit']) {
      assert.equal(sectionsJson.includes(hidden), false, `${hidden} must not be documented`)
    }
  })

  // 回归守卫：三个静默失败点。它们都不会报错，客户端只会看到任务正常跑完，
  // 但实际素材根本没到上游，属于最难排查的一类。
  //   1. 顶层 audios / audio 只有 autodl 读；youzanwan3 与 wan3 的 buildMedia
  //      均不读 req.Audios / req.Audio，进了 validateMedia 都到不了。
  //   2. metadata.audio 是音轨开关（bool），与顶层 audios（素材）同名不同义。
  //   3. images / input_reference / image 在 youzanwan3 里被强制标成
  //      reference_image；只有 wan3 走 inferLegacyMediaType 按 URL 后缀推断。
  // 实测：youzanwan3 与 wan3 的 req.Audios/req.Audio 命中数均为 0，
  //      autodl 为 3；youzanwan3 的 inferLegacyMediaType 为 0，wan3 为 2。
  test('warns about the silently-ignored media fields for the wan3 models', () => {
    for (const expected of [
      '顶层 audios / audio：',
      '仅 autodl:minimax-h3-u24 生效',
      '传了会被静默丢弃',
      // ⚠️ 2026-10-28：metadata.audio 此前被描述成「wan3 系通用开关」，实际只有
      // 官网渠道读它；有赞 V1 协议不读这个字段（传了被静默丢弃）。
      'metadata.audio（是否生成音轨）只对 wan3.0-video-官网 生效',
      '有赞 wan3.0-video / wan3.0-video-prime 不读这个字段',
      '只有 wan3.0-video-官网 会按 URL 后缀推断类型',
      '传参考视频、参考音频只有这一条路',
      '「传图片给视频模型」的三种写法',
      '优先级 media > metadata.media > 旧字段',
      '首尾帧是图片的一种特殊类型',
      '请改用 reference_image',
      // Nano Banana Pro 走 /v1/videos 且支持 n：客户端若按图片接口习惯
      // 直接 POST /v1/images/generations 会拿不到模型，这两点之前都没写。
      '本模型走 /v1/videos 异步任务（不是 /v1/images/generations）',
      '其余 OpenAI 图片字段（quality / style / background / output_format 等）会被透传给上游',
    ]) {
      assert.equal(sectionsJson.includes(expected), true, `missing ${expected}`)
    }
  })

  // 回归守卫：wan3 系传参考素材此前只有字段列表、没有可照抄的请求示例，
  // 下游开发不知道 media 数组怎么写。文档必须给出图生视频（首帧）与多模态
  // 参考（图 + 视频 + 音频）的完整 JSON 示例，且示例要与 wan3/adaptor.go 的
  // TaskMedia{type,url} 结构一致（type 取值 reference_image / reference_video /
  // reference_audio / first_frame / last_frame），并说明提示词内 @素材N 引用。
  test('shows concrete media-array examples for passing image/video/audio to wan3', () => {
    const codeBlocks: string[] = []
    for (const section of sections) {
      for (const node of section.content ?? []) {
        if (node.type === 'codeBlock') codeBlocks.push(node.value)
      }
    }
    const code = codeBlocks.join('\n')
    // 图生视频：首帧写法
    assert.equal(code.includes('"type": "first_frame"'), true, 'missing first_frame media example')
    // 多模态参考：图 + 视频 + 音频各一条完整示例
    assert.equal(code.includes('"type": "reference_image"'), true, 'missing reference_image media example')
    assert.equal(code.includes('"type": "reference_video"'), true, 'missing reference_video media example')
    assert.equal(code.includes('"type": "reference_audio"'), true, 'missing reference_audio media example')
    // 专讲「怎么传素材」的小节存在且进了文档目录
    assert.equal(collectAnchorIds().has('video-media-examples'), true, 'missing video-media-examples heading')
    // 提示词里 @视频1 / @音频1 的素材引用写法有示例
    assert.equal(sectionsJson.includes('@视频1'), true, 'missing @视频1 prompt reference')
    assert.equal(sectionsJson.includes('@音频1'), true, 'missing @音频1 prompt reference')
  })

  // 回归守卫：seedance-2.5（dola 站）已于 2026-10-10 下架（接口层 channels.models +
  // abilities 同时摘除），文档必须同步移除，否则客户照文档调用只会拿到
  // 503「分组 default 下模型 seedance-2.5 无可用渠道」。豆包站的 seedance-2-5
  // 是另一个模型名（带连字符），不受此守卫影响。
  test('does not document the de-listed dola seedance-2.5', () => {
    assert.equal(sectionsJson.includes('seedance-2.5'), false)
    assert.equal(sectionsJson.includes('model-seedance-25'), false)
    assert.equal(sectionsJson.includes('¥0.80 / 次'), false)
  })

  // 回归守卫：2026-10-25 下架 wan3.0-1080p-慢速 / wan2.7-r2v / wan3.0-480p
  // （接口层 channels.models + abilities + 广场卡片 + ModelPrice 同时摘除）。
  // 文档必须同步移除，否则客户照文档调用只会拿到
  // 503「No available channel for model ...」。
  // ⚠️ 2026-10-26 wan2.7-r2v 恢复上架 → 它的两个名字从本守卫移除，
  //    并回到「广场在售模型必须逐个写进教程」那份清单里。
  // ⚠️ 2026-10-28 上游切换到 V1 协议，/v1/models 目录里已没有 wan2.7-r2v，
  //    该模型被**再次**彻底下架 → 名字重新加回本守卫，并从在售清单里移除。
  test('does not document the de-listed wan3.0 models', () => {
    for (const gone of [
      'wan3.0-1080p-慢速',
      'model-wan30-1080p-slow',
      'wan3.0-480p',
      'wan3.0-video-prime-1080p',
      'model-wan30-prime',
      'wan2.7-r2v',
    ]) {
      assert.equal(sectionsJson.includes(gone), false, `still documented: ${gone}`)
    }
    // 同前缀的在售模型不能被误删
    assert.equal(sectionsJson.includes('wan2.7-图生'), true)
    assert.equal(sectionsJson.includes('wan2.7-编辑'), true)
  })

  // 回归守卫：2026-10-12 豆包公开名去掉 db- 前缀，旧名退役后文档不得再出现，
  // 否则客户照旧文档调用只会拿到 400 invalid_model。
  test('does not document the retired db-seedance names', () => {
    assert.equal(sectionsJson.includes('db-seedance-2-0'), false)
    assert.equal(sectionsJson.includes('db-seedance-2-5'), false)
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
