/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the License,
or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * 前端 CapCut 五档定义 × 后端 Go 源码的**交叉校验**。
 *
 * ── 这道闸门存在的理由 ──────────────────────────────────────────────────
 *
 * 前端 `capcut-family.ts` 是后端 `constant/capcut_family.go` +
 * `relay/channel/task/manwu/adaptor.go`（时长上界表 / 720P 倍率表）的
 * **手工镜像**。
 *
 * 两边漂移的后果都是**静默**的：
 *   - 后端 2.5 系列 30 秒、前端写 15 秒 → 客户生成 20 秒被 400；
 *   - 后端 mini/fast 720P 是 ×1.5、前端按 ×2 算展示价 → 展示价与实收不符；
 *   - 后端加了第六档、前端没登记 → 新模型在广场上显示成「/次」或参数全错。
 *
 * 读源码而不是读运行时接口：/api/pricing 要起服务连数据库，且返回的是
 * **当前生产配置**，反映不了代码里的能力定义 —— 源码才是唯一来源。
 */
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

import {
  CAPCUT_20,
  CAPCUT_20_FAST,
  CAPCUT_20_MINI,
  CAPCUT_25,
  CAPCUT_25_FLASH,
  getCapcutSampleSpec,
  isCapcutFamilyMember,
} from './capcut-family'

/**
 * 后端源码路径（仓库根）。
 *
 * 目录层级：src/features/pricing/lib → src/features/pricing → src/features
 * → src → default → web → 仓库根。所以要往上走 6 层。
 */
const REPO_ROOT = join(
  fileURLToPath(new URL('.', import.meta.url)),
  '..',
  '..',
  '..',
  '..',
  '..',
  '..'
)

const BACKEND_FAMILY_FILE = join(REPO_ROOT, 'constant', 'capcut_family.go')
const BACKEND_ADAPTOR_FILE = join(
  REPO_ROOT,
  'relay',
  'channel',
  'task',
  'manwu',
  'adaptor.go'
)

/**
 * 解析 Go 源码里的**所有** const 块，取出 `名字 = "字面值"`。
 *
 * ⚠️ 必须扫全部块：`manwu/adaptor.go` 里 CapCut 的公开名常量在第 178 行
 * 那个块里，而文件开头还有好几个 const 块 —— 只取第一个会漏掉它们。
 */
function parseGoConstStrings(source: string): Map<string, string> {
  const constants = new Map<string, string>()
  const constBlock = /const \(([\s\S]*?)\n\)/g
  const pattern = /^\s*(\w+)\s*=\s*"([^"]*)"/gm
  let blockMatch: RegExpExecArray | null
  while ((blockMatch = constBlock.exec(source)) !== null) {
    pattern.lastIndex = 0
    let match: RegExpExecArray | null
    while ((match = pattern.exec(blockMatch[1])) !== null) {
      constants.set(match[1], match[2])
    }
  }
  return constants
}

/**
 * 解析 Go 源码里的 `map[string]bool{ ... }` 成员（返回常量名列表）。
 */
function parseGoBoolMap(source: string, varName: string): string[] {
  const startMarker = `var ${varName} = map[string]bool{`
  const start = source.indexOf(startMarker)
  assert.notEqual(
    start,
    -1,
    `后端 constant/capcut_family.go 里找不到 ${varName}`
  )
  const end = source.indexOf('\n}', start)
  assert.notEqual(end, -1, `${varName} 的 map 字面量没有闭合`)

  const body = source.slice(start + startMarker.length, end)
  const members: string[] = []
  const entryPattern = /^\s*(\w+):\s*true\s*,?\s*$/gm
  let match: RegExpExecArray | null
  while ((match = entryPattern.exec(body)) !== null) {
    members.push(match[1])
  }
  return members
}

/**
 * 解析 Go 源码里的 `map[string]float64{ ... }` 成员（返回 [常量名, 倍率][]）。
 */
function parseGoFloatMap(
  source: string,
  varName: string
): Array<[string, number]> {
  const startMarker = `var ${varName} = map[string]float64{`
  const start = source.indexOf(startMarker)
  assert.notEqual(
    start,
    -1,
    `后端 manwu/adaptor.go 里找不到 ${varName}`
  )
  const end = source.indexOf('\n}', start)
  assert.notEqual(end, -1, `${varName} 的 map 字面量没有闭合`)

  const body = source.slice(start + startMarker.length, end)
  const entries: Array<[string, number]> = []
  const entryPattern = /^\s*(\w+)\s*:\s*([0-9.]+)\s*,?\s*$/gm
  let match: RegExpExecArray | null
  while ((match = entryPattern.exec(body)) !== null) {
    entries.push([match[1], Number(match[2])])
  }
  return entries
}

/**
 * 解析 Go 源码里的**数字**常量（无引号），如 `capcutMaxDuration = 15`。
 *
 * ⚠️ 与 parseGoConstStrings 分开：后端 adaptor.go 里，公开名是字符串常量
 * （`ModelCapcut25 = "cp-seedance-2.5"`），而时长上界是数字常量
 * （`capcutMaxDuration25 = 30`）—— 一个解析器吃不下两种。
 */
function parseGoIntConstants(source: string): Map<string, number> {
  const constants = new Map<string, number>()
  const constBlock = /const \(([\s\S]*?)\n\)/g
  const pattern = /^\s*(\w+)\s*=\s*(\d+)\s*$/gm
  let blockMatch: RegExpExecArray | null
  while ((blockMatch = constBlock.exec(source)) !== null) {
    pattern.lastIndex = 0
    let match: RegExpExecArray | null
    while ((match = pattern.exec(blockMatch[1])) !== null) {
      constants.set(match[1], Number(match[2]))
    }
  }
  return constants
}

/**
 * 解析 Go 源码里的 `map[string]int{ ... }` 成员（返回 [常量名, 数值][]）。
 */
function parseGoIntMap(
  source: string,
  varName: string
): Array<[string, number]> {
  const startMarker = `var ${varName} = map[string]int{`
  const start = source.indexOf(startMarker)
  assert.notEqual(
    start,
    -1,
    `后端 manwu/adaptor.go 里找不到 ${varName}`
  )
  const end = source.indexOf('\n}', start)
  assert.notEqual(end, -1, `${varName} 的 map 字面量没有闭合`)

  const body = source.slice(start + startMarker.length, end)
  const entries: Array<[string, number]> = []
  const entryPattern = /^\s*(\w+)\s*:\s*(\w+)\s*,?\s*$/gm
  let match: RegExpExecArray | null
  while ((match = entryPattern.exec(body)) !== null) {
    entries.push([match[1], match[2]])
  }
  return entries
}

describe('前端 CapCut 五档定义 × 后端 Go 源码', () => {
  const familySource = readFileSync(BACKEND_FAMILY_FILE, 'utf-8')
  const adaptorSource = readFileSync(BACKEND_ADAPTOR_FILE, 'utf-8')

  const familyConstants = parseGoConstStrings(familySource)
  const adaptorConstants = parseGoConstStrings(adaptorSource)
  const adaptorIntConstants = parseGoIntConstants(adaptorSource)

  const familyMembers = parseGoBoolMap(familySource, 'CapcutFamily').map(
    (identifier) => {
      const value = familyConstants.get(identifier)
      assert.ok(value !== undefined, `无法解析常量 ${identifier}`)
      return value
    }
  )

  const perSecondMembers = parseGoBoolMap(
    familySource,
    'CapcutPerSecondModels'
  ).map((identifier) => {
    const value = familyConstants.get(identifier)
    assert.ok(value !== undefined, `无法解析常量 ${identifier}`)
    return value
  })

  test('后端源码可解析（测试自身的前置条件）', () => {
    assert.equal(familyMembers.length, 5, 'CapCut 应为五档')
    assert.equal(perSecondMembers.length, 5, 'CapCut 五档应全部按秒')
  })

  // 核心断言：前端认定的族成员必须与后端完全一致（不多不少）。
  test('前端族成员与后端 CapcutFamily 完全一致', () => {
    const frontend = [
      CAPCUT_20_MINI,
      CAPCUT_20_FAST,
      CAPCUT_20,
      CAPCUT_25_FLASH,
      CAPCUT_25,
    ]
    assert.deepEqual(
      [...frontend].sort(),
      [...familyMembers].sort(),
      '前端 CapCut 族成员与后端能力组不一致 —— ' +
        '请同步 capcut-family.ts 的 CAPCUT_FAMILY_MEMBERS'
    )
    for (const name of frontend) {
      assert.equal(
        isCapcutFamilyMember(name),
        true,
        `${name} 应被前端识别为 CapCut 族成员`
      )
    }
  })

  // 后端加了新成员，前端必须同步。
  test('后端新增族成员时前端必须同步登记', () => {
    for (const name of familyMembers) {
      assert.equal(
        isCapcutFamilyMember(name),
        true,
        `后端把 ${name} 加进了 CapCut 族，前端还没登记 —— ` +
          '请把它加进 capcut-family.ts 的 CAPCUT_FAMILY_MEMBERS'
      )
    }
  })

  // 五档全部按秒 —— 不登记会让广场把 ¥0.05/秒 挂在「/次」下。
  test('后端按秒成员在前端也按秒', () => {
    for (const name of perSecondMembers) {
      assert.equal(
        isCapcutFamilyMember(name),
        true,
        `${name} 是按秒档，前端必须识别为族成员才能显示 /秒`
      )
      assert.notEqual(
        getCapcutSampleSpec(name),
        null,
        `${name} 缺少参数规格，示例代码会给出非法参数`
      )
    }
  })

  // 时长上界：前端表必须与后端 capcutMaxDurations 一致。
  test('前端时长上界与后端 capcutMaxDurations 一致', () => {
    const backendDurations = parseGoIntMap(adaptorSource, 'capcutMaxDurations')
    assert.equal(backendDurations.length, 5, '后端时长表应有五档')

    for (const [identifier, rawValue] of backendDurations) {
      // 后端表的值可能是字面量数字，也可能是派生常量名（如 capcutMaxDuration25）。
      const literal = rawValue.match(/^\d+$/)
        ? Number(rawValue)
        : Number(adaptorIntConstants.get(rawValue))
      assert.ok(
        literal > 0,
        `后端 ${rawValue} 解析不出有效时长（constant=${adaptorIntConstants.get(rawValue)}）`
      )

      // 常量名 → 模型名：后端用 ModelCapcut25 这类标识符做键。
      const modelName = adaptorConstants.get(identifier)
      assert.ok(modelName, `无法解析后端时长表的键 ${identifier}`)

      const spec = getCapcutSampleSpec(modelName)
      assert.ok(spec, `前端缺少 ${modelName} 的参数规格`)
      assert.equal(
        spec.maxSeconds,
        literal,
        `前端时长上界 ${spec.maxSeconds} 与后端 ${literal} 不一致（${modelName}）—— ` +
          '客户生成超上界的时长会被 400'
      )
    }
  })

  // 720P 倍率：前端虽不直接展示倍率，但它是定价正确性的根基。
  test('后端 720P 倍率表覆盖五档且前端族成员一一对应', () => {
    const backendRatios = parseGoFloatMap(adaptorSource, 'capcut720PRatios')
    assert.equal(backendRatios.length, 5, '后端倍率表应有五档')

    const backendModels = backendRatios.map(([identifier]) => {
      const modelName = adaptorConstants.get(identifier)
      assert.ok(modelName, `无法解析后端倍率表的键 ${identifier}`)
      return modelName
    })
    assert.deepEqual(
      [...backendModels].sort(),
      [...familyMembers].sort(),
      '后端倍率表覆盖的模型与能力组成员不一致 —— ' +
        '会有一档 720P 算不出价'
    )
  })

  // 子串陷阱守卫：2.5 Flash 必须被当成独立档（30 秒），不能被 2.5 吞掉。
  test('cp-seedance-2.5flash 是独立档，不被 cp-seedance-2.5 吞掉', () => {
    assert.ok(
      CAPCUT_25_FLASH.includes(CAPCUT_25),
      '前提：2.5flash 的公开名确实包含 2.5 子串'
    )
    const flash = getCapcutSampleSpec(CAPCUT_25_FLASH)
    const full = getCapcutSampleSpec(CAPCUT_25)
    assert.ok(flash && full)
    assert.equal(
      flash.maxSeconds,
      30,
      '2.5 Flash 的时长上界应是 30 秒（官网 2.5 系列）'
    )
    assert.equal(
      full.maxSeconds,
      30,
      '2.5 的时长上界应是 30 秒（官网 2.5 系列）'
    )
    // mini/fast/2.0 是 15 秒档 —— 若有人用前缀匹配，2.5flash 会被错判成 15 秒。
    const mini = getCapcutSampleSpec(CAPCUT_20_MINI)
    assert.ok(mini)
    assert.equal(mini.maxSeconds, 15, '2.0 Mini 的时长上界应是 15 秒')
  })
})
