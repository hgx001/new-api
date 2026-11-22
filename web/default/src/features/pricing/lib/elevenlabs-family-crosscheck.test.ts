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

For commercial licensing, please contact support at quantumnous.com
*/
/**
 * ElevenLabs 三档 × 前端「按秒」穷举表 × 后端计费口径的交叉校验。
 *
 * ── 这道闸门存在的理由（2026-11-22 真机）─────────────────────────────
 *
 * 上架当天用户直接问：「价格是不是标错了，应该是 ¥0.25/秒 而不是 ¥0.25/请求」。
 * 查账本：5 秒 360p 实测 `quota=85615`，85615/500000×7.3 = ¥1.25 → ¥0.25/秒，
 * **扣费是对的**；错的只有广场上那个单位 —— `el-*` 没登记进
 * `isPerSecondModel()`，牌价 ¥0.25 被挂在「/次」下面，客户按次理解就是错价。
 *
 * 与 seedance2.5 / capcut 那两道闸门同款：这类漂移永远是静默的，
 * 没有报错、没有告警，只有客户投诉或财务报表对不上。
 *
 * ── 为什么读源码 ──────────────────────────────────────────────────────
 *
 * `/api/pricing` 要起服务、连数据库，单测里做不到；而且它返回的是**当前生产
 * 配置**（DB 里的 options.ModelPrice），反映不了代码里的能力定义。源码才是来源。
 *
 * 两个方向都断言：
 *   ① 后端存在 el-* 常量 → 前端必须判「按秒」（防漏登记）；
 *   ② 前端集合里的名字 → 后端必须有对应常量（防前端写错名/提前上架）。
 */
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, test } from 'node:test'
import { fileURLToPath } from 'node:url'

import type { PricingModel } from '../types'
import { isPerSecondModel } from './model-helpers'

/**
 * 后端源码路径（仓库根）。
 *
 * 目录层级：src/features/pricing/lib → src/features/pricing → src/features
 * → src → default → web → 仓库根，所以要往上走 6 层。
 *
 * ⚠️ 用 import.meta.url 而不是 import.meta.dir：后者是 Bun 独有的，
 * tsgo（TypeScript）不认，会直接报 TS2339。
 */
const BACKEND_ADAPTOR_FILE = join(
  fileURLToPath(new URL('.', import.meta.url)),
  '..',
  '..',
  '..',
  '..',
  '..',
  '..',
  'relay',
  'channel',
  'task',
  'manwu',
  'adaptor.go'
)

/** 解析 Go 源码里所有 const 块的 `名字 = "字面值"`。 */
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

function backendElevenlabsModels(): string[] {
  const source = readFileSync(BACKEND_ADAPTOR_FILE, 'utf8')
  const constants = parseGoConstStrings(source)
  const names: string[] = []
  for (const [key, value] of constants) {
    if (!key.startsWith('ModelElevenlabs')) continue
    if (!value.startsWith('el-')) continue
    names.push(value)
  }
  return names
}

function pricingModel(modelName: string): PricingModel {
  // isPerSecondModel 只读 model_name，其余字段给最小占位即可。
  return { model_name: modelName } as PricingModel
}

describe('ElevenLabs 按秒口径 × 后端源码', () => {
  test('后端 el-* 常量都能解析到（防止正则失效导致空跑全绿）', () => {
    const names = backendElevenlabsModels()
    assert.ok(
      names.length >= 3,
      `后端 adaptor.go 应至少有 3 个 ModelElevenlabs* 公开名，实际解析到：${JSON.stringify(names)}`
    )
    assert.ok(names.includes('el-seedance-2.5flash'))
    assert.ok(names.includes('el-seedance-2.0mini'))
    assert.ok(names.includes('el-gemini-1.2flash'))
  })

  test('后端每一个 el-* 档位，前端都必须显示「/秒」', () => {
    for (const name of backendElevenlabsModels()) {
      assert.equal(
        isPerSecondModel(pricingModel(name)),
        true,
        `后端 ${name} 按秒计费（EstimateBilling 给 seconds 倍率），前端却没登记 → 广场会把每秒价挂在「/次」下，客户按次理解就是错价`
      )
    }
  })

  test('前端登记的 el-* 名字必须与后端常量一一对应', () => {
    const backend = new Set(backendElevenlabsModels())
    // 前端集合是私有常量，用行为反查：对所有 el-* 形态的候选名逐个判定，
    // 判定为「按秒」的必须能在后端找到同名字面值。
    const candidates = [
      'el-seedance-2.5flash',
      'el-seedance-2.0mini',
      'el-gemini-1.2flash',
      // 未上架的档位不该被前缀/误写命中
      'el-seedance-2.0',
      'el-seedance-2.0fast',
      'el-seedance-2.5',
    ]
    for (const name of candidates) {
      if (!isPerSecondModel(pricingModel(name))) continue
      assert.ok(
        backend.has(name),
        `前端把 ${name} 判成按秒，但后端 adaptor.go 没有对应常量（写错名或提前上架）`
      )
    }
  })

  test('非 el-* 名字不受影响（按次档仍显示「/次」）', () => {
    assert.equal(isPerSecondModel(pricingModel('seedance-2.5')), false)
    assert.equal(isPerSecondModel(pricingModel('el-unknown-model')), false)
  })
})
