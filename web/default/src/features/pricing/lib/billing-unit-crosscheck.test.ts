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
 * 后端「按秒模型清单」× 前端「isPerSecondModel 穷举表」的交叉校验。
 *
 * ── 这道闸门存在的理由 ──────────────────────────────────────────────────
 *
 * 前端 `isPerSecondModel()` 是**手抄的穷举表**（见 model-helpers.ts 的注释：
 * 「has silently dropped models twice — `seedance-2-0-fast` (2026-10-12)
 * and `wan3.0-480p` (erchun) — both of which rendered a per-second price
 * under a "/ request" unit on the model square」）。
 *
 * 后果是**静默错价**：后端按秒结算、前端显示「/次」，客户看到 ¥0.20/秒
 * 的标价却以为是一次性收费，账单与标价对不上 —— 而且没有任何报错。
 *
 * ── 为什么本次能自动发现 ────────────────────────────────────────────────
 *
 * 后端把 seedance2.5 促销族的计费口径集中到了
 * `constant/seedance25_family.go`（唯一来源），本测试**直接读那份 Go 源码**，
 * 解析出按次/按秒两组成员，再与前端表的判定结果逐条比对。
 *
 * 所以「后端加了一个按秒模型、前端忘了登记」这种不一致，
 * 会在 CI 里当场变红，而不是等到客户投诉。
 *
 * ⚠️ 读源码而不是读运行时接口：/api/pricing 需要起服务、连数据库，
 * 单测里做不到；而且它返回的是**当前生产配置**，
 * 不能反映代码里的能力定义 —— 源码才是唯一来源。
 */
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

import type { PricingModel } from '../types'
import { isPerSecondModel } from './model-helpers'

/**
 * 后端能力组定义文件的路径（仓库根的 constant/seedance25_family.go）。
 *
 * 目录层级：src/features/pricing/lib → src/features/pricing → src/features
 * → src → default → web → 仓库根。所以要往上走 6 层。
 *
 * ⚠️ 用 import.meta.url 而不是 import.meta.dir：后者是 Bun 独有的，
 * tsgo（TypeScript）不认，会直接报 TS2339。
 */
const BACKEND_FAMILY_FILE = join(
  fileURLToPath(new URL('.', import.meta.url)),
  '..',
  '..',
  '..',
  '..',
  '..',
  '..',
  'constant',
  'seedance25_family.go'
)

/**
 * 解析 Go 源码里的能力组定义。
 *
 * 只解析两个 map 字面量：`Seedance25PerCallModels` 与 `Seedance25PerSecondModels`。
 * 解析方式是扫出 `var <名字> = map[string]bool{` 到配对的 `}` 之间的
 * `名字: true` 条目 —— 不引 Go 工具链，也不引正则解析器，保持零依赖。
 */
function parseGoBoolMap(source: string, varName: string): string[] {
  const startMarker = `var ${varName} = map[string]bool{`
  const start = source.indexOf(startMarker)
  assert.notEqual(
    start,
    -1,
    `后端 constant/seedance25_family.go 里找不到 ${varName} —— ` +
      '前端交叉校验依赖这个能力组，请确认后端定义没被改名或移走'
  )
  const end = source.indexOf('\n}', start)
  assert.notEqual(end, -1, `${varName} 的 map 字面量没有闭合`)

  const body = source.slice(start + startMarker.length, end)
  const members: string[] = []
  // 逐行扫：`Seedance25TeHuiModel: true,` 这种形式。
  // 值只认 true（false 的条目不算成员）。
  const entryPattern = /^\s*(\w+):\s*true\s*,?\s*$/gm
  let match: RegExpExecArray | null
  while ((match = entryPattern.exec(body)) !== null) {
    members.push(match[1])
  }
  return members
}

/**
 * 把 Go 侧的常量标识符解析成实际的模型名字符串。
 *
 * `constant/seedance25_family.go` 里写的是：
 *
 * ```go
 * const (
 *     Seedance25TeHuiModel   = "seedance2.5-特惠"
 *     Seedance25HuoDongModel = "seedance2.5-活动"
 * )
 * ```
 *
 * 能力组里引用的是标识符，所以要先把它换成字面值。
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

function loadBackendFamily(): {
  perCall: string[]
  perSecond: string[]
} {
  const source = readFileSync(BACKEND_FAMILY_FILE, 'utf-8')
  const constants = parseGoConstStrings(source)

  const resolve = (members: string[]): string[] =>
    members.map((identifier) => {
      const value = constants.get(identifier)
      assert.ok(
        value !== undefined,
        `后端能力组引用了常量 ${identifier}，但在 const 块里找不到它的字面值 —— ` +
          '前端交叉校验无法解析，请保持 const 块可被静态读取'
      )
      return value
    })

  return {
    perCall: resolve(parseGoBoolMap(source, 'Seedance25PerCallModels')),
    perSecond: resolve(parseGoBoolMap(source, 'Seedance25PerSecondModels')),
  }
}

function pricingModel(modelName: string): PricingModel {
  return {
    id: 1,
    model_name: modelName,
    quota_type: 1,
    model_ratio: 0,
    model_price: 0,
    completion_ratio: 0,
    enable_groups: ['default'],
    supported_endpoint_types: [],
  } as unknown as PricingModel
}

describe('后端按秒模型清单 × 前端 isPerSecondModel', () => {
  test('后端能力组文件可解析（测试自身的前置条件）', () => {
    const family = loadBackendFamily()
    assert.ok(family.perCall.length > 0, '按次组不应为空')
    assert.ok(family.perSecond.length > 0, '按秒组不应为空')
  })

  // 核心断言：后端说「按秒」的，前端必须也判「按秒」。
  test('后端按秒成员在前端一律显示 /秒', () => {
    const family = loadBackendFamily()
    for (const name of family.perSecond) {
      assert.equal(
        isPerSecondModel(pricingModel(name)),
        true,
        `后端把 ${name} 定义为按秒计费，前端却会显示「/次」 —— ` +
          '请在 model-helpers.ts 的 isPerSecondModel 里补登记'
      )
    }
  })

  // 反向断言：后端说「按次」的，前端必须显示「/次」。
  //
  // 这一条是本次新增模型（seedance2.5-活动）的直接守卫：
  // 它是按次档，前端**不能**命中任何按秒分支 —— 命中了就会显示
  // 「¥4.5/秒」，而实际是「¥4.5/次」，标价与账单直接对不上。
  test('后端按次成员在前端一律显示 /次', () => {
    const family = loadBackendFamily()
    for (const name of family.perCall) {
      assert.equal(
        isPerSecondModel(pricingModel(name)),
        false,
        `后端把 ${name} 定义为按次计费，前端却会显示「/秒」 —— ` +
          '这是静默错价：请确认 isPerSecondModel 没有误命中该模型'
      )
    }
  })

  // 两组必须互斥 —— 破了这个不变量，前端显示的单位必然有一边是错的。
  test('按次组与按秒组互斥', () => {
    const family = loadBackendFamily()
    const perSecondSet = new Set(family.perSecond)
    for (const name of family.perCall) {
      assert.ok(
        !perSecondSet.has(name),
        `${name} 同时在按次组和按秒组里 —— 前端显示的单位必然有一边是错的`
      )
    }
  })

  // 活动版必须落在按次组（回归守卫：它是本次新增的促销档）。
  test('seedance2.5-活动 在按次组且不在按秒组', () => {
    const family = loadBackendFamily()
    assert.ok(
      family.perCall.includes('seedance2.5-活动'),
      'seedance2.5-活动 必须在后端按次组里'
    )
    assert.ok(
      !family.perSecond.includes('seedance2.5-活动'),
      'seedance2.5-活动 绝不能在按秒组里'
    )
    assert.equal(
      isPerSecondModel(pricingModel('seedance2.5-活动')),
      false,
      'seedance2.5-活动 是按次档，前端必须显示「/次」'
    )
  })
})
