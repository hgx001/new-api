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
 * 前端促销族定义 × 后端 Go 源码的**交叉校验**。
 *
 * ── 这道闸门存在的理由 ──────────────────────────────────────────────────
 *
 * 前端 `seedance25-family.ts` 是后端 `constant/seedance25_family.go` +
 * `relay/channel/task/youzanwan3/constants.go` 的**手工镜像**。
 *
 * 两边漂移的后果都是**静默**的：
 *   - 后端固定 30 秒、前端示例写 5 秒 → 客户照抄吃 400；
 *   - 前端显示「720P/1080P 可选」、后端只收 720P → 客户传 1080P 吃 400；
 *   - 促销族加了新成员、前端没登记 → 新模型的示例/参数表全错。
 *
 * 读源码而不是读运行时接口：/api/pricing 要起服务连数据库，且返回的是
 * **当前生产配置**，反映不了代码里的能力定义 —— 源码才是唯一来源。
 *
 * ⚠️ 这个测试与 billing-unit-crosscheck.test.ts 是**互补**的两道闸门：
 *   - 那道管「计价单位（/次 还是 /秒）」；
 *   - 这道管「档位参数（时长、分辨率）与族成员」。
 */
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

import {
  SEEDANCE25_HUODONG,
  SEEDANCE25_TEHUI,
  getSeedance25SampleResolution,
  getSeedance25SampleSpec,
  isSeedance25FamilyMember,
} from './seedance25-family'

/**
 * 后端源码路径（仓库根）。
 *
 * 目录层级：src/features/pricing/lib → src/features/pricing → src/features
 * → src → default → web → 仓库根。所以要往上走 6 层。
 *
 * ⚠️ 用 import.meta.url 而不是 import.meta.dir：后者是 Bun 独有的，
 * tsgo（TypeScript）不认，会直接报 TS2339。
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

const BACKEND_FAMILY_FILE = join(REPO_ROOT, 'constant', 'seedance25_family.go')
const BACKEND_YOUZAN_CONSTANTS = join(
  REPO_ROOT,
  'relay',
  'channel',
  'task',
  'youzanwan3',
  'constants.go'
)
const BACKEND_ROUTE_FILE = join(REPO_ROOT, 'middleware', 'seedance_route.go')

/**
 * 解析 Go 源码里的 const 块，取出 `名字 = "字面值"`。
 */
function parseGoConstStrings(source: string): Map<string, string> {
  const constants = new Map<string, string>()
  const constBlock = source.match(/const \(([\s\S]*?)\n\)/)
  if (!constBlock) return constants
  const pattern = /^\s*(\w+)\s*=\s*"([^"]*)"/gm
  let match: RegExpExecArray | null
  while ((match = pattern.exec(constBlock[1])) !== null) {
    constants.set(match[1], match[2])
  }
  return constants
}

/**
 * 解析 Go 源码里的 `map[string]bool{ ... }` 成员。
 */
function parseGoBoolMap(source: string, varName: string): string[] {
  const startMarker = `var ${varName} = map[string]bool{`
  const start = source.indexOf(startMarker)
  assert.notEqual(
    start,
    -1,
    `后端 constant/seedance25_family.go 里找不到 ${varName}`
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
 * 解析 Go 源码里的 `const 名字 = 数字`（无引号）。
 */
function parseGoIntConst(source: string, name: string): number {
  const pattern = new RegExp(`^\\s*${name}\\s*=\\s*(\\d+)\\s*$`, 'm')
  const match = source.match(pattern)
  assert.ok(
    match,
    `后端 youzanwan3/constants.go 里找不到 const ${name} —— ` +
      '前端交叉校验依赖这个常量，请确认它没被改名或移走'
  )
  return Number(match[1])
}

/**
 * 解析 Go 源码里的 `const 名字 = "字面值"`（顶层单行）。
 */
function parseGoStringConst(source: string, name: string): string {
  const pattern = new RegExp(`^\\s*${name}\\s*=\\s*"([^"]*)"\\s*$`, 'm')
  const match = source.match(pattern)
  assert.ok(match, `后端 youzanwan3/constants.go 里找不到 const ${name}`)
  return match[1]
}

/**
 * 解析 Go 源码里的 `const 名字 = constant.别的常量` 这种**派生声明**。
 *
 * 返回被引用的常量名；不是派生声明时返回 null。
 *
 * ⚠️ 为什么要专门解析派生关系：后端把「固定 30 秒」收敛成
 * constant.Seedance25PerCallFixedSeconds 一个来源，youzanwan3 与 middleware
 * 都从它派生。这之后 youzanwan3/constants.go 里写的是
 * `perCallFixedDuration = constant.Seedance25PerCallFixedSeconds`，
 * **不再有字面量 30** —— 旧解析器会直接匹配失败。
 *
 * 前端必须校验「最终生效的那个值」，而不是「某个副本」。
 * 所以这里沿派生链往下走，直到落到字面量。
 */
function parseGoDerivedConst(source: string, name: string): string | null {
  // 前缀 `const ` 可选：youzanwan3 的声明在 const 块内（`名字 = constant.X`），
  // middleware 的声明在顶层（`const 名字 = constant.X`）。两种都要认。
  const pattern = new RegExp(
    `^\\s*(?:const\\s+)?${name}\\s*=\\s*constant\\.(\\w+)\\s*$`,
    'm'
  )
  const match = source.match(pattern)
  return match ? match[1] : null
}

/**
 * 沿派生链解析出后端**最终生效**的固定时长（秒）。
 *
 * 走的是真实生效路径：
 *   youzanwan3.perCallFixedDuration
 *     → constant.Seedance25PerCallFixedSeconds
 *     → 字面量
 *
 * 中间任何一环断了（常量被改名、被移走、派生写错）都会在这里报错，
 * 而不是让前端拿一个「看起来对」的值蒙混过关。
 */
function resolveBackendFixedSeconds(
  youzanSource: string,
  familySource: string
): number {
  // 第 1 环：youzanwan3 里是不是派生写法？
  const derived = parseGoDerivedConst(youzanSource, 'perCallFixedDuration')
  if (derived) {
    // 第 2 环：派生目标在 constant 包里是什么值？
    const value = parseGoIntConst(familySource, derived)
    assert.notEqual(
      value,
      0,
      `后端 ${derived} 不应解析为 0 —— 固定档时长为 0 会让所有请求时长校验失效`
    )
    return value
  }
  // 非派生写法：直接读字面量（向后兼容）。
  const literal = parseGoIntConst(youzanSource, 'perCallFixedDuration')
  assert.notEqual(literal, 0, '后端固定档时长不应为 0')
  return literal
}

/**
 * 沿派生链解析出后端**最终生效**的固定分辨率。
 */
function resolveBackendFixedResolution(
  youzanSource: string,
  familySource: string
): string {
  const derived = parseGoDerivedConst(
    youzanSource,
    'perCallFixedResolution'
  )
  if (derived) {
    return parseGoConstStrings(familySource).get(derived) ?? ''
  }
  return parseGoStringConst(youzanSource, 'perCallFixedResolution')
}

describe('前端促销族定义 × 后端 Go 源码', () => {
  const familySource = readFileSync(BACKEND_FAMILY_FILE, 'utf-8')
  const youzanSource = readFileSync(BACKEND_YOUZAN_CONSTANTS, 'utf-8')

  const familyConstants = parseGoConstStrings(familySource)
  const familyMembers = parseGoBoolMap(familySource, 'Seedance25Family').map(
    (identifier) => {
      const value = familyConstants.get(identifier)
      assert.ok(value !== undefined, `无法解析常量 ${identifier}`)
      return value
    }
  )

  const perCallMembers = parseGoBoolMap(
    familySource,
    'Seedance25PerCallModels'
  ).map((identifier) => {
    const value = familyConstants.get(identifier)
    assert.ok(value !== undefined, `无法解析常量 ${identifier}`)
    return value
  })

  const perSecondMembers = parseGoBoolMap(
    familySource,
    'Seedance25PerSecondModels'
  ).map((identifier) => {
    const value = familyConstants.get(identifier)
    assert.ok(value !== undefined, `无法解析常量 ${identifier}`)
    return value
  })

  test('后端源码可解析（测试自身的前置条件）', () => {
    assert.ok(familyMembers.length >= 2)
    assert.ok(perCallMembers.length >= 1)
    assert.ok(perSecondMembers.length >= 1)
  })

  // 核心断言：前端认定的族成员必须与后端完全一致（不多不少）。
  test('前端族成员与后端 Seedance25Family 完全一致', () => {
    const frontend = [SEEDANCE25_TEHUI, SEEDANCE25_HUODONG]
    assert.deepEqual(
      [...frontend].sort(),
      [...familyMembers].sort(),
      '前端促销族成员与后端能力组不一致 —— ' +
        '请同步 seedance25-family.ts 的 SEEDANCE25_FAMILY_MEMBERS'
    )
    for (const name of frontend) {
      assert.equal(
        isSeedance25FamilyMember(name),
        true,
        `${name} 应被前端识别为促销族成员`
      )
    }
  })

  // 后端加了新成员，前端必须同步 —— 否则新模型的示例/参数表全错。
  test('后端新增族成员时前端必须同步登记', () => {
    for (const name of familyMembers) {
      assert.equal(
        isSeedance25FamilyMember(name),
        true,
        `后端把 ${name} 加进了促销族，前端还没登记 —— ` +
          '请把它加进 seedance25-family.ts 的 SEEDANCE25_FAMILY_MEMBERS'
      )
    }
  })

  // 计价口径必须一致（与 billing-unit-crosscheck 呼应，但这里查族成员口径）。
  test('后端按次成员在前端也是按次（不在按秒口径里）', () => {
    for (const name of perCallMembers) {
      assert.equal(
        getSeedance25SampleSpec(name) !== null || name === SEEDANCE25_HUODONG,
        true,
        `后端把 ${name} 定义为按次档，前端必须给它固定档定义`
      )
    }
    for (const name of perSecondMembers) {
      assert.equal(
        getSeedance25SampleSpec(name),
        null,
        `后端把 ${name} 定义为按秒档（时长可选），前端不能把它当固定档`
      )
    }
  })

  // 固定时长：前端必须与后端**最终生效**的值一致（沿派生链追溯）。
  test('前端固定时长与后端 perCallFixedDuration 一致', () => {
    const backendSeconds = resolveBackendFixedSeconds(youzanSource, familySource)
    const spec = getSeedance25SampleSpec(SEEDANCE25_HUODONG)
    assert.ok(spec, '活动版必须有固定档定义')
    assert.equal(
      spec.seconds,
      backendSeconds,
      `前端固定时长 ${spec.seconds} 与后端 ${backendSeconds} 不一致 —— ` +
        '客户照抄示例会被后端 400 拒掉'
    )
  })

  // 固定分辨率：前端必须与后端**最终生效**的值一致（沿派生链追溯）。
  test('前端固定分辨率与后端 perCallFixedResolution 一致', () => {
    const backendResolution = resolveBackendFixedResolution(
      youzanSource,
      familySource
    )
    const spec = getSeedance25SampleSpec(SEEDANCE25_HUODONG)
    assert.ok(spec)
    assert.ok(
      backendResolution,
      '后端固定分辨率解析为空 —— 派生链断了，请检查 constant 包的常量'
    )
    assert.equal(
      spec.resolution,
      backendResolution,
      `前端固定分辨率 ${spec.resolution} 与后端 ${backendResolution} 不一致`
    )
    assert.equal(
      getSeedance25SampleResolution(SEEDANCE25_HUODONG),
      backendResolution,
      '前端示例分辨率必须与后端固定档一致'
    )
  })

  // 派生链守卫：后端必须把固定档常量收敛到 constant 包的单一来源。
  //
  // ⚠️ 这条不是「值对不对」，是「结构对不对」——
  // 如果哪天有人把 youzanwan3 的派生改回手写字面量，值可能还对（都是 30），
  // 但「源链路」和「路由层」就又变成两份独立副本，漂移风险回来了。
  // 所以这里显式要求：路由层与源链路都必须派生自 constant 的同一个常量。
  test('后端固定档常量收敛到 constant 单一来源', () => {
    assert.equal(
      parseGoDerivedConst(youzanSource, 'perCallFixedDuration'),
      'Seedance25PerCallFixedSeconds',
      'youzanwan3.perCallFixedDuration 必须派生自 constant 的单一来源 —— ' +
        '手写字面量会让源链路门控与路由层归一各自漂移'
    )
    assert.equal(
      parseGoDerivedConst(youzanSource, 'perCallFixedResolution'),
      'Seedance25PerCallFixedResolution',
      'youzanwan3.perCallFixedResolution 必须派生自 constant 的单一来源'
    )

    // 路由层（middleware）也必须派生自同一个常量。
    const routeSource = readFileSync(BACKEND_ROUTE_FILE, 'utf-8')
    assert.equal(
      parseGoDerivedConst(routeSource, 'perCallFixedTierSeconds'),
      'Seedance25PerCallFixedSeconds',
      'middleware.perCallFixedTierSeconds 必须派生自 constant 的单一来源'
    )
  })
})
