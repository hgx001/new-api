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
/**
 * 示例代码的**可用性**守卫。
 *
 * ── 这道闸门存在的理由 ──────────────────────────────────────────────────
 *
 * 示例代码是客户**直接照抄**的东西。它一旦和后端校验口径不一致，
 * 客户照抄就吃 400，而报错信息（「this model has a fixed duration of
 * 30 seconds, got 5」）与示例里写的值对不上，排查成本极高。
 *
 * 这个坑已经真实存在过：`buildVideoSample` 对**所有**视频模型一律写
 * `seconds=5`，而 seedance2.5-活动 是**固定 30 秒**的按次档 ——
 * 后端对固定档是 fail-loud（不做就近吸附），所以照抄必 400。
 */
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { buildSample, type Lang } from './code-samples'
import {
  SEEDANCE25_HUODONG,
  SEEDANCE25_TEHUI,
  getSeedance25SampleSpec,
  isFixedTierModel,
} from './seedance25-family'

const VIDEO_LANGS: Lang[] = ['curl', 'python', 'javascript', 'typescript']

function videoSample(modelName: string, lang: Lang): string {
  return buildSample(lang, 'openai-video', {
    baseUrl: 'https://api.example.com',
    apiKeyEnv: 'NEW_API_KEY',
    modelName,
    endpointType: 'openai-video',
    endpointPath: '/v1/videos',
  })
}

/**
 * 从示例里**提取**实际写的秒数值。
 *
 * 为什么用提取而不是「包含 30」：
 *   - 「包含 30」会被 prompt 里别的数字误判为通过（假绿）；
 *   - 「不包含 5」在不同语言里分隔符不同（`seconds=5` / `("seconds", "5")` /
 *     `"seconds": "5"` / `form.append('seconds', '5')`），正则很难写全，
 *     漏掉一种写法就等于该语言没有守卫。
 *
 * 提取法把四种写法一次性覆盖，且断言的是**值**而不是**字面量**。
 */
function extractSeconds(sample: string): string[] {
  const patterns = [
    /-F "seconds=(\d+)"/g, // curl
    /\("seconds", "(\d+)"\)/g, // python tuple（带 resolution 的分支）
    /"seconds": "(\d+)"/g, // python dict（无 resolution 的分支）
    /form\.append\('seconds', '(\d+)'\)/g, // javascript / typescript
  ]
  const values: string[] = []
  for (const pattern of patterns) {
    let match: RegExpExecArray | null
    while ((match = pattern.exec(sample)) !== null) {
      values.push(match[1])
    }
  }
  return values
}

describe('固定档模型的示例代码', () => {
  // 核心断言：活动版固定 30 秒，示例必须写 30。
  //
  // 写 5 秒的话，客户照抄 → 后端 400
  // （「this model has a fixed duration of 30 seconds, got 5」）。
  test('seedance2.5-活动 的示例时长是 30 秒，不是 5 秒', () => {
    for (const lang of VIDEO_LANGS) {
      const sample = videoSample(SEEDANCE25_HUODONG, lang)
      const values = extractSeconds(sample)
      assert.equal(
        values.length,
        1,
        `${lang} 示例里应有且只有一个 seconds 字段，实际 ${values.length} 个：\n${sample}`
      )
      assert.equal(
        values[0],
        '30',
        `${lang} 示例写的是 ${values[0]} 秒，客户照抄会被后端 400 拒掉：\n${sample}`
      )
    }
  })

  // 逐语言精确断言：curl / python / javascript 三种写法不同，
  // 必须分别确认写对了，而不是靠「包含 30」蒙混过关。
  test('curl 示例写 seconds=30', () => {
    const sample = videoSample(SEEDANCE25_HUODONG, 'curl')
    assert.ok(
      sample.includes('-F "seconds=30"'),
      `curl 示例应写 seconds=30：\n${sample}`
    )
  })

  test('python 示例写 30 秒', () => {
    const sample = videoSample(SEEDANCE25_HUODONG, 'python')
    assert.ok(/"30"/.test(sample), `python 示例应写 30 秒：\n${sample}`)
    assert.ok(!/"5"/.test(sample), `python 示例不应出现 5 秒：\n${sample}`)
  })

  test('javascript 示例写 30 秒', () => {
    const sample = videoSample(SEEDANCE25_HUODONG, 'javascript')
    assert.ok(
      sample.includes("form.append('seconds', '30')"),
      `javascript 示例应写 form.append('seconds', '30')：\n${sample}`
    )
  })

  // 分辨率同理：活动版锁死 720P，传 1080P 会被 400 拒掉。
  test('seedance2.5-活动 的示例分辨率是 720P', () => {
    for (const lang of VIDEO_LANGS) {
      const sample = videoSample(SEEDANCE25_HUODONG, lang)
      assert.ok(sample.includes('720P'), `${lang} 示例应写 720P：\n${sample}`)
      assert.ok(
        !sample.includes('1080P'),
        `${lang} 示例不应出现 1080P —— 活动版锁死 720P，1080P 会被 400 拒掉：\n${sample}`
      )
    }
  })

  // 母本**不能**被误伤：它按秒计费、时长 2—30 秒可选，
  // 示例写 5 秒是对的（把母本也标成固定 30 秒会让想跑短片的客户被误导）。
  test('seedance2.5-特惠 的示例保持 5 秒（母本时长可选）', () => {
    for (const lang of VIDEO_LANGS) {
      const sample = videoSample(SEEDANCE25_TEHUI, lang)
      const values = extractSeconds(sample)
      assert.equal(
        values.length,
        1,
        `${lang} 示例里应有且只有一个 seconds 字段：\n${sample}`
      )
      assert.equal(
        values[0],
        '5',
        `${lang} 母本示例写的是 ${values[0]} 秒 —— 母本时长可选，应保持 5 秒：\n${sample}`
      )
    }
  })

  // 其它视频模型的示例不受影响（回归守卫）。
  test('其它模型示例保持 5 秒', () => {
    const curl = videoSample('wan3.0-video', 'curl')
    assert.ok(
      curl.includes('-F "seconds=5"'),
      `wan3.0-video 示例应保持 seconds=5：\n${curl}`
    )
  })
})

describe('前端固定档定义', () => {
  test('活动版是固定档：30 秒 / 720P', () => {
    const spec = getSeedance25SampleSpec(SEEDANCE25_HUODONG)
    assert.ok(spec, '活动版必须被识别为固定档')
    assert.equal(spec.seconds, 30)
    assert.equal(spec.resolution, '720P')
    assert.equal(isFixedTierModel(SEEDANCE25_HUODONG), true)
  })

  test('母本不是固定档（时长 2—30 秒可选）', () => {
    assert.equal(
      getSeedance25SampleSpec(SEEDANCE25_TEHUI),
      null,
      '母本按秒计费、时长可选，不能标成固定档'
    )
    assert.equal(isFixedTierModel(SEEDANCE25_TEHUI), false)
  })

  test('非促销族模型不是固定档', () => {
    assert.equal(getSeedance25SampleSpec('wan3.0-video'), null)
    assert.equal(getSeedance25SampleSpec('seedance-2-5'), null)
    assert.equal(isFixedTierModel('gpt-6-luna'), false)
  })
})

// 2026-11-07：SeetaCloud 对外模型换成 minimax_h3_v2 / minimax_h3_omni，
// 旧名退役为隐藏别名。若样例分发没跟着改，客户从模型广场复制到的会是通用
// 视频样例（带 input_reference 文件上传、resolution=768p竖），照抄直接 400。
describe('SeetaCloud H3 模型的示例', () => {
  for (const modelName of ['minimax_h3_v2', 'minimax_h3_omni']) {
    test(`${modelName} 的示例用档位+方向的 resolution 且不带参考图`, () => {
      for (const lang of VIDEO_LANGS) {
        const sample = videoSample(modelName, lang)
        assert.ok(
          sample.includes(modelName),
          `${lang} 示例缺模型名：\n${sample}`
        )
        assert.ok(
          sample.includes('480p横'),
          `${lang} 示例缺 480p横 档位：\n${sample}`
        )
        assert.ok(
          !sample.includes('768p竖'),
          `${lang} 示例混用了 autoDL 的档位：\n${sample}`
        )
        assert.ok(
          !sample.includes('input_reference'),
          `${lang} 示例不应带参考图文件（两模型都能纯文生）：\n${sample}`
        )
      }
    })
  }
})
